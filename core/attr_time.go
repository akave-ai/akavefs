package core

import (
	"sync/atomic"
	"time"
)

// atomicTime is a time.Time that is read and written atomically.
//
// It exists for Inode.AttrTime. LookUpCached and the listing paths read that
// field holding the parent's lock only, while SetAttrTime is annotated to write
// it under the inode's own lock. A time.Time is three words, so a plain read
// can return a mix of two writes: a time that nobody stored (issue #24).
//
// Every Load returns a value that some Store was given, or the zero time. A
// reader can still see a time that is about to be replaced.
//
// It is not a lock, and readers do not take inode.mu instead, on purpose: a
// child's lock taken under its parent's is not provably deadlock-free against
// Rename, which locks its two directories in inode Id order, and a child can
// have a lower Id than its parent.
//
// The zero value is ready to use and reads as the zero time.Time: Inode
// literals that bypass NewInode rely on that. An atomicTime must not be copied
// after first use.
type atomicTime struct {
	p atomic.Pointer[time.Time]
}

// Load returns the time last stored, or the zero time if none was.
func (t *atomicTime) Load() time.Time {
	if p := t.p.Load(); p != nil {
		return *p
	}
	return time.Time{}
}

// Store sets the time. tm is this call's own copy, so the pointer kept aliases
// nothing of the caller's and the value behind it is never written again.
func (t *atomicTime) Store(tm time.Time) {
	t.p.Store(&tm)
}

// Before reports whether the stored time is before u.
func (t *atomicTime) Before(u time.Time) bool {
	return t.Load().Before(u)
}

// Unix returns the stored time as a Unix time.
func (t *atomicTime) Unix() int64 {
	return t.Load().Unix()
}
