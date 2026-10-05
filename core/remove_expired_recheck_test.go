package core

import (
	"context"
	"sync/atomic"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// TestRemoveExpiredVersusOpenNoCloud lets an open of a cached file finish
// while removeExpired is between its look at the file and the file's lock,
// and checks that the file is not dropped.
//
// removeExpired looks at a child's handle count and cache state under the
// directory's lock and only then takes the child's lock. OpenFile raises the
// count under the child's lock alone, so an open that holds that lock makes
// removeExpired wait and then finishes ahead of it. Without a second look
// under the child's lock the file is dropped with a handle on it.
//
// The test plays the open by hand so that it can hold the child's lock for as
// long as it needs: it takes the lock, starts removeExpired, raises the count
// the way OpenFile does and lets go. removeExpired holds the directory's lock
// for its whole run, so a failed TryLock on the directory says it has started;
// the pause after that gives it time to pass its first look and reach the
// child's lock. If the pause were too short the first look would already see
// the handle and the test would pass on code without the second look; it
// cannot fail on code that has it.
//
// As in the other fixture-free tests, asserts run only after every lock is
// released.
func (s *GoofysTest) TestRemoveExpiredVersusOpenNoCloud(t *C) {
	const waitLimit = 30 * time.Second

	backend := &TestBackend{err: syscall.ENOSYS}
	s.cloud = backend
	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)
	root := s.getRoot(t)

	inode := NewInode(s.fs, root, "cached")
	root.mu.Lock()
	s.fs.insertInode(root, inode)
	// A refresh that started in the future makes every child look stale.
	root.dir.refreshStartTime = time.Now().Add(time.Hour)
	root.mu.Unlock()

	inode.mu.Lock()

	done := make(chan struct{})
	go func() {
		root.mu.Lock()
		root.removeExpired("")
		root.mu.Unlock()
		close(done)
	}()

	started := false
	deadline := time.Now().Add(waitLimit)
	for !started && time.Now().Before(deadline) {
		if root.mu.TryLock() {
			root.mu.Unlock()
			time.Sleep(time.Millisecond)
		} else {
			started = true
		}
	}
	time.Sleep(100 * time.Millisecond)

	atomic.AddInt32(&inode.fileHandles, 1)
	inode.mu.Unlock()

	finished := false
	select {
	case <-done:
		finished = true
	case <-time.After(waitLimit):
	}
	// Whether the file is still in its directory is what shows a removal:
	// the cache state does not, because dropping the directory's reference
	// to the inode resets it.
	var kept *Inode
	if finished {
		root.mu.Lock()
		kept = root.findChildUnlocked("cached")
		root.mu.Unlock()
	}

	t.Assert(started, Equals, true)
	t.Assert(finished, Equals, true)
	t.Assert(kept, Equals, inode)
}
