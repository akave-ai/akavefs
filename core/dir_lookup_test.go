package core

import (
	"sync"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// LookUpInodeMaybeDir launches one probe per enabled lookup strategy:
// HeadBlob(key), HeadBlob(key+"/") and the prefix LIST. All three run only while
// the backend reports no DirBlob capability and Cheap, NoDirObject and
// ExplicitDir are all off, so the barriers below are sized by
// maybeDirLookupProbes and the tests assert those four facts rather than assume
// them: a miscount would otherwise wait for a probe that never starts. The
// maybeDirLookup prefix keeps both constants clear of upstream symbols: this is
// package core, shared with every inherited file.
const maybeDirLookupProbes = 3

// maybeDirLookupProbeTimeout only has to outlast a probe that is already
// unblocked; it is ~2000x the measured per-iteration cost under -race, so it
// cannot flake, and it exists purely to turn a probe miscount into a legible
// failure instead of a hang.
const maybeDirLookupProbeTimeout = 30 * time.Second

// The three variants below each make one rung of the result ladder the only
// usable answer, so the expected key is assertable whichever probe reports
// first -- which is all the arrival-ordered priority permits. Between them they
// cover the object, dirObject and prefixList routing in receiveResult.

// Prefix-listing rung: both HeadBlobs are ENOENT and only the LIST answers.
func (s *DirTest) TestLookUpInodeMaybeDirConcurrentResults(t *C) {
	flags := cfg.DefaultFlags()
	fs := &Goofys{flags: flags}
	t.Assert(flags.Cheap, Equals, false)
	t.Assert(flags.NoDirObject, Equals, false)
	t.Assert(flags.ExplicitDir, Equals, false)

	for i := 0; i < 1000; i++ {
		started := make(chan struct{}, maybeDirLookupProbes)
		release := make(chan struct{})
		// t.Fatalf unwinds with runtime.Goexit, which runs defers but skips the
		// explicit release below, so a probe left waiting on release would leak
		// once per failing iteration, taking the lookup goroutine with it.
		// sync.OnceFunc makes closing on both paths safe; on the passing path
		// the deferred call is a no-op left to run when the test returns.
		releaseProbes := sync.OnceFunc(func() { close(release) })
		defer releaseProbes()

		cloud := &TestBackend{
			HeadBlobFunc: func(param *HeadBlobInput) (*HeadBlobOutput, error) {
				started <- struct{}{}
				<-release
				return nil, syscall.ENOENT
			},
			ListBlobsFunc: func(param *ListBlobsInput) (*ListBlobsOutput, error) {
				started <- struct{}{}
				<-release
				return &ListBlobsOutput{
					Items: []BlobItemOutput{{Key: PString("child/entry")}},
				}, nil
			},
		}
		t.Assert(cloud.Capabilities().DirBlob, Equals, false)
		parent := &Inode{
			fs:  fs,
			dir: &DirInodeData{cloud: cloud},
		}
		result := make(chan *BlobItemOutput, 1)
		errs := make(chan error, 1)
		go func() {
			item, err := parent.LookUpInodeMaybeDir("child")
			result <- item
			errs <- err
		}()

		for n := 0; n < maybeDirLookupProbes; n++ {
			select {
			case <-started:
			case <-time.After(maybeDirLookupProbeTimeout):
				t.Fatalf("iteration %d: %d of %d lookup probes started", i, n, maybeDirLookupProbes)
			}
		}
		releaseProbes()

		item, err := <-result, <-errs
		t.Assert(err, IsNil)
		t.Assert(NilStr(item.Key), Equals, "child/entry")
		assertNoExtraLookupProbe(t, i, started)
	}
}

// Object rung: HeadBlob(key) succeeds, HeadBlob(key+"/") is ENOENT and the LIST
// comes back empty, so the object is the only usable answer.
func (s *DirTest) TestLookUpInodeMaybeDirObjectResult(t *C) {
	flags := cfg.DefaultFlags()
	fs := &Goofys{flags: flags}
	t.Assert(flags.Cheap, Equals, false)
	t.Assert(flags.NoDirObject, Equals, false)
	t.Assert(flags.ExplicitDir, Equals, false)

	for i := 0; i < 1000; i++ {
		started := make(chan struct{}, maybeDirLookupProbes)
		release := make(chan struct{})
		releaseProbes := sync.OnceFunc(func() { close(release) })
		defer releaseProbes()

		cloud := &TestBackend{
			HeadBlobFunc: func(param *HeadBlobInput) (*HeadBlobOutput, error) {
				started <- struct{}{}
				<-release
				if param.Key == "child" {
					return &HeadBlobOutput{
						BlobItemOutput: BlobItemOutput{Key: PString("child")},
					}, nil
				}
				return nil, syscall.ENOENT
			},
			ListBlobsFunc: func(param *ListBlobsInput) (*ListBlobsOutput, error) {
				started <- struct{}{}
				<-release
				return &ListBlobsOutput{}, nil
			},
		}
		t.Assert(cloud.Capabilities().DirBlob, Equals, false)
		parent := &Inode{
			fs:  fs,
			dir: &DirInodeData{cloud: cloud},
		}
		result := make(chan *BlobItemOutput, 1)
		errs := make(chan error, 1)
		go func() {
			item, err := parent.LookUpInodeMaybeDir("child")
			result <- item
			errs <- err
		}()

		for n := 0; n < maybeDirLookupProbes; n++ {
			select {
			case <-started:
			case <-time.After(maybeDirLookupProbeTimeout):
				t.Fatalf("iteration %d: %d of %d lookup probes started", i, n, maybeDirLookupProbes)
			}
		}
		releaseProbes()

		item, err := <-result, <-errs
		t.Assert(err, IsNil)
		t.Assert(NilStr(item.Key), Equals, "child")
		assertNoExtraLookupProbe(t, i, started)
	}
}

// Dir-object rung: only HeadBlob(key+"/") succeeds and the LIST comes back
// empty, which is how a directory that exists solely as a "key/" object is
// found. Without this variant, discarding the value in receiveResult's
// lookupDirObject case leaves the suite green and such a directory would
// silently become ENOENT.
func (s *DirTest) TestLookUpInodeMaybeDirDirObjectResult(t *C) {
	flags := cfg.DefaultFlags()
	fs := &Goofys{flags: flags}
	t.Assert(flags.Cheap, Equals, false)
	t.Assert(flags.NoDirObject, Equals, false)
	t.Assert(flags.ExplicitDir, Equals, false)

	for i := 0; i < 1000; i++ {
		started := make(chan struct{}, maybeDirLookupProbes)
		release := make(chan struct{})
		releaseProbes := sync.OnceFunc(func() { close(release) })
		defer releaseProbes()

		cloud := &TestBackend{
			HeadBlobFunc: func(param *HeadBlobInput) (*HeadBlobOutput, error) {
				started <- struct{}{}
				<-release
				if param.Key == "child/" {
					return &HeadBlobOutput{
						BlobItemOutput: BlobItemOutput{Key: PString("child/")},
					}, nil
				}
				return nil, syscall.ENOENT
			},
			ListBlobsFunc: func(param *ListBlobsInput) (*ListBlobsOutput, error) {
				started <- struct{}{}
				<-release
				return &ListBlobsOutput{}, nil
			},
		}
		t.Assert(cloud.Capabilities().DirBlob, Equals, false)
		parent := &Inode{
			fs:  fs,
			dir: &DirInodeData{cloud: cloud},
		}
		result := make(chan *BlobItemOutput, 1)
		errs := make(chan error, 1)
		go func() {
			item, err := parent.LookUpInodeMaybeDir("child")
			result <- item
			errs <- err
		}()

		for n := 0; n < maybeDirLookupProbes; n++ {
			select {
			case <-started:
			case <-time.After(maybeDirLookupProbeTimeout):
				t.Fatalf("iteration %d: %d of %d lookup probes started", i, n, maybeDirLookupProbes)
			}
		}
		releaseProbes()

		item, err := <-result, <-errs
		t.Assert(err, IsNil)
		t.Assert(NilStr(item.Key), Equals, "child/")
		assertNoExtraLookupProbe(t, i, started)
	}
}

// assertNoExtraLookupProbe catches the drift the barrier cannot: the barrier
// waits for maybeDirLookupProbes starts, so too few probes fail it, but one too
// many used to pass unnoticed. The lookup has returned by the time this runs,
// so a probe it launched has already sent, and the receive stays non-blocking
// to keep the check free on the passing path.
func assertNoExtraLookupProbe(t *C, iteration int, started <-chan struct{}) {
	select {
	case <-started:
		t.Fatalf("iteration %d: more than %d lookup probes started", iteration, maybeDirLookupProbes)
	default:
	}
}
