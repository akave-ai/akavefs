package core

import (
	"context"
	"fmt"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jacobsa/fuse/fuseops"
	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// switchBackend is a StorageBackend whose delegate a test can replace while
// the filesystem runs.
//
// Tests used to replace the root's backend by assigning to the root's
// dir.cloud. Inode.cloud() reads that field without a lock the test could
// hold, from the flusher among others, so the assignment was a data race.
// With a switchBackend installed before the filesystem starts, dir.cloud is
// written once, in newGoofys, before any goroutine that reads it exists; what
// a test replaces afterwards is the delegate, behind an atomic pointer.
//
// It does not behave exactly as the assignment did. A backend call that has
// already entered the previous delegate finishes there. Every later call of
// the same filesystem operation goes to the new delegate - for example the
// remaining parts and the commit of a multipart upload - whereas an operation
// that had read dir.cloud kept that backend to its end. None of the tests
// that use this has dirty work spanning a swap.
type switchBackend struct {
	cur atomic.Pointer[StorageBackend]
}

func newSwitchBackend(b StorageBackend) *switchBackend {
	sw := &switchBackend{}
	sw.cur.Store(&b)
	return sw
}

// get returns the current delegate.
func (sw *switchBackend) get() StorageBackend {
	return *sw.cur.Load()
}

// swap installs b as the delegate and returns the one it replaced.
func (sw *switchBackend) swap(b StorageBackend) (old StorageBackend) {
	return *sw.cur.Swap(&b)
}

func (sw *switchBackend) Init(key string) error { return sw.get().Init(key) }

func (sw *switchBackend) Capabilities() *Capabilities { return sw.get().Capabilities() }

func (sw *switchBackend) Bucket() string { return sw.get().Bucket() }

func (sw *switchBackend) HeadBlob(param *HeadBlobInput) (*HeadBlobOutput, error) {
	return sw.get().HeadBlob(param)
}

func (sw *switchBackend) ListBlobs(param *ListBlobsInput) (*ListBlobsOutput, error) {
	return sw.get().ListBlobs(param)
}

func (sw *switchBackend) DeleteBlob(param *DeleteBlobInput) (*DeleteBlobOutput, error) {
	return sw.get().DeleteBlob(param)
}

func (sw *switchBackend) DeleteBlobs(param *DeleteBlobsInput) (*DeleteBlobsOutput, error) {
	return sw.get().DeleteBlobs(param)
}

func (sw *switchBackend) RenameBlob(param *RenameBlobInput) (*RenameBlobOutput, error) {
	return sw.get().RenameBlob(param)
}

func (sw *switchBackend) CopyBlob(param *CopyBlobInput) (*CopyBlobOutput, error) {
	return sw.get().CopyBlob(param)
}

func (sw *switchBackend) GetBlob(param *GetBlobInput) (*GetBlobOutput, error) {
	return sw.get().GetBlob(param)
}

func (sw *switchBackend) PutBlob(param *PutBlobInput) (*PutBlobOutput, error) {
	return sw.get().PutBlob(param)
}

func (sw *switchBackend) PatchBlob(param *PatchBlobInput) (*PatchBlobOutput, error) {
	return sw.get().PatchBlob(param)
}

func (sw *switchBackend) MultipartBlobBegin(param *MultipartBlobBeginInput) (*MultipartBlobCommitInput, error) {
	return sw.get().MultipartBlobBegin(param)
}

func (sw *switchBackend) MultipartBlobAdd(param *MultipartBlobAddInput) (*MultipartBlobAddOutput, error) {
	return sw.get().MultipartBlobAdd(param)
}

func (sw *switchBackend) MultipartBlobCopy(param *MultipartBlobCopyInput) (*MultipartBlobCopyOutput, error) {
	return sw.get().MultipartBlobCopy(param)
}

func (sw *switchBackend) MultipartBlobAbort(param *MultipartBlobCommitInput) (*MultipartBlobAbortOutput, error) {
	return sw.get().MultipartBlobAbort(param)
}

func (sw *switchBackend) MultipartBlobCommit(param *MultipartBlobCommitInput) (*MultipartBlobCommitOutput, error) {
	return sw.get().MultipartBlobCommit(param)
}

func (sw *switchBackend) MultipartExpire(param *MultipartExpireInput) (*MultipartExpireOutput, error) {
	return sw.get().MultipartExpire(param)
}

func (sw *switchBackend) RemoveBucket(param *RemoveBucketInput) (*RemoveBucketOutput, error) {
	return sw.get().RemoveBucket(param)
}

func (sw *switchBackend) MakeBucket(param *MakeBucketInput) (*MakeBucketOutput, error) {
	return sw.get().MakeBucket(param)
}

// Delegate answers for the current delegate and never returns the wrapper:
// OpenDir asks it whether the backend is S3, and must get the answer it got
// before the wrapper was there.
func (sw *switchBackend) Delegate() interface{} { return sw.get().Delegate() }

var _ StorageBackend = (*switchBackend)(nil)

// useSwitchBackend replaces the filesystem SetUpTest created with one whose
// root backend is a switchBackend, and returns that wrapper. A test that
// swaps the root backend (setS3, disableS3, or sw.swap) calls it before it
// uses s.fs for anything: the wrapper has to be in place before the
// filesystem's goroutines start, and inodes or handles taken from the earlier
// filesystem do not belong to the new one.
//
// The new filesystem has the same bucket, the same root mount prefix and the
// same flags pointer as the one it replaces, and its backend is built the way
// SetUpTest builds it.
func (s *GoofysTest) useSwitchBackend(t *C) *switchBackend {
	// newGoofys moved a ":prefix" out of the bucket name into the root's
	// mount prefix, so it is put back for the new filesystem to find.
	bucket, flags := s.fs.bucket, s.fs.flags
	if prefix := s.getRoot(t).dir.mountPrefix; prefix != "" {
		bucket += ":" + prefix
	}
	s.fs.Shutdown()
	var sw *switchBackend
	var err error
	// s.fs is assigned before err is asserted on: Shutdown closes a channel,
	// and TearDownTest would shut the old filesystem down a second time if a
	// failed assert left it in s.fs. It accepts a nil s.fs.
	s.fs, err = newGoofys(context.Background(), bucket, flags,
		func(bucket string, flags *cfg.FlagStorage) (StorageBackend, error) {
			cloud, err := NewBackend(bucket, flags)
			if err != nil {
				return nil, err
			}
			if hasEnv("EVENTUAL_CONSISTENCY") {
				cloud = NewS3BucketEventualConsistency(cloud.(*S3Backend))
			}
			sw = newSwitchBackend(cloud)
			return sw, nil
		})
	t.Assert(err, IsNil)
	return sw
}

// switchBackendOrPanic returns the switchBackend installed as the root
// backend. It panics when there is none instead of falling back to assigning
// dir.cloud, so that a test which swaps the backend without calling
// useSwitchBackend fails where it can be seen, not as a race report.
//
// LOCKS_EXCLUDED(s.fs.mu)
func (s *GoofysTest) switchBackendOrPanic() *switchBackend {
	s.fs.mu.RLock()
	root := s.fs.inodes[fuseops.RootInodeID]
	s.fs.mu.RUnlock()
	sw, ok := root.dir.cloud.(*switchBackend)
	if !ok {
		panic("call useSwitchBackend first")
	}
	return sw
}

// TestSetS3VersusCloudNoCloud replaces the root backend, through
// switchBackend.swap and through the setS3 helper, while another goroutine
// resolves the root's backend with Inode.cloud() as the flusher does.
//
// The race detector is the oracle for the concurrent part: a write of the
// root's dir.cloud while the reader runs is reported with that write, in swap
// or in setS3, on one side and the read in Inode.cloud() on the other. The
// asserts at the end check what the swaps returned and where calls went; a
// swap that also wrote dir.cloud would still pass them, and only a -race run
// fails on it. The reader keeps running through the two setS3 calls so that
// an assignment in setS3 itself is reported, not only one in swap.
//
// The test is slow for a fixture-free one: setS3 keeps its sleep, and it is
// called twice.
func (s *GoofysTest) TestSetS3VersusCloudNoCloud(t *C) {
	// Enough swaps for the two goroutines to overlap.
	const swaps = 300
	// Not reached in a correct run: the reader takes no lock that anything
	// here holds across a wait, and stops at its next iteration.
	const waitLimit = 30 * time.Second

	backend := &TestBackend{err: syscall.ENOSYS}
	sw := newSwitchBackend(backend)
	s.cloud = backend

	var err error
	// setS3 finds the filesystem through s.fs.
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return sw, nil
	})
	t.Assert(err, IsNil)
	root := s.getRoot(t)

	disabled := StorageBackendInitError{fmt.Errorf("cloud disabled"), *backend.Capabilities()}

	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			root.mu.Lock()
			root.cloud()
			root.mu.Unlock()
		}
	}()

	for i := 0; i < swaps; i++ {
		sw.swap(disabled)
		sw.swap(backend)
	}

	old := s.setS3(nil)
	root.mu.Lock()
	cloud, _ := root.cloud()
	root.mu.Unlock()
	_, disabledErr := cloud.HeadBlob(&HeadBlobInput{Key: "x"})

	prev := s.setS3(old)
	_, restoredErr := cloud.HeadBlob(&HeadBlobInput{Key: "x"})

	close(stop)
	stopped := true
	select {
	case <-done:
	case <-time.After(waitLimit):
		stopped = false
	}
	// TearDownTest leaves the filesystem of a NoCloud test alone. Clearing s.fs
	// keeps a later TearDownTest from shutting the same filesystem down twice.
	s.fs.Shutdown()
	s.fs = nil

	t.Assert(stopped, Equals, true)
	t.Assert(cloud == StorageBackend(sw), Equals, true)
	t.Assert(old == StorageBackend(backend), Equals, true)
	t.Assert(disabledErr, Equals, syscall.ENOENT)
	t.Assert(restoredErr, Equals, syscall.ENOSYS)
	_, isErr := prev.(StorageBackendInitError)
	t.Assert(isErr, Equals, true)
}
