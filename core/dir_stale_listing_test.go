package core

import (
	"sync"
	"syscall"
	"time"

	. "gopkg.in/check.v1"
)

// The rules of dir_handle_generation_test.go hold here as well: asserts run
// only after every fs lock is released, and CloseDir is called explicitly and
// never deferred.

// Two handles on one unlisted directory each request its first page, because
// nothing marks a listing as in flight. The answer that arrives second is a
// duplicate of a page the directory has already moved past. Neither handle may
// lose an entry to it.
//
// The backend parks both requests, so the test decides the order: the first
// handle's answer is applied and that handle reads to the end of the page, then
// the second handle's answer arrives. A duplicate that is applied says again
// that the cache holds listed entries up to the end of that page; the first
// handle already stands there, so it takes the end of the cache for the end of
// the directory and returns a listing cut on the page boundary, with no error.
func (s *GoofysTest) TestDirHandleDuplicatePageKeepsListingNoCloud(t *C) {
	// Bounds every wait below, so that a regression fails the test instead of
	// hanging the suite. A correct run waits for nothing but the goroutines.
	const wait = 10 * time.Second
	// A correct run lists four times. The cap ends a caller that would
	// otherwise keep asking for a page it never gets applied.
	const maxCalls = 20
	// Enough for every entry of the directory; a correct run stops at the nil
	// entry before it.
	const maxEntries = 10

	pages := map[string]*ListBlobsOutput{
		"":  {Items: []BlobItemOutput{{Key: PString("a")}, {Key: PString("b")}}, IsTruncated: true},
		"b": {Items: []BlobItemOutput{{Key: PString("c")}, {Key: PString("d")}}, IsTruncated: true},
		"d": {Items: []BlobItemOutput{{Key: PString("e")}, {Key: PString("f")}}},
	}

	// The first two requests for the first page are held until the test lets
	// each one go; arrived tells the test that the request is at the backend.
	arrived := []chan struct{}{make(chan struct{}), make(chan struct{})}
	release := []chan struct{}{make(chan struct{}), make(chan struct{})}

	var mu sync.Mutex
	var requested []string
	firstPageCalls := 0

	// The hook runs on the reading goroutines, so it must not touch t.
	list := func(param *ListBlobsInput) (*ListBlobsOutput, error) {
		startAfter := NilStr(param.StartAfter)

		mu.Lock()
		requested = append(requested, startAfter)
		calls := len(requested)
		parked := -1
		if startAfter == "" {
			if firstPageCalls < len(release) {
				parked = firstPageCalls
			}
			firstPageCalls++
		}
		mu.Unlock()

		// EINVAL because it is one of the errors a listing is not retried on.
		if calls > maxCalls {
			return nil, syscall.EINVAL
		}
		if parked >= 0 {
			close(arrived[parked])
			select {
			case <-release[parked]:
			case <-time.After(wait):
				return nil, syscall.EINVAL
			}
		}

		page := pages[startAfter]
		if page == nil {
			return nil, syscall.EINVAL
		}
		// A copy, because the listing code cuts and appends to what it is given.
		return &ListBlobsOutput{
			Items:       append([]BlobItemOutput(nil), page.Items...),
			IsTruncated: page.IsTruncated,
		}, nil
	}

	root := s.newDirHandleGenerationRootNoCloud(t, list)
	root.mu.Lock()
	// The helper leaves the directory fresh; the zero time makes ReadDir list.
	root.dir.DirTime = time.Time{}
	root.mu.Unlock()

	dhA := root.OpenDir()
	dhB := root.OpenDir()
	// Only the flat listing is under test.
	dhA.noSlurp = true
	dhB.noSlurp = true

	type result struct {
		names []string
		err   error
	}
	read := func(dh *DirHandle, max int) chan result {
		done := make(chan result, 1)
		go func() {
			dh.mu.Lock()
			names, err := readDirHandleNoCloud(dh, max)
			dh.mu.Unlock()
			done <- result{names, err}
		}()
		return done
	}

	// Each step waits for one event. The first one that does not come is
	// remembered and the remaining steps are skipped.
	stalled := ""
	await := func(what string, event chan struct{}) {
		if stalled != "" {
			return
		}
		select {
		case <-event:
		case <-time.After(wait):
			stalled = what
		}
	}
	collect := func(what string, done chan result) (res result) {
		if stalled != "" {
			return
		}
		select {
		case res = <-done:
		case <-time.After(wait):
			stalled = what
		}
		return
	}

	// The two dot entries and the first page.
	const firstA = 4
	// The two dot entries and "a": B stays inside the first page, so that
	// the entry it reads next is one the duplicate's answer covers.
	const firstB = 3

	doneA := read(dhA, firstA)
	await("the first handle's request for the first page", arrived[0])
	doneB := read(dhB, firstB)
	await("the second handle's request for the first page", arrived[1])

	close(release[0])
	a1 := collect("the first handle's read of the first page", doneA)
	close(release[1])
	b1 := collect("the second handle's read after the duplicate answer", doneB)

	var a2, b2 result
	if stalled == "" {
		a2 = collect("the first handle's read to the end", read(dhA, maxEntries))
	}
	if stalled == "" {
		b2 = collect("the second handle's read to the end", read(dhB, maxEntries))
	}

	if stalled != "" {
		// A reader may still hold the inode lock, which CloseDir takes, so the
		// handles are deliberately not closed on this path.
		t.Fatalf("timed out waiting for %v", stalled)
	}

	dhA.CloseDir()
	dhB.CloseDir()

	mu.Lock()
	gotRequested := append([]string(nil), requested...)
	mu.Unlock()

	t.Assert(a1.err, IsNil)
	t.Assert(b1.err, IsNil)
	t.Assert(a2.err, IsNil)
	t.Assert(b2.err, IsNil)
	// The root's two dot entries read as "": readDirHandleNoCloud passes the
	// inode name, and the root's is empty.
	t.Assert(a1.names, DeepEquals, []string{"", "", "a", "b"})
	t.Assert(b1.names, DeepEquals, []string{"", "", "a"})
	t.Assert(a2.names, DeepEquals, []string{"c", "d", "e", "f"})
	t.Assert(b2.names, DeepEquals, []string{"b", "c", "d", "e", "f"})
	// The handle whose answer was the duplicate asks again from where the
	// directory stands, and no page is requested more often than that.
	t.Assert(gotRequested, DeepEquals, []string{"", "", "b", "d"})
}
