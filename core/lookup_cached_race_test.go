package core

import (
	"context"
	"sync/atomic"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// TestLookUpCachedVersusCacheStateNoCloud runs Inode.LookUpCached, which reads
// the cache state of a child whose attributes have expired while holding only
// the parent's lock, against a change of that state made under the child's own
// lock, as SetCacheState's callers make it.
//
// The race detector is the oracle. The asserts at the end hold whether
// LookUpCached reads the state atomically or not; only a -race run fails on a
// plain read. Its report has the read in LookUpCached on one side and
// sync/atomic.StoreInt32 on the other, in the goroutine this test starts for
// the stores: the storing line has no frame of its own in the report.
//
// The state alternates between two values that are both not ST_CACHED, so every
// lookup returns the cached child without rechecking it and no backend method
// is called; the backend counts the two calls a recheck would make. The state
// is stored directly instead of through SetCacheState, so the child never
// enters the dirty queue, and it is put back to ST_CACHED before the test ends:
// the background Flusher has nothing to act on.
func (s *GoofysTest) TestLookUpCachedVersusCacheStateNoCloud(t *C) {
	// Enough stores for the two goroutines to overlap.
	const stores = 20000
	// Neither bound is reached in a correct run. The first stops the lookup
	// loop if the stores never finish; the second, longer, stops the test
	// waiting for that loop.
	const loopLimit = 30 * time.Second
	const waitLimit = 60 * time.Second

	var backendCalls int64
	backend := &TestBackend{err: syscall.ENOSYS}
	backend.HeadBlobFunc = func(*HeadBlobInput) (*HeadBlobOutput, error) {
		atomic.AddInt64(&backendCalls, 1)
		return nil, syscall.ENOSYS
	}
	backend.ListBlobsFunc = func(*ListBlobsInput) (*ListBlobsOutput, error) {
		atomic.AddInt64(&backendCalls, 1)
		return nil, syscall.ENOSYS
	}
	s.cloud = backend
	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	root := s.getRoot(t)
	child := NewInode(s.fs, root, "child")
	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, child)
	root.mu.Unlock()

	// Expired attributes are what sends LookUpCached to the state check. The
	// field is written directly: SetAttrTime would also register an expiry
	// time for the inode with the fs.
	child.mu.Lock()
	child.AttrTime = time.Time{}
	atomic.StoreInt32(&child.CacheState, ST_MODIFIED)
	child.mu.Unlock()
	queuedBefore := s.fs.inodeQueue.Size()

	// What the lookup loop saw. It is sent to the test goroutine, which is the
	// only one that calls gocheck.
	type lookups struct {
		calls  int
		errs   int
		other  int
		sawEnd bool
	}

	begin := make(chan struct{})
	end := make(chan struct{})
	looked := make(chan lookups, 1)
	go func() {
		<-begin
		for i := 0; i < stores; i++ {
			child.mu.Lock() // SetCacheState is called with inode.mu held
			if i%2 == 0 {
				atomic.StoreInt32(&child.CacheState, ST_CREATED)
			} else {
				atomic.StoreInt32(&child.CacheState, ST_MODIFIED)
			}
			child.mu.Unlock()
		}
		close(end)
	}()
	go func() {
		<-begin
		var l lookups
		deadline := time.Now().Add(loopLimit)
		for !l.sawEnd && time.Now().Before(deadline) {
			inode, err := root.LookUpCached("child")
			if err != nil {
				l.errs++
			} else if inode != child {
				l.other++
			}
			l.calls++
			select {
			case <-end:
				l.sawEnd = true
			default:
			}
		}
		looked <- l
	}()
	close(begin)

	var res lookups
	finished := false
	select {
	case res = <-looked:
		finished = true
	case <-time.After(waitLimit):
	}
	// The state is restored only once the stores are known to have ended:
	// before that the storing goroutine would overwrite it.
	if finished && res.sawEnd {
		child.mu.Lock()
		atomic.StoreInt32(&child.CacheState, ST_CACHED)
		child.mu.Unlock()
	}
	queuedAfter := s.fs.inodeQueue.Size()

	// No lock is held here: assert only now.
	t.Assert(finished, Equals, true)
	t.Assert(res.sawEnd, Equals, true)
	t.Assert(res.calls > 0, Equals, true)
	t.Assert(res.errs, Equals, 0)
	t.Assert(res.other, Equals, 0)
	t.Assert(atomic.LoadInt64(&backendCalls), Equals, int64(0))
	t.Assert(queuedAfter, Equals, queuedBefore)
}
