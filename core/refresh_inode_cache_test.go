package core

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/jacobsa/fuse/fuseops"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// Names for the asDir argument below, which is otherwise a bare bool at every
// call site. The prefix keeps them clear of upstream symbols: this is package
// core, shared with every inherited file.
const (
	staleRefreshChildIsFile = false
	staleRefreshChildIsDir  = true
)

// setUpStaleInodeRefreshNoCloud builds the fixture-free scenario the NoCloud
// tests in this file share: a Goofys over a TestBackend whose HeadBlob answers as
// headErr says and which lists nothing, a root holding one clean child "file1"
// (current), and a second inode for the same name carrying a different id
// (stale), which is what the kernel still refers to after a listing replaced the
// child object.
//
// The backend hooks are closures inside this method on purpose: a -race report
// is identified by the top frames of its two accesses, so a hook defined in
// another function reports a frame with no baseline and reads as a new race.
//
// asDir makes current a directory before it is inserted, while nothing else can
// reach it. onHead, when non-nil, runs inside HeadBlobFunc, which is how a test
// dirties current while a lookup is in flight. headErr is what HeadBlobFunc
// returns; a nil headErr makes the object exist instead, which is the lookup
// success the removal must not act on.
func (s *GoofysTest) setUpStaleInodeRefreshNoCloud(t *C, asDir bool, onHead func(), headErr error) (
	root, current, stale *Inode, notes *[]interface{}, counts func() (heads, slurps int)) {

	flags := cfg.DefaultFlags()

	// The counters are under mu because LookUpInodeMaybeDir calls the backend
	// from goroutines.
	var mu sync.Mutex
	var heads, slurps int
	backend := &TestBackend{
		err: syscall.ENOSYS,
		HeadBlobFunc: func(param *HeadBlobInput) (*HeadBlobOutput, error) {
			mu.Lock()
			heads++
			mu.Unlock()
			if onHead != nil {
				onHead()
			}
			if headErr != nil {
				return nil, headErr
			}
			// Otherwise the bucket holds exactly the file object: its key
			// answers and the "file1/" directory-marker probe does not. Were both
			// to answer, LookUp could take the marker's answer first and, as
			// upstream does for a name that is both, replace the file child with
			// a directory, which is not the case under test.
			if strings.HasSuffix(param.Key, "/") {
				return nil, syscall.ENOENT
			}
			// A key is all LookUp needs to match the object to the registered
			// child; with no ETag and size 0 the child's cache is left as it is.
			return &HeadBlobOutput{BlobItemOutput: BlobItemOutput{Key: PString(param.Key)}}, nil
		},
		ListBlobsFunc: func(param *ListBlobsInput) (*ListBlobsOutput, error) {
			// LookUpInodeMaybeDir always lists with Delimiter "/", so only a
			// slurp lists without one.
			if param.Delimiter == nil {
				mu.Lock()
				slurps++
				mu.Unlock()
			}
			return &ListBlobsOutput{}, nil
		},
	}
	s.cloud = backend

	var err error
	s.fs, err = newGoofys(context.Background(), "test", flags, func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	// The callback is synchronous, so a test can read what it captured straight
	// after RefreshInodeCache returns.
	var captured []interface{}
	s.fs.NotifyCallback = func(n []interface{}) { captured = append(captured, n...) }

	root = s.getRoot(t)
	current = NewInode(s.fs, root, "file1")
	if asDir {
		current.ToDir()
	}
	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, current)
	root.mu.Unlock()

	current.mu.Lock() // an Id can be reassigned, so read it under its own lock
	currentId := current.Id
	current.mu.Unlock()

	stale = NewInode(s.fs, root, "file1")
	// A distinct id is what shows which inode is named and which one is removed.
	stale.Id = currentId + 1000

	return root, current, stale, &captured, func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return heads, slurps
	}
}

func (s *GoofysTest) TestRefreshInodeCacheRemovesCurrentChildNotifiesStaleIdNoCloud(t *C) {
	root, _, stale, notes, counts := s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, nil, syscall.ENOENT)

	t.Assert(s.fs.RefreshInodeCache(stale), IsNil)

	t.Assert(root.findChild("file1") == nil, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	del, ok := (*notes)[0].(*fuseops.NotifyDelete)
	t.Assert(ok, Equals, true)
	// The kernel's dentry holds the id it looked up, i.e. the stale one, so that
	// is the child the delete notification has to name.
	t.Assert(del.Child, Equals, stale.Id)

	_, slurps := counts()
	// This path never slurps, as on master.
	t.Assert(slurps, Equals, 0)
}

func (s *GoofysTest) TestRefreshInodeCacheKeepsDirtyCurrentChildForStaleInodeNoCloud(t *C) {
	root, current, stale, notes, counts := s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, nil, syscall.ENOENT)

	// No WakeupFlusher: the flusher would try a PUT on a TestBackend that has
	// no backend behind it.
	current.mu.Lock() // SetCacheState is LOCKS_REQUIRED(inode.mu)
	current.SetCacheState(ST_CREATED)
	current.mu.Unlock()

	t.Assert(s.fs.RefreshInodeCache(stale), IsNil)

	t.Assert(root.findChild("file1") == current, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	_, ok := (*notes)[0].(*fuseops.NotifyInvalEntry)
	t.Assert(ok, Equals, true)

	heads, _ := counts()
	// A dirty current child is not looked up: S3 does not hold its true state
	// until it is flushed.
	t.Assert(heads, Equals, 0)
}

func (s *GoofysTest) TestRefreshInodeCacheKeepsDirtyCurrentDirForStaleInodeNoCloud(t *C) {
	root, current, stale, notes, counts := s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsDir, nil, syscall.ENOENT)

	current.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	g := NewInode(s.fs, current, "g")
	s.fs.insertInode(current, g)
	current.mu.Unlock()
	g.mu.Lock() // SetCacheState is LOCKS_REQUIRED(inode.mu)
	g.SetCacheState(ST_CREATED)
	g.mu.Unlock()

	// The directory itself is clean, so only the directory half of the guard —
	// ModifiedChildren — can keep it.
	t.Assert(atomic.LoadInt32(&current.CacheState), Equals, ST_CACHED)
	t.Assert(atomic.LoadInt64(&current.dir.ModifiedChildren), Equals, int64(1))

	t.Assert(s.fs.RefreshInodeCache(stale), IsNil)

	t.Assert(root.findChild("file1") == current, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	_, ok := (*notes)[0].(*fuseops.NotifyInvalEntry)
	t.Assert(ok, Equals, true)

	heads, _ := counts()
	t.Assert(heads, Equals, 0)
}

func (s *GoofysTest) TestRemoveChildUnlessDirtyNoCloud(t *C) {
	root, current, _, _, _ := s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, nil, syscall.ENOENT)

	// Dirty at removal time: the child is kept.
	current.mu.Lock() // SetCacheState is LOCKS_REQUIRED(inode.mu)
	current.SetCacheState(ST_MODIFIED)
	current.mu.Unlock()
	t.Assert(root.removeChildUnlessDirty(current), Equals, true)
	t.Assert(root.findChild("file1") == current, Equals, true)

	// Same name, but not the registered child: nothing is removed.
	other := NewInode(s.fs, root, "file1")
	t.Assert(root.removeChildUnlessDirty(other), Equals, false)
	t.Assert(root.findChild("file1") == current, Equals, true)

	// Registered and clean: removed. This is the positive control that the
	// helper removes at all, so the two cases above are not vacuous.
	current.mu.Lock()
	current.SetCacheState(ST_CACHED)
	current.mu.Unlock()
	t.Assert(root.removeChildUnlessDirty(current), Equals, false)
	t.Assert(root.findChild("file1") == nil, Equals, true)
}

func (s *GoofysTest) TestRefreshInodeCacheKeepsChildDirtiedDuringLookupForStaleInodeNoCloud(t *C) {
	var root, current, stale *Inode
	var notes *[]interface{}
	var once sync.Once
	// The hook closes over current before the setup assigns it, which is safe
	// because the setup itself issues no HeadBlob: the hook first runs inside the
	// RefreshInodeCache call below, long after the assignment.
	root, current, stale, notes, _ = s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, func() {
		once.Do(func() {
			// LookUp holds no lock of its own while LookUpInodeMaybeDir runs, so
			// taking the child's lock from the backend hook cannot deadlock.
			current.mu.Lock()
			current.SetCacheState(ST_MODIFIED)
			current.mu.Unlock()
		})
	}, syscall.ENOENT)

	t.Assert(s.fs.RefreshInodeCache(stale), IsNil)

	t.Assert(root.findChild("file1") == current, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	_, ok := (*notes)[0].(*fuseops.NotifyInvalEntry)
	t.Assert(ok, Equals, true)
}

func (s *GoofysTest) TestRefreshInodeCacheReturnsLookupErrorForChildDirtiedDuringLookupNoCloud(t *C) {
	var root, current, stale *Inode
	var notes *[]interface{}
	var once sync.Once
	// Safe for the same reason as the test above: the setup issues no HeadBlob,
	// so the hook first runs well after current is assigned.
	root, current, stale, notes, _ = s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, func() {
		once.Do(func() {
			current.mu.Lock()
			current.SetCacheState(ST_MODIFIED)
			current.mu.Unlock()
		})
	}, syscall.EIO)

	// Keeping the dirty child must not swallow a real backend error: only an
	// ENOENT lookup clears it.
	err := s.fs.RefreshInodeCache(stale)
	t.Assert(err, Equals, syscall.EIO)
	t.Assert(root.findChild("file1") == current, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	_, ok := (*notes)[0].(*fuseops.NotifyInvalEntry)
	t.Assert(ok, Equals, true)
}

func (s *GoofysTest) TestRefreshInodeCacheKeepsCleanChildOnLookupErrorForStaleInodeNoCloud(t *C) {
	root, current, stale, notes, _ := s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, nil, syscall.EIO)

	// current stays clean throughout. A lookup error that is not a not-found
	// says nothing about whether the object exists, so a throttle or a 5xx must
	// never unlink a valid child: the child is kept, the entry is only
	// invalidated, and the backend error is returned.
	err := s.fs.RefreshInodeCache(stale)
	t.Assert(err, Equals, syscall.EIO)
	t.Assert(root.findChild("file1") == current, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	_, ok := (*notes)[0].(*fuseops.NotifyInvalEntry)
	t.Assert(ok, Equals, true)
}

func (s *GoofysTest) TestRefreshInodeCacheKeepsCleanChildThatStillExistsForStaleInodeNoCloud(t *C) {
	root, current, stale, notes, counts := s.setUpStaleInodeRefreshNoCloud(t, staleRefreshChildIsFile, nil, nil)

	// The object still exists, so the lookup succeeds. A successful lookup is
	// the one outcome that must never reach the removal: the child is live.
	t.Assert(s.fs.RefreshInodeCache(stale), IsNil)
	t.Assert(root.findChild("file1") == current, Equals, true)
	t.Assert(len(*notes), Equals, 1)
	_, ok := (*notes)[0].(*fuseops.NotifyInvalEntry)
	t.Assert(ok, Equals, true)

	// The lookup did run, so this is the success branch and not the dirty
	// pre-check, which returns without one.
	heads, _ := counts()
	t.Assert(heads > 0, Equals, true)
}

// Unlike the tests above this one runs against the real cloud fixture (no
// NoCloud in its name), so it only runs where s3proxy does, i.e. in CI. It lives
// here rather than in the inherited goofys_test.go so that file stays upstream
// as is; the fixture/NoCloud split keys on the test name, not the file.
func (s *GoofysTest) TestRefreshInodeCacheRemovesCurrentChildForStaleInode(t *C) {
	root := s.getRoot(t)
	current, err := root.LookUp("file1", false)
	t.Assert(err, IsNil)
	t.Assert(current, NotNil)

	stale := NewInode(s.fs, root, current.Name)
	// The stale inode is registered under its own id, as the kernel still refers
	// to it after a listing replaced the child, and so that it does not overwrite
	// the current child's entry in fs.inodes.
	s.fs.mu.Lock() // allocateInodeId is LOCKS_REQUIRED(fs.mu)
	stale.Id = s.fs.allocateInodeId()
	s.fs.inodes[stale.Id] = stale
	s.fs.mu.Unlock()

	var notes []interface{}
	s.fs.NotifyCallback = func(n []interface{}) { notes = append(notes, n...) }

	s.removeBlob(s.cloud, t, current.Name)
	t.Assert(s.fs.RefreshInodeCache(stale), IsNil)

	root.mu.Lock()
	t.Assert(root.findChildUnlocked(current.Name), IsNil)
	root.mu.Unlock()

	// The kernel's dentry holds the id it looked up, i.e. the stale one.
	t.Assert(len(notes), Equals, 1)
	del, ok := notes[0].(*fuseops.NotifyDelete)
	t.Assert(ok, Equals, true)
	t.Assert(del.Child, Equals, stale.Id)
}
