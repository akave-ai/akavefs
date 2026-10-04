package core

import (
	"context"
	"fmt"
	"sync/atomic"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// TestUploadDecisionVersusReleaseNoCloud runs the upload decision (sendUpload,
// called under inode.mu as TryFlush calls it) against FileHandle.Release, which
// changes Inode.fileHandles without inode.mu.
//
// The race detector is the oracle. The asserts at the end hold whether the
// upload path reads the field atomically or not; only a -race run fails on a
// plain read. Its report has the read in sendUpload, sendUploadParts or
// patchObjectRanges on one side and, on the other, sync/atomic.AddInt32 at the
// line of this test that calls Release: FileHandle.Release has no frame of its
// own in the report. The three cases exist to reach each of those reads.
//
// The handle count is kept above zero throughout: every case opens one handle
// more than it releases concurrently. With a handle open each upload decision
// declines, so no upload starts and no backend method is called, and Release
// never reaches zero off the test goroutine, where it would read inode.Parent
// without a lock.
//
// The inodes never enter the dirty queue (their state is set directly instead
// of through SetCacheState) and are put back to clean before the test ends, so
// the background Flusher has nothing to act on.
func (s *GoofysTest) TestUploadDecisionVersusReleaseNoCloud(t *C) {
	// Enough releases for the two goroutines to overlap.
	const releases = 2000
	// Neither bound is reached in a correct run. The first stops the decision
	// loop if the releases never finish; the second, longer, stops the test
	// waiting for that loop. When the second is reached the test fails without
	// waiting for anything else: see busy below.
	const loopLimit = 30 * time.Second
	const waitLimit = 60 * time.Second

	backend := &TestBackend{err: syscall.ENOSYS}
	s.cloud = backend
	flags := cfg.DefaultFlags()
	// These flags belong to this test's fs alone, so there is nothing to
	// restore. Only the "patch" inode reaches the PATCH path: the other two
	// stay ST_CACHED.
	flags.UsePatch = true
	var err error
	s.fs, err = newGoofys(context.Background(), "test", flags, func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	root := s.getRoot(t)
	small := NewInode(s.fs, root, "small")
	multipart := NewInode(s.fs, root, "multipart")
	patch := NewInode(s.fs, root, "patch")
	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, small)
	s.fs.insertInode(root, multipart)
	s.fs.insertInode(root, patch)
	root.mu.Unlock()

	// A started multipart upload with nothing dirty: sendUploadParts reads the
	// count, finds no part to send, and sendUpload reads it again to decide
	// whether to complete the upload.
	multipart.mu.Lock()
	multipart.Attributes.Size = flags.SinglePartMB*1024*1024 + 1
	multipart.mpu = &MultipartBlobCommitInput{}
	multipart.mu.Unlock()

	// A modified, non-empty small object: with UsePatch that is what sends
	// sendUpload to patchObjectRanges.
	patch.mu.Lock()
	atomic.StoreInt32(&patch.CacheState, ST_MODIFIED)
	patch.Attributes.Size = 1
	patch.knownSize = 1
	patch.mu.Unlock()

	// What the decision loop saw. It is sent to the test goroutine, which is
	// the only one that calls gocheck.
	type decisions struct {
		calls    int
		started  int
		released bool
	}
	type outcome struct {
		name     string
		openErr  error
		finished bool
		decisions
		left int32
	}
	var outcomes []outcome
	// The inode whose decision goroutine did not finish in time, if any. That
	// goroutine may be inside sendUpload with the inode's lock held, so the
	// cleanup below must not take that lock: it would wait for as long as the
	// goroutine does.
	var busy *Inode

	for _, c := range []struct {
		name  string
		inode *Inode
	}{
		{"small", small},
		{"multipart", multipart},
		{"patch", patch},
	} {
		inode := c.inode
		res := outcome{name: c.name}

		fhs := make([]*FileHandle, 0, releases+1)
		for i := 0; i < releases+1; i++ {
			fh, err := inode.OpenFile()
			if err != nil {
				res.openErr = err
				break
			}
			fhs = append(fhs, fh)
		}
		if res.openErr != nil {
			for _, fh := range fhs {
				fh.Release()
			}
			outcomes = append(outcomes, res)
			break
		}

		begin := make(chan struct{})
		released := make(chan struct{})
		decided := make(chan decisions, 1)
		go func() {
			<-begin
			for _, fh := range fhs[:releases] {
				fh.Release()
			}
			close(released)
		}()
		go func() {
			<-begin
			var d decisions
			deadline := time.Now().Add(loopLimit)
			for !d.released && time.Now().Before(deadline) {
				inode.mu.Lock() // sendUpload is called with inode.mu held
				if inode.sendUpload(1) {
					d.started++
				}
				inode.mu.Unlock()
				d.calls++
				select {
				case <-released:
					d.released = true
				default:
				}
			}
			decided <- d
		}()
		close(begin)

		select {
		case res.decisions = <-decided:
			res.finished = true
		case <-time.After(waitLimit):
		}
		if !res.finished || !res.released {
			// A goroutine may still be running on this inode: leave its
			// handles alone and stop here.
			if !res.finished {
				busy = inode
			}
			outcomes = append(outcomes, res)
			break
		}

		res.left = atomic.LoadInt32(&inode.fileHandles)
		fhs[releases].Release()
		outcomes = append(outcomes, res)
	}

	if busy != multipart {
		multipart.mu.Lock()
		multipart.mpu = nil
		multipart.mu.Unlock()
	}
	if busy != patch {
		patch.mu.Lock()
		atomic.StoreInt32(&patch.CacheState, ST_CACHED)
		patch.mu.Unlock()
	}

	// Every lock this goroutine took is released, and every inode it could
	// lock is clean again: assert only now, so that a failure leaves nothing
	// held by the test goroutine. An inode left as it was (busy) still has its
	// handles open and is in no dirty queue, so the Flusher does not act on it.
	t.Assert(len(outcomes), Equals, 3)
	for _, res := range outcomes {
		comment := Commentf("case %v", res.name)
		t.Assert(res.openErr, IsNil, comment)
		t.Assert(res.finished, Equals, true, comment)
		t.Assert(res.released, Equals, true, comment)
		t.Assert(res.calls > 0, Equals, true, comment)
		t.Assert(res.started, Equals, 0, comment)
		t.Assert(res.left, Equals, int32(1), comment)
	}
}

// TestDumpVersusReleaseNoCloud runs Inode.DumpThis, which reads
// Inode.fileHandles under inode.mu, against FileHandle.Release, which changes
// it without inode.mu.
//
// The race detector is the oracle. The asserts at the end hold whether DumpThis
// reads the field atomically or not; only a -race run fails on a plain read.
// Its report has the read in DumpThis on one side and, on the other,
// sync/atomic.AddInt32 at the line of this test that calls Release.
//
// As in TestUploadDecisionVersusReleaseNoCloud, one handle more is opened than
// is released concurrently, so Release never reaches zero off the test
// goroutine. The inode stays clean and no backend method is called.
func (s *GoofysTest) TestDumpVersusReleaseNoCloud(t *C) {
	// Enough releases for the two goroutines to overlap.
	const releases = 2000
	// DumpThis writes one log line per call, so its loop is bounded by a count
	// and not by the releases.
	const dumps = 100
	// Not reached in a correct run: stops the test waiting for the goroutines.
	const waitLimit = 60 * time.Second

	backend := &TestBackend{err: syscall.ENOSYS}
	s.cloud = backend
	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	root := s.getRoot(t)
	inode := NewInode(s.fs, root, "dumped")
	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, inode)
	root.mu.Unlock()

	var openErr error
	fhs := make([]*FileHandle, 0, releases+1)
	for i := 0; i < releases+1; i++ {
		fh, err := inode.OpenFile()
		if err != nil {
			openErr = err
			break
		}
		fhs = append(fhs, fh)
	}
	if openErr != nil {
		for _, fh := range fhs {
			fh.Release()
		}
	}
	t.Assert(openErr, IsNil)

	begin := make(chan struct{})
	released := make(chan struct{})
	dumped := make(chan struct{})
	go func() {
		<-begin
		for _, fh := range fhs[:releases] {
			fh.Release()
		}
		close(released)
	}()
	go func() {
		<-begin
		for i := 0; i < dumps; i++ {
			inode.DumpThis(false)
		}
		close(dumped)
	}()
	close(begin)

	// One timer for both waits. A goroutine that has not finished may hold
	// inode.mu (DumpThis) or be releasing handles, so on that path nothing
	// more is done with the inode and the test fails.
	timeout := time.After(waitLimit)
	releasedInTime, dumpedInTime := false, false
	select {
	case <-released:
		releasedInTime = true
	case <-timeout:
	}
	if releasedInTime {
		select {
		case <-dumped:
			dumpedInTime = true
		case <-timeout:
		}
	}

	left := int32(-1)
	if releasedInTime && dumpedInTime {
		left = atomic.LoadInt32(&inode.fileHandles)
		fhs[releases].Release()
	}

	// No lock is held here: assert only now.
	t.Assert(releasedInTime, Equals, true)
	t.Assert(dumpedInTime, Equals, true)
	t.Assert(left, Equals, int32(1))
}

// TestCreateVersusEvictEntryNoCloud runs Inode.Create, which sets
// Inode.fileHandles of the new inode to one after fs.insertInode has made the
// inode reachable by id, against Goofys.EvictEntry, which reads the count of
// the inode it is given before it takes any inode lock.
//
// The race detector is the oracle. The asserts at the end hold whether
// CreateOrOpen stores the count atomically or not; only a -race run fails on a
// plain write. Its report has the write in CreateOrOpen on one side and, on the
// other, sync/atomic.LoadInt32 at the line of this test that calls EvictEntry:
// EvictEntry has no frame of its own in the report.
//
// Nothing is uploaded and no backend method is called: a created file with an
// open handle is one the upload decision declines, and each file is taken out
// of the dirty queue before its handle is released.
func (s *GoofysTest) TestCreateVersusEvictEntryNoCloud(t *C) {
	// Enough creations for the evicting goroutine to meet some of them between
	// insertInode and the store of the count.
	const creates = 200
	// Neither bound is reached in a correct run. The first stops the evicting
	// loop if the creations never finish; the second, longer, stops the test
	// waiting for the goroutines.
	const loopLimit = 30 * time.Second
	const waitLimit = 60 * time.Second

	backend := &TestBackend{err: syscall.ENOSYS}
	s.cloud = backend
	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	root := s.getRoot(t)
	// EvictEntry checks the child's count and state before it takes any lock
	// and does not check them again once it holds the locks; what it does
	// check there is the count of the parent directory. A handle on the
	// directory therefore keeps it from evicting a file that this test has
	// just created, whatever the interleaving.
	dh := root.OpenDir()

	type createdFile struct {
		inode *Inode
		fh    *FileHandle
	}
	// What each goroutine saw. It is sent to the test goroutine, which is the
	// only one that calls gocheck.
	type creations struct {
		files []createdFile
		err   error
	}
	type evictions struct {
		calls   int
		evicted int
		sawEnd  bool
	}

	begin := make(chan struct{})
	end := make(chan struct{})
	created := make(chan creations, 1)
	evicted := make(chan evictions, 1)
	go func() {
		<-begin
		var c creations
		for i := 0; i < creates; i++ {
			inode, fh, err := root.Create(fmt.Sprintf("created%v", i))
			if err != nil {
				c.err = err
				break
			}
			c.files = append(c.files, createdFile{inode, fh})
		}
		close(end)
		created <- c
	}()
	go func() {
		<-begin
		var e evictions
		deadline := time.Now().Add(loopLimit)
		for !e.sawEnd && time.Now().Before(deadline) {
			// The inode created last is the one whose count may not be
			// stored yet. Before the first creation this is the root, which
			// EvictEntry refuses.
			s.fs.mu.RLock() // nextInodeID is written under fs.mu
			newest := s.fs.nextInodeID - 1
			s.fs.mu.RUnlock()
			if s.fs.EvictEntry(newest) {
				e.evicted++
			}
			e.calls++
			select {
			case <-end:
				e.sawEnd = true
			default:
			}
		}
		evicted <- e
	}()
	close(begin)

	// One timer for both waits. A goroutine that has not finished may hold
	// root.mu or the lock of a new inode, so on that path the cleanup, which
	// takes both, is skipped and the test fails.
	timeout := time.After(waitLimit)
	var made creations
	var evictSeen evictions
	createdInTime, evictedInTime := false, false
	select {
	case made = <-created:
		createdInTime = true
	case <-timeout:
	}
	if createdInTime {
		select {
		case evictSeen = <-evicted:
			evictedInTime = true
		case <-timeout:
		}
	}

	var left []int32
	if createdInTime && evictedInTime {
		for _, f := range made.files {
			left = append(left, atomic.LoadInt32(&f.inode.fileHandles))
			// The file exists in the cache only. It leaves the dirty queue
			// before its handle is released: a created file with no open
			// handle is one the Flusher uploads.
			f.inode.mu.Lock()
			f.inode.SetCacheState(ST_CACHED)
			f.inode.mu.Unlock()
			f.fh.Release()
		}
		// CloseDir takes root.mu; called explicitly, never deferred, so that
		// a failed assert cannot run it.
		dh.CloseDir()
	}

	// Every lock is released and nothing is left for the Flusher: assert only
	// now.
	t.Assert(createdInTime, Equals, true)
	t.Assert(evictedInTime, Equals, true)
	t.Assert(made.err, IsNil)
	t.Assert(len(made.files), Equals, creates)
	t.Assert(evictSeen.sawEnd, Equals, true)
	t.Assert(evictSeen.calls > 0, Equals, true)
	t.Assert(evictSeen.evicted, Equals, 0)
	for i, n := range left {
		t.Assert(n, Equals, int32(1), Commentf("file %v", i))
	}
}
