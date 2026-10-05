//go:build !windows

package core

import (
	"context"
	"sync/atomic"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// TestClusterReadDirDotDotAfterInvalidationNoCloud reads "." through
// ClusterFs.readDir, invalidates the handle, and checks that the next read
// starts at "..".
//
// An invalidated handle finds its place again by the name of the last entry
// it returned. readDir used to record the directory's own name for a dot
// entry, which for the root is empty, so the search ran among the children
// and landed past "..".
//
// The first buffer holds exactly one entry, which is what stops readDir
// between "." and "..". The handle is invalidated the way the child mutators
// do it, by moving the directory's generation on; the directory has no
// children, so no mutator can be called on it here.
//
// As in the other fixture-free tests, asserts run only after every lock is
// released, and the handle is closed explicitly and not by a defer.
func (s *GoofysTest) TestClusterReadDirDotDotAfterInvalidationNoCloud(t *C) {
	list := func(param *ListBlobsInput) (*ListBlobsOutput, error) {
		return &ListBlobsOutput{}, nil
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

	// The fixed part of a directory entry: inode, offset, name length, type.
	const direntHeader = 24
	// A name of up to eight bytes is padded to eight.
	const oneDotEntry = direntHeader + 8

	// openDir and readDir are both REQUIRED_LOCK(inode.KeepOwnerLock).
	root.KeepOwnerLock()
	handleId := cfs.openDir(root)

	first := make([]byte, oneDotEntry)
	firstRead := 0
	firstErr := cfs.readDir(handleId, 0, first, &firstRead)

	atomic.AddUint64(&root.dir.generation, 1)

	second := make([]byte, 4096)
	secondRead := 0
	secondErr := cfs.readDir(handleId, 1, second, &secondRead)

	cfs.releaseDirHandle(handleId)
	root.KeepOwnerUnlock()

	t.Assert(firstErr, IsNil)
	t.Assert(firstRead, Equals, oneDotEntry)
	t.Assert(string(first[direntHeader:direntHeader+2]), Equals, ".\x00")

	t.Assert(secondErr, IsNil)
	t.Assert(secondRead, Equals, oneDotEntry)
	t.Assert(string(second[direntHeader:direntHeader+3]), Equals, "..\x00")
}
