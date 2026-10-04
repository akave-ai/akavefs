//go:build !windows

package core

import (
	"context"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// TestClusterReadDirListingErrorNoCloud makes a listing fail inside the read
// loop of ClusterFs.readDir, after loadChildren has succeeded, and checks that
// the error comes back with the handle's lock released exactly once.
//
// readDir releases dh.mu with a deferred unlock. A second, explicit unlock on
// that error path is not something an assert can catch: unlocking an unlocked
// sync.Mutex is a fatal runtime error, which ends the test binary and cannot
// be recovered. So a regression shows as the whole run dying with a stack
// through readDir, not as a failed assert here.
//
// readDir uses nothing of ClusterFs but its Goofys, so a ClusterFs wrapping
// the test's fs is enough: no peers, connections or gRPC server are involved.
//
// The backend's first page is truncated, which is what leaves the directory
// half-listed: loadChildren is satisfied by it, the loop serves the dot
// entries and the page's one child, and the next entry needs the page after
// it, which fails. The error is one that the listing retry loop does not
// retry, so the call cannot spin.
//
// As in the other fixture-free tests, asserts run only after every lock is
// released, and the handle is closed explicitly and not by a defer.
func (s *GoofysTest) TestClusterReadDirListingErrorNoCloud(t *C) {
	// The hook is defined here and keeps no state: it tells the pages apart by
	// the marker the listing passes, so it gives the same answers whoever calls
	// it and however often.
	list := func(param *ListBlobsInput) (*ListBlobsOutput, error) {
		if param.StartAfter == nil || *param.StartAfter == "" {
			return &ListBlobsOutput{
				Items:       []BlobItemOutput{{Key: PString("a")}},
				IsTruncated: true,
			}, nil
		}
		return nil, syscall.EACCES
	}
	backend := &TestBackend{err: syscall.ENOSYS, ListBlobsFunc: list}
	s.cloud = backend

	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)
	root := s.getRoot(t)

	cfs := &ClusterFs{Goofys: s.fs}

	// openDir and readDir are both REQUIRED_LOCK(inode.KeepOwnerLock).
	root.KeepOwnerLock()
	handleId := cfs.openDir(root)

	s.fs.mu.RLock()
	dh := s.fs.dirHandles[handleId]
	s.fs.mu.RUnlock()

	dst := make([]byte, 4096)
	bytesRead := 0
	readErr := cfs.readDir(handleId, 0, dst, &bytesRead)

	// TryLock and not Lock: a handle left locked must fail the test, not hang it.
	unlocked := dh.mu.TryLock()
	if unlocked {
		dh.mu.Unlock()
	}

	cfs.releaseDirHandle(handleId)
	root.KeepOwnerUnlock()

	t.Assert(readErr, Equals, syscall.EACCES)
	// loadChildren returns before anything is written, so entries in dst mean
	// the error came from the loop.
	t.Assert(bytesRead > 0, Equals, true)
	t.Assert(unlocked, Equals, true)
}
