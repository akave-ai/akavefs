// AkaveFS-specific half of Goofys.RefreshInodeCache.
//
// It lives in its own file rather than in the inherited core/goofys.go and
// core/dir.go so that a future GeeseFS sync sees those files unchanged: the only
// trace of this fix in inherited code is one call site.

package core

import (
	"sort"
	"sync/atomic"
	"syscall"
)

// isDirtyLocked reports whether the inode holds state the backend does not: it
// is not clean itself, or it is a directory with unflushed descendants. This is
// the same predicate LookUpCached applies before it drops a cached child. It is
// written out here instead of being factored out of LookUpCached, because
// LookUpCached is inherited and changing it would conflict on every upstream
// sync.
//
// LOCKS_REQUIRED(inode.mu)
func (inode *Inode) isDirtyLocked() bool {
	return atomic.LoadInt32(&inode.CacheState) != ST_CACHED ||
		inode.isDir() && atomic.LoadInt64(&inode.dir.ModifiedChildren) > 0
}

// refreshCurrentChild rechecks parent's child called name on behalf of
// RefreshInodeCache, and returns the lookup error that caller turns into a
// NotifyDelete (not found) or a NotifyInvalEntry (anything else).
//
// The inode the kernel handed RefreshInodeCache may be stale:
// removeChildUnlocked drops a child from parent.dir.Children without marking it
// ST_DEAD, so fs.inodes can still return the old object after a listing inserted
// a new one under the same name. When the child registered now is that same
// inode, or there is none, this is master's path unchanged:
// recheckInode(inode, name), which never slurps for a non-nil inode.
//
// When the registered child is a different object:
//   - a child that is already dirty is kept untouched, and nil is returned
//     having issued no HEAD at all, so the refresh costs no request;
//   - otherwise it is looked up without a slurp, and it is removed only when the
//     lookup says it is gone, and only through removeChildUnlessDirty, which
//     re-checks under both locks that it is still registered and still clean;
//   - any other lookup error keeps the child and is returned. A throttle or a
//     5xx says nothing about whether the object exists, so it must not unlink a
//     valid child. That makes this path stricter than the inherited non-stale
//     one, where recheckInode removes on any error; that difference is
//     deliberate and left to upstream.
//
// LOCKS_EXCLUDED(parent.fs.mu)
// LOCKS_EXCLUDED(parent.mu)
// LOCKS_EXCLUDED(inode.mu)
func (parent *Inode) refreshCurrentChild(inode *Inode, name string) error {
	parent.mu.Lock()
	current := parent.findChildUnlocked(name)
	dirty := false
	if current != nil && current != inode {
		current.mu.Lock() // parent before child
		dirty = current.isDirtyLocked()
		current.mu.Unlock()
	}
	parent.mu.Unlock()

	if current == nil || current == inode {
		// Inherited path, left as is: unlike the stale path below, recheckInode
		// removes the child on any lookup error, not only on a not-found.
		_, err := parent.recheckInode(inode, name)
		return err
	}
	if dirty {
		return nil
	}
	_, err := parent.LookUp(name, false)
	if mapAwsError(err) == syscall.ENOENT && parent.removeChildUnlessDirty(current) {
		// It turned dirty between the pre-check and the removal, so it was kept.
		// Report success: the caller then only invalidates the entry.
		return nil
	}
	return err
}

// removeChildUnlessDirty removes inode from parent's children only if it is
// still the child registered under its name and is not dirty, both checked under
// parent.mu and inode.mu so that nothing can dirty it between the check and the
// removal. It exists for refreshCurrentChild, which removes a child the kernel
// did not hand it; the inherited removeChild removes regardless of state.
//
// It returns true only for the kept-because-dirty outcome. Both other outcomes
// return false: the child was removed, or inode is not the child registered
// under its name and nothing was done at all.
//
// NOTE: for a directory this does not fully close the window. Its
// ModifiedChildren is changed by a plain atomic add in addModified, which holds
// no lock this function can take: a write or open under the directory holds only
// the written inode's own mu (WriteFile, OpenFile). So a directory can become
// dirty after the check below and still be removed. The file half is closed:
// CacheState only changes through SetCacheState, under inode.mu. The residual
// window costs a detached subtree, not data: the directory is dropped from the
// parent's children until the next listing re-creates it, while its dirty
// descendants are still queued in fs.inodeQueue and still flush. That re-listing
// creates a different inode object for the name, so until a detached dirty
// descendant has flushed, a traversal through the new object does not show it.
//
// LOCKS_EXCLUDED(parent.fs.mu)
// LOCKS_EXCLUDED(parent.mu)
// LOCKS_EXCLUDED(inode.mu)
func (parent *Inode) removeChildUnlessDirty(inode *Inode) (dirty bool) {
	parent.mu.Lock()
	defer parent.mu.Unlock()

	l := len(parent.dir.Children)
	i := sort.Search(l, parent.findInodeFunc(inode.Name))
	if i >= l || parent.dir.Children[i] != inode {
		return false
	}

	inode.mu.Lock() // parent before child, as removeChild does
	defer inode.mu.Unlock()

	if inode.isDirtyLocked() {
		return true
	}
	parent.removeChildUnlocked(inode)
	return false
}
