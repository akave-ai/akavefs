package core

import (
	"sync"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// Everything in this file only guards under -race. It covers the removal of an
// unsynchronised write, so the failure it detects is a data race and nothing
// else: with master's core/dir.go, `go test -race` reports data races and exits
// 1, while the same suite without -race -- which is the command CI runs today --
// passes and exits 0. The number of reports is deliberately not quoted here: the
// detector dedups by stack, so it varies run to run and with the shape of this
// file.
//
// LookUpInodeMaybeDir launches one probe per enabled lookup strategy:
// HeadBlob(key), HeadBlob(key+"/") and the prefix LIST. All three run only while
// the backend reports no DirBlob capability and Cheap, NoDirObject and
// ExplicitDir are all off, so the barrier below is sized by maybeDirLookupProbes
// and the helpers assert those four facts rather than assume them: a miscount
// would otherwise wait for a probe that never starts. The maybeDirLookup prefix
// keeps every name here clear of upstream symbols: this is package core, shared
// with every inherited file.
const maybeDirLookupProbes = 3

// maybeDirLookupProbeTimeout only has to outlast a probe that is already
// unblocked. A barrier-released iteration costs ~0.4ms under -race, measured over
// this file's own repetitions, so the bound below cannot flake; it exists purely
// to turn a probe miscount into a legible failure instead of a hang.
const maybeDirLookupProbeTimeout = 30 * time.Second

// maybeDirLookupIterations is how often each variant repeats its lookup. One
// iteration already interleaves the probes, because they are released from a
// barrier; the repetition is what makes the race detector see the window, and
// what makes the probe census effective (see maybeDirLookupAssertNoExtraProbe).
const maybeDirLookupIterations = 1000

// maybeDirLookupCase is the whole difference between the three variants: which
// HeadBlob key answers, what the prefix LIST returns, and the key the lookup
// must then produce. Each case makes exactly one rung of the result ladder the
// only usable answer, so the expected key holds whichever probe reports first --
// which is all the arrival-ordered priority permits a test to assert.
type maybeDirLookupCase struct {
	// headBlobHit is the one key whose HeadBlob succeeds; "" means both the
	// object and the dir-object probe return ENOENT.
	headBlobHit string
	// listItem is the single item the prefix LIST returns; "" means it comes
	// back empty, which is not a usable answer.
	listItem string
	// want is the key LookUpInodeMaybeDir must return.
	want string
}

// maybeDirLookupFlags returns the default flags and asserts the three that
// decide the probe count, before any barrier is sized by them.
func maybeDirLookupFlags(t *C) *cfg.FlagStorage {
	flags := cfg.DefaultFlags()
	t.Assert(flags.Cheap, Equals, false)
	t.Assert(flags.NoDirObject, Equals, false)
	t.Assert(flags.ExplicitDir, Equals, false)
	return flags
}

// maybeDirLookupRunIteration runs one barrier-released lookup of "child" against
// tc's answers and checks what comes back. The three variants share it so the
// barrier, the release and the probe census cannot drift apart between them. It
// is one call per iteration on purpose: the deferred release then scopes to a
// single iteration instead of accumulating one closure per repetition.
func maybeDirLookupRunIteration(t *C, fs *Goofys, iteration int, tc maybeDirLookupCase) {
	started := make(chan struct{}, maybeDirLookupProbes)
	release := make(chan struct{})
	// t.Fatalf unwinds with runtime.Goexit, which runs defers but skips the
	// explicit release below, so a probe left waiting on release would leak once
	// per failing iteration, taking the lookup goroutine with it. sync.OnceFunc
	// makes closing on both paths safe.
	releaseProbes := sync.OnceFunc(func() { close(release) })
	defer releaseProbes()

	cloud := &TestBackend{
		HeadBlobFunc: func(param *HeadBlobInput) (*HeadBlobOutput, error) {
			started <- struct{}{}
			<-release
			if tc.headBlobHit != "" && param.Key == tc.headBlobHit {
				return &HeadBlobOutput{
					BlobItemOutput: BlobItemOutput{Key: PString(tc.headBlobHit)},
				}, nil
			}
			return nil, syscall.ENOENT
		},
		ListBlobsFunc: func(param *ListBlobsInput) (*ListBlobsOutput, error) {
			started <- struct{}{}
			<-release
			if tc.listItem == "" {
				return &ListBlobsOutput{}, nil
			}
			return &ListBlobsOutput{
				Items: []BlobItemOutput{{Key: PString(tc.listItem)}},
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
			t.Fatalf("iteration %d: %d of %d lookup probes started", iteration, n, maybeDirLookupProbes)
		}
	}
	releaseProbes()

	item, err := <-result, <-errs
	t.Assert(err, IsNil)
	t.Assert(NilStr(item.Key), Equals, tc.want)
	maybeDirLookupAssertNoExtraProbe(t, iteration, started)
}

// maybeDirLookupAssertNoExtraProbe catches the drift the barrier cannot: the
// barrier waits for maybeDirLookupProbes starts, so too few probes fail it,
// while one too many would otherwise pass unnoticed.
//
// This is an aggregate check, not a per-iteration one, and the distinction
// matters: the lookup can return on one buffered result while a later probe
// goroutine is still between its go statement and its send, so an extra probe
// may not have reached the channel yet and this receive can find nothing. Over
// maybeDirLookupIterations repetitions the drift is caught in practice -- an
// extra probe added to LookUpInodeMaybeDir without incrementing n fails every
// variant on an early iteration, in every run measured -- but no single
// iteration is guaranteed to catch it. The receive stays non-blocking so the
// passing path pays nothing; a bounded drain would not make one iteration
// sufficient either, since no fixed wait outlasts every straggler.
func maybeDirLookupAssertNoExtraProbe(t *C, iteration int, started <-chan struct{}) {
	select {
	case <-started:
		t.Fatalf("iteration %d: more than %d lookup probes started", iteration, maybeDirLookupProbes)
	default:
	}
}

// Prefix-listing rung: both HeadBlobs are ENOENT and only the LIST answers.
func (s *DirTest) TestLookUpInodeMaybeDirConcurrentResults(t *C) {
	fs := &Goofys{flags: maybeDirLookupFlags(t)}

	for i := 0; i < maybeDirLookupIterations; i++ {
		maybeDirLookupRunIteration(t, fs, i, maybeDirLookupCase{
			listItem: "child/entry",
			want:     "child/entry",
		})
	}
}

// Object rung: HeadBlob(key) succeeds, HeadBlob(key+"/") is ENOENT and the LIST
// comes back empty, so the object is the only usable answer.
func (s *DirTest) TestLookUpInodeMaybeDirObjectResult(t *C) {
	fs := &Goofys{flags: maybeDirLookupFlags(t)}

	for i := 0; i < maybeDirLookupIterations; i++ {
		maybeDirLookupRunIteration(t, fs, i, maybeDirLookupCase{
			headBlobHit: "child",
			want:        "child",
		})
	}
}

// Dir-object rung: only HeadBlob(key+"/") succeeds and the LIST comes back
// empty, which is how a directory that exists solely as a "key/" object is
// found. Without this variant, discarding the value in receiveResult's
// lookupDirObject case leaves the suite green and such a directory would
// silently become ENOENT.
func (s *DirTest) TestLookUpInodeMaybeDirDirObjectResult(t *C) {
	fs := &Goofys{flags: maybeDirLookupFlags(t)}

	for i := 0; i < maybeDirLookupIterations; i++ {
		maybeDirLookupRunIteration(t, fs, i, maybeDirLookupCase{
			headBlobHit: "child/",
			want:        "child/",
		})
	}
}
