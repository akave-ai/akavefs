package core

import (
	"context"
	"fmt"
	"sync/atomic"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// TestEvictEntryVersusOpenNoCloud opens a cached file while EvictEntry is
// called on it over and over, and checks that the file is never evicted while
// the handle is open.
//
// EvictEntry looks at the handle count and the cache state before it takes
// any lock. OpenFile raises the count under the inode's lock alone, so an
// open can finish between that look and the locks. Without a second look
// under the locks the file is then evicted with a handle on it.
//
// The opener reads the cache state twice while it holds its handle. Alive at
// the first read and dead at the second means an eviction with the handle
// open, because nothing else here makes an inode dead. An eviction that wins
// before the open shows as dead at the first read and is not an error.
//
// Whether a round hits the window is up to the scheduler, so a run can pass
// on code without the second look; it cannot fail on code that has it. Each
// round uses a fresh inode, because an evicted one is gone.
func (s *GoofysTest) TestEvictEntryVersusOpenNoCloud(t *C) {
	const rounds = 20000

	backend := &TestBackend{err: syscall.ENOSYS}
	s.cloud = backend
	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)
	root := s.getRoot(t)

	evictedWhileOpen := 0
	var openErr error
	for i := 0; i < rounds && openErr == nil; i++ {
		// A cached inode that was never given an expiry time is evictable.
		inode := NewInode(s.fs, root, fmt.Sprintf("cached%v", i))
		root.mu.Lock()
		s.fs.insertInode(root, inode)
		id := inode.Id
		root.mu.Unlock()

		var stop int32
		done := make(chan struct{})
		go func() {
			for atomic.LoadInt32(&stop) == 0 && !s.fs.EvictEntry(id) {
			}
			close(done)
		}()

		fh, err := inode.OpenFile()
		if err != nil {
			openErr = err
		} else {
			before := atomic.LoadInt32(&inode.CacheState)
			atomic.StoreInt32(&stop, 1)
			<-done
			after := atomic.LoadInt32(&inode.CacheState)
			if before != ST_DEAD && after == ST_DEAD {
				evictedWhileOpen++
			}
			fh.Release()
		}
		atomic.StoreInt32(&stop, 1)
		<-done
	}

	t.Assert(openErr, IsNil)
	t.Assert(evictedWhileOpen, Equals, 0)
}
