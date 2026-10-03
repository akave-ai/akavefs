package core

import (
	"context"
	"fmt"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// The tests in this file cover how an open directory handle keeps its place
// while the directory changes under it. They are fixture-free (NoCloud), so
// they run without s3proxy and under the race detector.
//
// Two rules hold throughout, because a failed assert unwinds the test while
// whatever it held stays held and the suite's teardown then blocks on it:
// asserts run only after every fs lock is released, and CloseDir, which takes
// the inode lock, is called explicitly and never deferred.

// newDirHandleGenerationRootNoCloud builds a Goofys over a TestBackend that
// answers listings with list (an empty listing when list is nil) and returns
// its root, holding one child per name.
//
// A test that needs its own listing defines it inside the test method: a -race
// report is identified by the top frames of its two accesses, so a hook defined
// elsewhere reports a frame with no baseline and reads as a new race.
func (s *GoofysTest) newDirHandleGenerationRootNoCloud(t *C, list func(*ListBlobsInput) (*ListBlobsOutput, error), names ...string) *Inode {
	if list == nil {
		list = func(*ListBlobsInput) (*ListBlobsOutput, error) { return &ListBlobsOutput{}, nil }
	}
	backend := &TestBackend{err: syscall.ENOSYS, ListBlobsFunc: list}
	s.cloud = backend

	var err error
	s.fs, err = newGoofys(context.Background(), "test", cfg.DefaultFlags(), func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	root := s.getRoot(t)
	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	for _, name := range names {
		s.fs.insertInode(root, NewInode(s.fs, root, name))
	}
	// A fresh DirTime makes ReadDir serve from cache instead of listing, so a
	// test decides for itself whether and when the backend is listed.
	root.dir.DirTime = time.Now()
	root.mu.Unlock()
	return root
}

// newDirHandleGenerationSubdirNoCloud is newDirHandleGenerationRootNoCloud for
// a directory below the root: it returns the root and a directory "sub" in it
// that holds one child per name. The root's name is empty and it has no parent,
// so a handle on it does not behave like a handle on any other directory.
func (s *GoofysTest) newDirHandleGenerationSubdirNoCloud(t *C, list func(*ListBlobsInput) (*ListBlobsOutput, error), names ...string) (root, sub *Inode) {
	root = s.newDirHandleGenerationRootNoCloud(t, list)
	sub = NewInode(s.fs, root, "sub")
	sub.ToDir()
	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, sub)
	root.mu.Unlock()

	sub.mu.Lock()
	for _, name := range names {
		s.fs.insertInode(sub, NewInode(s.fs, sub, name))
	}
	// As for the root: a fresh DirTime keeps ReadDir from listing.
	sub.dir.DirTime = time.Now()
	sub.mu.Unlock()
	return root, sub
}

// rewindDuringListingNoCloud reads the two dot entries from dh, a handle on
// dir, expires dir so that the next read lists it, and reads on to the end.
// The test's listing hook is what moves the handle back during that listing, in
// the tests that rewind; a hook that leaves the handle alone gets a plain read
// across a listing.
//
// A panic in the second part is recovered and returned, so that the caller can
// release what it holds and then fail on it, instead of the panic unwinding the
// test with dh.mu held.
//
// LOCKS_REQUIRED(dh.mu)
func rewindDuringListingNoCloud(dh *DirHandle, dir *Inode) (dots, rest []string, panicked interface{}, err error) {
	const dotEntries = 2
	const maxEntries = 10

	dots, err = readDirHandleNoCloud(dh, dotEntries)
	if err != nil {
		return
	}

	dir.mu.Lock()
	dir.dir.DirTime = time.Time{}
	dir.mu.Unlock()

	defer func() { panicked = recover() }()
	rest, err = readDirHandleNoCloud(dh, maxEntries)
	return
}

// readDirHandleNoCloud reads up to max entries from dh and returns their inode
// names. It passes the inode name to Next, as RefreshInodeCache and
// ClusterFs.readDir do, so on the root the two dot entries read as "".
//
// The cap only bounds a handle that never reaches the end; a correct run stops
// at the nil entry well before it.
//
// LOCKS_REQUIRED(dh.mu)
func readDirHandleNoCloud(dh *DirHandle, max int) (names []string, err error) {
	for len(names) < max {
		en, err := dh.ReadDir()
		if err != nil {
			return names, err
		}
		if en == nil {
			break
		}
		names = append(names, en.Name)
		dh.Next(en.Name)
	}
	return names, nil
}

// There are no asserts here: the race detector is the oracle. Next runs under
// dh.mu only and the child mutators under the parent's lock only, so anything a
// mutator writes into the handle directly is reported as a data race.
//
// The mutators exercised are insertChildUnlocked on its mid-slice path, which is
// the insert that shifts positions, and removeChildUnlocked. The inserted names
// sort between the two existing children for that reason: a name sorting last
// would take the append path, which never touches handles.
// removeAllChildrenUnlocked has its own loop in the next test.
func (s *GoofysTest) TestDirHandleNextVersusChildChangeNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	const rounds = 200
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < rounds; i++ {
			dh.mu.Lock()
			dh.Next("b")
			dh.mu.Unlock()
		}
	}()

	for i := 0; i < rounds; i++ {
		root.mu.Lock()
		child := NewInode(s.fs, root, fmt.Sprintf("c%04d", i))
		s.fs.insertInode(root, child)
		child.mu.Lock() // removeChildUnlocked is LOCKS_REQUIRED(inode.mu)
		root.removeChildUnlocked(child)
		child.mu.Unlock()
		root.mu.Unlock()
	}

	<-done
	dh.CloseDir()
}

// The same as the test above, for removeAllChildrenUnlocked: no asserts, the
// race detector is the oracle. Each round puts one child back afterwards, so
// that removing everything always has a child to drop.
func (s *GoofysTest) TestDirHandleNextVersusRemoveAllNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	const rounds = 200
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < rounds; i++ {
			dh.mu.Lock()
			dh.Next("b")
			dh.mu.Unlock()
		}
	}()

	for i := 0; i < rounds; i++ {
		root.mu.Lock() // both calls are LOCKS_REQUIRED(parent.mu)
		root.removeAllChildrenUnlocked()
		s.fs.insertInode(root, NewInode(s.fs, root, "b"))
		root.mu.Unlock()
	}

	<-done
	dh.CloseDir()
}

// A flat listing that inserts a child before the handle's position must not
// make the handle return an entry twice.
func (s *GoofysTest) TestDirHandleFlatListingKeepsPositionNoCloud(t *C) {
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		return &ListBlobsOutput{Items: []BlobItemOutput{
			{Key: PString("a")}, {Key: PString("b")}, {Key: PString("d")},
		}}, nil
	}
	root := s.newDirHandleGenerationRootNoCloud(t, list, "b", "d")
	dh := root.OpenDir()

	var got []string
	var readErr, listErr error

	dh.mu.Lock()
	// The first two entries are the directory itself and its parent, which
	// ReadDir returns as inodes; the FUSE layer is what names them.
	for _, dot := range []string{".", "..", ""} {
		en, err := dh.ReadDir()
		if err != nil || en == nil {
			readErr = err
			break
		}
		name := dot
		if name == "" {
			name = en.Name
		}
		got = append(got, name)
		dh.Next(name)
	}

	// Make the directory look unlisted, so the listing below runs and seals it.
	root.mu.Lock()
	root.dir.listDone = false
	root.dir.lastFromCloud = nil
	root.mu.Unlock()

	// This is the path ClusterFs takes: loadChildren in cluster_fs.go calls
	// listObjectsFlat directly, with no checkDirPosition between the listing
	// and the next ReadDir other than the one ReadDir itself starts with.
	_, listErr = dh.listObjectsFlat()

	// The cap only bounds a handle that never reaches the end; a correct run
	// stops at the nil entry well before it.
	const maxEntries = 10
	for readErr == nil && listErr == nil && len(got) < maxEntries {
		en, err := dh.ReadDir()
		if err != nil {
			readErr = err
			break
		}
		if en == nil {
			break
		}
		got = append(got, en.Name)
		dh.Next(en.Name)
	}
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(listErr, IsNil)
	t.Assert(readErr, IsNil)
	t.Assert(got, DeepEquals, []string{".", "..", "b", "d"})
}

// Sealing a directory whose children did not change must not invalidate its
// open handles.
//
// A handle on the root that has returned only "." is what shows it. Its callers
// pass the root's empty name to Next, and an invalidated handle re-finds its
// place from that name, which lands past "..". So a needless invalidation here
// makes the handle skip "..", while an untouched handle returns it.
func (s *GoofysTest) TestDirHandleNoopSealKeepsDotEntriesNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	dh.mu.Lock()
	first, firstErr := dh.ReadDir()
	if first != nil {
		dh.Next(first.Name)
	}

	root.mu.Lock() // sealDir is LOCKS_REQUIRED(inode.mu)
	root.sealDir()
	root.mu.Unlock()

	second, secondErr := dh.ReadDir()
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(firstErr, IsNil)
	t.Assert(secondErr, IsNil)
	// The first entry of a directory is the directory itself ("."), and the
	// root has no parent, so its ".." is the root as well.
	t.Assert(first == root, Equals, true)
	t.Assert(second == root, Equals, true)
}

// listObjectsFlat must keep dh.mu for as long as it holds the inode lock. A
// second user of the same handle takes dh.mu and then the inode lock, which is
// the documented order, so releasing and retaking dh.mu under the inode lock
// lets the two block each other for good.
func (s *GoofysTest) TestDirHandleSealKeepsLockOrderNoCloud(t *C) {
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		return &ListBlobsOutput{Items: []BlobItemOutput{{Key: PString("d")}}}, nil
	}
	root := s.newDirHandleGenerationRootNoCloud(t, list, "b", "d")

	root.mu.Lock()
	root.dir.listDone = false
	root.dir.lastFromCloud = nil
	// "b" is older than the refresh and missing from the listing, so the seal's
	// removeExpired drops it and calls NotifyCallback with root.mu held. That
	// callback is the only hook inside the window under test.
	root.dir.refreshStartTime = time.Now()
	root.findChildUnlocked("b").AttrTime = time.Now().Add(-time.Hour)
	root.mu.Unlock()

	dh := root.OpenDir()

	// The callback's wait has to stay well below the test's wait for the
	// driver, so a correct run is never mistaken for a deadlock.
	const callbackWait = 200 * time.Millisecond
	const driverWait = 10 * time.Second

	secondHasHandle := make(chan struct{})
	secondDone := make(chan struct{})
	// The callback runs on the driver goroutine and the second user on its own,
	// so neither may touch t: they report through channels only.
	s.fs.NotifyCallback = func([]interface{}) {
		go func() {
			defer close(secondDone)
			dh.mu.Lock()
			close(secondHasHandle)
			root.mu.Lock() // dh.mu before dh.inode.mu, the documented order
			root.mu.Unlock()
			dh.mu.Unlock()
		}()
		// Give the second user time to get dh.mu if the seal let go of it. When
		// the seal keeps dh.mu this wait simply runs out.
		select {
		case <-secondHasHandle:
		case <-time.After(callbackWait):
		}
	}

	driverDone := make(chan struct{})
	go func() {
		defer close(driverDone)
		dh.mu.Lock()
		dh.listObjectsFlat()
		dh.mu.Unlock()
	}()

	// No lock is held here, so failing is safe. The handle is deliberately not
	// closed on this path: the driver may still hold the inode lock.
	select {
	case <-driverDone:
	case <-time.After(driverWait):
		t.Fatal("listObjectsFlat did not return: it is blocked against a second user of the handle")
	}

	// The second user only exists if the callback ran, so waiting for it also
	// shows the seal really removed a child with root.mu held.
	select {
	case <-secondDone:
	case <-time.After(driverWait):
		t.Fatal("the second user of the handle never finished, or the seal never called NotifyCallback")
	}

	dh.CloseDir()
}

// A change to the directory between opening a handle and its first read must
// not make the handle skip "." and "..".
func (s *GoofysTest) TestDirHandleChangeBeforeFirstReadKeepsDotEntriesNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	// "a" sorts before the existing children, so it shifts every position.
	s.fs.insertInode(root, NewInode(s.fs, root, "a"))
	root.mu.Unlock()

	dh.mu.Lock()
	en, err := dh.ReadDir()
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(err, IsNil)
	// The first entry of a directory is the directory itself (".").
	t.Assert(en == root, Equals, true)
}

// Refreshing the root lists it through a handle that passes the root's empty
// name to Next for "." and "..". A listing that changes the children at that
// point invalidates the handle while its last name is empty.
//
// Only the outcome of the refresh is checked here: it must not panic and must
// return no error. That such a handle carries on with the children instead of
// starting over at "." is pinned by TestDirHandleRootChangeAfterDotEntriesNoCloud.
func (s *GoofysTest) TestDirHandleRootRefreshAfterChangeNoCloud(t *C) {
	// Each case is the whole key set the backend holds; the root starts with
	// "b" and "d". The first adds a key that sorts before an existing child.
	// The second lacks "b", which the refresh has aged along with every other
	// child, so the listing expires it.
	for _, keys := range [][]string{{"a", "b", "d"}, {"d"}} {
		list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
			out := &ListBlobsOutput{}
			for _, key := range keys {
				out.Items = append(out.Items, BlobItemOutput{Key: PString(key)})
			}
			return out, nil
		}
		root := s.newDirHandleGenerationRootNoCloud(t, list, "b", "d")

		// A panic is caught here instead of being left to unwind the test, so
		// that it is reported as what it is and with its value.
		var panicked interface{}
		var err error
		func() {
			defer func() { panicked = recover() }()
			err = s.fs.RefreshInodeCache(root)
		}()

		t.Assert(panicked, IsNil)
		t.Assert(err, IsNil)
	}
}

// A change to the root after a handle has returned "." and ".." must not make
// the handle return them again.
func (s *GoofysTest) TestDirHandleRootChangeAfterDotEntriesNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	const dotEntries = 2
	const maxEntries = 10

	dh.mu.Lock()
	dots, dotsErr := readDirHandleNoCloud(dh, dotEntries)

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	// "a" sorts before the existing children, so it shifts every position.
	s.fs.insertInode(root, NewInode(s.fs, root, "a"))
	root.mu.Unlock()

	rest, restErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(dotsErr, IsNil)
	t.Assert(restErr, IsNil)
	// Both dot entries of the root are the root, whose name is empty.
	t.Assert(dots, DeepEquals, []string{"", ""})
	t.Assert(rest, DeepEquals, []string{"a", "b", "d"})
}

// Removing a child the handle has already passed must not make the handle skip
// the entry that follows its position.
func (s *GoofysTest) TestDirHandleRemoveEarlierChildKeepsPositionNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "a", "b", "d")
	dh := root.OpenDir()

	const upToB = 4 // ".", "..", "a", "b"
	const maxEntries = 10

	dh.mu.Lock()
	head, headErr := readDirHandleNoCloud(dh, upToB)

	root.mu.Lock()
	child := root.findChildUnlocked("a")
	child.mu.Lock() // removeChildUnlocked is LOCKS_REQUIRED(inode.mu)
	root.removeChildUnlocked(child)
	child.mu.Unlock()
	root.mu.Unlock()

	rest, restErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(headErr, IsNil)
	t.Assert(restErr, IsNil)
	t.Assert(head, DeepEquals, []string{"", "", "a", "b"})
	t.Assert(rest, DeepEquals, []string{"d"})
}

// Removing every child must take open handles off their old position: the
// listing ends there, and children created afterwards are listed from where the
// handle's last name sorts, not from the stale position.
func (s *GoofysTest) TestDirHandleRemoveAllResetsPositionNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "a", "b", "d")
	dh := root.OpenDir()

	const upToB = 4 // ".", "..", "a", "b"
	const maxEntries = 10

	dh.mu.Lock()
	head, headErr := readDirHandleNoCloud(dh, upToB)

	root.mu.Lock() // removeAllChildrenUnlocked is LOCKS_REQUIRED(parent.mu)
	root.removeAllChildrenUnlocked()
	root.mu.Unlock()

	afterRemoveAll, afterRemoveAllErr := readDirHandleNoCloud(dh, maxEntries)

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	// Both names sort after "b", the last name the handle returned. Neither
	// insert moves an existing child, so neither invalidates handles: only what
	// removeAllChildrenUnlocked did can take the handle off its old position.
	s.fs.insertInode(root, NewInode(s.fs, root, "c"))
	s.fs.insertInode(root, NewInode(s.fs, root, "e"))
	root.mu.Unlock()

	rest, restErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(headErr, IsNil)
	t.Assert(afterRemoveAllErr, IsNil)
	t.Assert(restErr, IsNil)
	t.Assert(head, DeepEquals, []string{"", "", "a", "b"})
	t.Assert(afterRemoveAll, HasLen, 0)
	t.Assert(rest, DeepEquals, []string{"c", "e"})
}

// A handle that was opened before the directory changed and is first read after
// it must still list the whole directory, dot entries included. The same goes
// for the handles the filesystem opens for itself, which start past the dot
// entries: isEmptyDir is one of them.
func (s *GoofysTest) TestDirHandleFirstReadAfterChangeListsAllNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	// "a" sorts before the existing children, so it shifts every position.
	s.fs.insertInode(root, NewInode(s.fs, root, "a"))
	root.mu.Unlock()

	const maxEntries = 10

	dh.mu.Lock()
	got, gotErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	root.mu.Lock() // removeAllChildrenUnlocked is LOCKS_REQUIRED(parent.mu)
	root.removeAllChildrenUnlocked()
	root.mu.Unlock()

	// The directory has changed again and is empty now. A handle that started
	// over at "." here would report the directory itself as an entry.
	empty, emptyErr := root.isEmptyDir()

	t.Assert(gotErr, IsNil)
	t.Assert(emptyErr, IsNil)
	// Both dot entries of the root are the root, whose name is empty.
	t.Assert(got, DeepEquals, []string{"", "", "a", "b", "d"})
	t.Assert(empty, Equals, true)
}

// ReadDir lets go of dh.mu while it lists the directory. A second user of the
// handle that rewinds it in that window must get the listing from the start,
// "." first: the rewound position is below the first child, and ReadDir must
// not carry on to index the children with it.
//
// Here the listing changes nothing in the directory, so nothing invalidates
// the handle and the rewound position reaches ReadDir exactly as Seek left it.
func (s *GoofysTest) TestDirHandleRewindDuringListingNoCloud(t *C) {
	var dh *DirHandle
	rewound := false
	// The listing runs on the reading goroutine, in the window where ReadDir
	// does not hold dh.mu. TryLock, so that a ReadDir which kept dh.mu fails
	// the test below instead of blocking it here.
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		if !rewound && dh.mu.TryLock() {
			dh.Seek(0)
			dh.mu.Unlock()
			rewound = true
		}
		return &ListBlobsOutput{Items: []BlobItemOutput{
			{Key: PString("b")}, {Key: PString("d")},
		}}, nil
	}
	root := s.newDirHandleGenerationRootNoCloud(t, list, "b", "d")
	dh = root.OpenDir()

	dh.mu.Lock()
	dots, rest, panicked, err := rewindDuringListingNoCloud(dh, root)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(panicked, IsNil)
	t.Assert(err, IsNil)
	t.Assert(rewound, Equals, true)
	// Both dot entries of the root are the root, whose name is empty.
	t.Assert(dots, DeepEquals, []string{"", ""})
	t.Assert(rest, DeepEquals, []string{"", "", "b", "d"})
}

// The same rewind as in the test above, during a listing that adds a child
// before the existing ones and so invalidates the handle as well. The handle is
// then re-found from a state that says nothing has been returned yet, and
// ReadDir must cope with that position after the listing too.
//
// Only the children are compared: whether a handle rewound in that window
// starts over with the dot entries is left to the test above.
func (s *GoofysTest) TestDirHandleRewindDuringChangingListingNoCloud(t *C) {
	var dh *DirHandle
	rewound := false
	// See the test above for why this is a TryLock.
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		if !rewound && dh.mu.TryLock() {
			dh.Seek(0)
			dh.mu.Unlock()
			rewound = true
		}
		return &ListBlobsOutput{Items: []BlobItemOutput{
			{Key: PString("a")}, {Key: PString("b")}, {Key: PString("d")},
		}}, nil
	}
	root := s.newDirHandleGenerationRootNoCloud(t, list, "b", "d")
	dh = root.OpenDir()

	dh.mu.Lock()
	dots, rest, panicked, err := rewindDuringListingNoCloud(dh, root)
	dh.mu.Unlock()
	dh.CloseDir()

	var children []string
	for _, name := range rest {
		if name != "" {
			children = append(children, name)
		}
	}

	t.Assert(panicked, IsNil)
	t.Assert(err, IsNil)
	t.Assert(rewound, Equals, true)
	t.Assert(dots, DeepEquals, []string{"", ""})
	t.Assert(children, DeepEquals, []string{"a", "b", "d"})
}

// A new child that sorts after every existing one moves no position, so it
// must not invalidate open handles. The detector is the one the no-op seal test
// uses: a root handle that has returned only "." skips ".." when invalidated.
func (s *GoofysTest) TestDirHandleAppendKeepsDotEntriesNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "b", "d")
	dh := root.OpenDir()

	const oneEntry = 1

	dh.mu.Lock()
	first, firstErr := readDirHandleNoCloud(dh, oneEntry)

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, NewInode(s.fs, root, "e"))
	root.mu.Unlock()

	second, secondErr := dh.ReadDir()
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(firstErr, IsNil)
	t.Assert(secondErr, IsNil)
	t.Assert(first, DeepEquals, []string{""})
	// The root has no parent, so its ".." is the root as well.
	t.Assert(second == root, Equals, true)
}

// The first child of an empty directory moves no position either, so it must
// not invalidate open handles. Same detector as in the test above.
func (s *GoofysTest) TestDirHandleFirstChildKeepsDotEntriesNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil)
	dh := root.OpenDir()

	const oneEntry = 1

	dh.mu.Lock()
	first, firstErr := readDirHandleNoCloud(dh, oneEntry)

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	s.fs.insertInode(root, NewInode(s.fs, root, "b"))
	root.mu.Unlock()

	second, secondErr := dh.ReadDir()
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(firstErr, IsNil)
	t.Assert(secondErr, IsNil)
	t.Assert(first, DeepEquals, []string{""})
	// The root has no parent, so its ".." is the root as well.
	t.Assert(second == root, Equals, true)
}

// A second user can also seek the handle to just past "." while ReadDir lists
// the directory. The entry that follows is then "..", which on a directory
// below the root is its parent and not the directory itself.
func (s *GoofysTest) TestDirHandleSubdirSeekDuringListingNoCloud(t *C) {
	var dh *DirHandle
	var sub *Inode
	sought := false
	const pastDot = 1
	// Seek to a non-zero offset takes the directory's lock itself, so both
	// locks are tried first: a ReadDir that kept either one during the listing
	// then fails the test below instead of blocking it here.
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		if !sought && dh.mu.TryLock() {
			if sub.mu.TryLock() {
				sub.mu.Unlock()
				dh.Seek(pastDot)
				sought = true
			}
			dh.mu.Unlock()
		}
		return &ListBlobsOutput{Items: []BlobItemOutput{
			{Key: PString("sub/b")}, {Key: PString("sub/d")},
		}}, nil
	}
	_, sub = s.newDirHandleGenerationSubdirNoCloud(t, list, "b", "d")
	dh = sub.OpenDir()

	dh.mu.Lock()
	dots, rest, panicked, err := rewindDuringListingNoCloud(dh, sub)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(panicked, IsNil)
	t.Assert(err, IsNil)
	t.Assert(sought, Equals, true)
	// "." reads as the directory's own name and ".." as the root's empty one.
	t.Assert(dots, DeepEquals, []string{"sub", ""})
	t.Assert(rest, DeepEquals, []string{"", "b", "d"})
}

// The rewind to the very start, on a directory below the root. There "." and
// ".." are different inodes, so this is where a handle that answers the rewound
// position with the parent instead of the directory itself shows: the names
// alone would not tell on the root, and the inodes are what is compared.
func (s *GoofysTest) TestDirHandleSubdirRewindDuringListingNoCloud(t *C) {
	var dh *DirHandle
	rewound := false
	// The listing runs on the reading goroutine, in the window where ReadDir
	// does not hold dh.mu. TryLock, so that a ReadDir which kept dh.mu fails
	// the test below instead of blocking it here.
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		if !rewound && dh.mu.TryLock() {
			dh.Seek(0)
			dh.mu.Unlock()
			rewound = true
		}
		return &ListBlobsOutput{Items: []BlobItemOutput{
			{Key: PString("sub/b")}, {Key: PString("sub/d")},
		}}, nil
	}
	root, sub := s.newDirHandleGenerationSubdirNoCloud(t, list, "b", "d")
	dh = sub.OpenDir()

	const dotEntries = 2
	const maxEntries = 10

	var rest []*Inode
	var restErr error
	var panicked interface{}

	dh.mu.Lock()
	dots, dotsErr := readDirHandleNoCloud(dh, dotEntries)
	if dotsErr == nil {
		sub.mu.Lock()
		sub.dir.DirTime = time.Time{} // expired, so the next read lists
		sub.mu.Unlock()

		// A panic is recovered so that dh.mu is released before the test fails
		// on it. The cap only bounds a handle that never reaches the end.
		func() {
			defer func() { panicked = recover() }()
			for len(rest) < maxEntries {
				en, err := dh.ReadDir()
				if err != nil {
					restErr = err
					return
				}
				if en == nil {
					return
				}
				rest = append(rest, en)
				dh.Next(en.Name)
			}
		}()
	}
	dh.mu.Unlock()
	dh.CloseDir()

	var names []string
	for _, en := range rest {
		names = append(names, en.Name)
	}

	t.Assert(dotsErr, IsNil)
	t.Assert(panicked, IsNil)
	t.Assert(restErr, IsNil)
	t.Assert(rewound, Equals, true)
	t.Assert(dots, DeepEquals, []string{"sub", ""})
	// The length is checked first so that the two entries below exist.
	t.Assert(rest, HasLen, 4)
	t.Assert(rest[0] == sub, Equals, true)
	t.Assert(rest[1] == root, Equals, true)
	// "." reads as the directory's own name and ".." as the root's empty one.
	t.Assert(names, DeepEquals, []string{"sub", "", "b", "d"})
}

// A listing that nobody rewinds the handle during must leave it where it was:
// a handle that has returned "." and ".." gets the first child next, not a dot
// entry again.
func (s *GoofysTest) TestDirHandleSubdirListingKeepsFirstChildNoCloud(t *C) {
	listed := false
	list := func(*ListBlobsInput) (*ListBlobsOutput, error) {
		listed = true
		return &ListBlobsOutput{Items: []BlobItemOutput{
			{Key: PString("sub/b")}, {Key: PString("sub/d")},
		}}, nil
	}
	_, sub := s.newDirHandleGenerationSubdirNoCloud(t, list, "b", "d")
	dh := sub.OpenDir()

	dh.mu.Lock()
	dots, rest, panicked, err := rewindDuringListingNoCloud(dh, sub)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(panicked, IsNil)
	t.Assert(err, IsNil)
	// Without a listing the read would not have gone through the code that
	// re-checks the position after one.
	t.Assert(listed, Equals, true)
	t.Assert(dots, DeepEquals, []string{"sub", ""})
	t.Assert(rest, DeepEquals, []string{"b", "d"})
}

// The change-before-first-read case on a directory below the root: the handle
// must still start with the directory itself and its parent.
func (s *GoofysTest) TestDirHandleSubdirChangeBeforeFirstReadNoCloud(t *C) {
	_, sub := s.newDirHandleGenerationSubdirNoCloud(t, nil, "b", "d")
	dh := sub.OpenDir()

	sub.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	// "a" sorts before the existing children, so it shifts every position.
	s.fs.insertInode(sub, NewInode(s.fs, sub, "a"))
	sub.mu.Unlock()

	const maxEntries = 10

	dh.mu.Lock()
	got, gotErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(gotErr, IsNil)
	// "." reads as the directory's own name and ".." as the root's empty one.
	t.Assert(got, DeepEquals, []string{"sub", "", "a", "b", "d"})
}

// The remove-an-earlier-child case on a directory below the root: the handle
// must not skip the entry that follows its position.
func (s *GoofysTest) TestDirHandleSubdirRemoveEarlierChildNoCloud(t *C) {
	_, sub := s.newDirHandleGenerationSubdirNoCloud(t, nil, "a", "b", "d")
	dh := sub.OpenDir()

	const upToB = 4 // ".", "..", "a", "b"
	const maxEntries = 10

	dh.mu.Lock()
	head, headErr := readDirHandleNoCloud(dh, upToB)

	sub.mu.Lock()
	child := sub.findChildUnlocked("a")
	child.mu.Lock() // removeChildUnlocked is LOCKS_REQUIRED(inode.mu)
	sub.removeChildUnlocked(child)
	child.mu.Unlock()
	sub.mu.Unlock()

	rest, restErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(headErr, IsNil)
	t.Assert(restErr, IsNil)
	t.Assert(head, DeepEquals, []string{"sub", "", "a", "b"})
	t.Assert(rest, DeepEquals, []string{"d"})
}

// Seeking a handle into the middle of a directory that changed after the handle
// was opened must list from the entry sought to, as if nothing had changed
// before the seek.
func (s *GoofysTest) TestDirHandleSeekAfterChangeListsRestNoCloud(t *C) {
	root := s.newDirHandleGenerationRootNoCloud(t, nil, "a", "b", "d")
	dh := root.OpenDir()

	root.mu.Lock() // insertInode is LOCKS_REQUIRED(parent.mu)
	// "c" sorts between existing children, so it shifts a position.
	s.fs.insertInode(root, NewInode(s.fs, root, "c"))
	root.mu.Unlock()

	const pastB = 4 // ".", "..", "a", "b"
	const maxEntries = 10

	dh.mu.Lock()
	dh.Seek(pastB)
	got, gotErr := readDirHandleNoCloud(dh, maxEntries)
	dh.mu.Unlock()
	dh.CloseDir()

	t.Assert(gotErr, IsNil)
	t.Assert(got, DeepEquals, []string{"c", "d"})
}
