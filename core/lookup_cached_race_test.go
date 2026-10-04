package core

import (
	"context"
	"sync"
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
	child.AttrTime.Store(time.Time{})
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

// TestLookUpCachedVersusAttrTimeNoCloud runs Inode.LookUpCached, which reads a
// child's AttrTime while holding only the parent's lock, against SetAttrTime,
// in two phases that never overlap.
//
// The race detector is the oracle. The asserts at the end hold whether the
// field is read atomically or not; only a -race run fails on a plain one.
//
// Phase 1 is the regression test. The writer holds the child's own lock, as
// SetAttrTime is annotated, which does not order it against a reader that
// holds the parent's. On a plain field the report has the read in LookUpCached
// on one side and the write in SetAttrTime on the other.
//
// Phase 2 is a guard, and cannot fail on a plain field as the code stands. Its
// writer holds the parent's lock only, as handleListResult and insertSubTree
// do, and that is the lock LookUpCached reads under, so the two are ordered by
// it. The phase fails if a later change moves the read in LookUpCached out
// from under the parent's lock without synchronising the field. It starts
// only after phase 1's writer has ended: SetAttrTime also writes ExpireTime,
// which is a plain field, and the two writers hold different locks.
//
// The written time alternates between the zero time and the current time, so
// the lookups take both the expired and the fresh branch. The child's state is
// never ST_CACHED while they run, so a lookup that finds the attributes
// expired returns the cached child without rechecking it and no backend method
// is called; the backend counts the two calls a recheck would make.
//
// The state is stored directly, so the child never enters the dirty queue,
// and it is put back to ST_CACHED before the test ends. What SetAttrTime
// leaves behind is not undone: the child stays registered by expiry time in
// this test's own filesystem. MetaEvictor looks at that register only once
// the number of inodes reaches the entry limit, which two inodes do not.
func (s *GoofysTest) TestLookUpCachedVersusAttrTimeNoCloud(t *C) {
	// Enough stores, in each phase, for the two goroutines to overlap.
	const stores = 20000
	// Neither bound is reached in a correct run. The first stops a phase's
	// lookup loop if its stores never finish; the second, longer, stops the
	// test waiting for that loop.
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

	child.mu.Lock()
	atomic.StoreInt32(&child.CacheState, ST_MODIFIED)
	child.mu.Unlock()
	queuedBefore := s.fs.inodeQueue.Size()

	// What a phase's lookup loop saw. It is sent to the test goroutine, which
	// is the only one that calls gocheck.
	type lookups struct {
		calls  int
		errs   int
		other  int
		sawEnd bool
	}

	// One phase: SetAttrTime under mu against LookUpCached. finished is false
	// if the lookup loop did not report within waitLimit.
	phase := func(mu *sync.Mutex) (res lookups, finished bool) {
		begin := make(chan struct{})
		end := make(chan struct{})
		looked := make(chan lookups, 1)
		go func() {
			<-begin
			for i := 0; i < stores; i++ {
				mu.Lock()
				if i%2 == 0 {
					child.SetAttrTime(time.Time{})
				} else {
					child.SetAttrTime(time.Now())
				}
				mu.Unlock()
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
		select {
		case res = <-looked:
			finished = true
		case <-time.After(waitLimit):
		}
		return
	}

	res1, finished1 := phase(&child.mu)
	// Phase 2 runs only once phase 1's stores are known to have ended.
	var res2 lookups
	finished2 := false
	if finished1 && res1.sawEnd {
		res2, finished2 = phase(&root.mu)
	}
	// The state is restored only once both phases are known to have ended: a
	// lookup loop still running would recheck a child that is ST_CACHED and
	// expired against the backend.
	if finished2 && res2.sawEnd {
		child.mu.Lock()
		atomic.StoreInt32(&child.CacheState, ST_CACHED)
		child.mu.Unlock()
	}
	queuedAfter := s.fs.inodeQueue.Size()

	// No lock is held here: assert only now.
	t.Assert(finished1, Equals, true)
	t.Assert(res1.sawEnd, Equals, true)
	t.Assert(res1.calls > 0, Equals, true)
	t.Assert(res1.errs, Equals, 0)
	t.Assert(res1.other, Equals, 0)
	t.Assert(finished2, Equals, true)
	t.Assert(res2.sawEnd, Equals, true)
	t.Assert(res2.calls > 0, Equals, true)
	t.Assert(res2.errs, Equals, 0)
	t.Assert(res2.other, Equals, 0)
	t.Assert(atomic.LoadInt64(&backendCalls), Equals, int64(0))
	t.Assert(queuedAfter, Equals, queuedBefore)
}

// TestAtomicTimeNoCloud pins what the readers of Inode.AttrTime rely on from
// atomicTime: a field that was never stored reads as the zero time, as a plain
// time.Time field did, and a Load returns the value last stored.
//
// The oracle is the returned values. A Load that dereferences the pointer
// without checking it panics on the first assert.
func (s *GoofysTest) TestAtomicTimeNoCloud(t *C) {
	var zero time.Time
	later := time.Now()

	// An Inode literal that does not go through NewInode, as Inode.LookUp and
	// other tests make it.
	inode := &Inode{}
	t.Assert(inode.AttrTime.Load() == zero, Equals, true)
	t.Assert(inode.AttrTime.Before(later), Equals, zero.Before(later))
	t.Assert(inode.AttrTime.Before(zero), Equals, false)
	t.Assert(inode.AttrTime.Unix(), Equals, zero.Unix())

	// == on the whole value: the monotonic reading is kept, not only the
	// instant.
	now := time.Now()
	inode.AttrTime.Store(now)
	t.Assert(inode.AttrTime.Load() == now, Equals, true)
	t.Assert(inode.AttrTime.Before(now.Add(time.Second)), Equals, true)
	t.Assert(inode.AttrTime.Before(now), Equals, false)
	t.Assert(inode.AttrTime.Unix(), Equals, now.Unix())

	// The zero time is a value production stores, to invalidate.
	inode.AttrTime.Store(zero)
	t.Assert(inode.AttrTime.Load() == zero, Equals, true)
	t.Assert(inode.AttrTime.Unix(), Equals, zero.Unix())
}
