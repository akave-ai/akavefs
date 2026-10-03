package core

import (
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

type DirTest struct{}

var _ = Suite(&DirTest{})

func (s *DirTest) TestLocateLtSlash(t *C) {
	t.Assert(locateLtSlash("wow", 0), Equals, -1)
	// '-', ' ' are less than '/'
	t.Assert(locateLtSlash("w-o-w", 0), Equals, 1)
	t.Assert(locateLtSlash("w o w", 0), Equals, 1)
	// All unicode chars have multi-byte values and are > '/'
	t.Assert(locateLtSlash("wøw", 0), Equals, -1)
	// Prefix is handled correctly
	t.Assert(locateLtSlash("w-o-w/w-o-w", 6), Equals, 7)
	t.Assert(locateLtSlash("w-o-w///w.jpg", 6), Equals, 9)
	t.Assert(locateLtSlash("w-o-w//.hidden", 6), Equals, -1)
}

// LookUpInodeMaybeDir launches one probe per enabled lookup strategy:
// HeadBlob(key), HeadBlob(key+"/") and the prefix LIST. All three run only while
// the backend reports no DirBlob capability and Cheap, NoDirObject and
// ExplicitDir are all off, so the barrier below is sized by lookupProbes and the
// tests assert those four facts rather than assume them: a miscount would
// otherwise wait for a probe that never starts.
const lookupProbes = 3

// lookupProbeTimeout only has to outlast a probe that is already unblocked; it is
// ~2000x the measured per-iteration cost under -race, so it cannot flake, and it
// exists purely to turn a probe miscount into a legible failure instead of a hang.
const lookupProbeTimeout = 30 * time.Second

func (s *DirTest) TestLookUpInodeMaybeDirConcurrentResults(t *C) {
	flags := cfg.DefaultFlags()
	fs := &Goofys{flags: flags}
	t.Assert(flags.Cheap, Equals, false)
	t.Assert(flags.NoDirObject, Equals, false)
	t.Assert(flags.ExplicitDir, Equals, false)

	for i := 0; i < 1000; i++ {
		started := make(chan struct{}, lookupProbes)
		release := make(chan struct{})
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

		for n := 0; n < lookupProbes; n++ {
			select {
			case <-started:
			case <-time.After(lookupProbeTimeout):
				t.Fatalf("iteration %d: %d of %d lookup probes started", i, n, lookupProbes)
			}
		}
		close(release)

		item, err := <-result, <-errs
		t.Assert(err, IsNil)
		t.Assert(NilStr(item.Key), Equals, "child/entry")
	}
}

// The prefix-listing variant above never reaches the object rung of the result
// ladder. This one makes the object the only usable answer -- HeadBlob(key)
// succeeds, HeadBlob(key+"/") is ENOENT and the LIST comes back empty -- so the
// expected result is the object whichever probe reports first, which is what the
// arrival-ordered priority permits us to assert.
func (s *DirTest) TestLookUpInodeMaybeDirObjectResult(t *C) {
	flags := cfg.DefaultFlags()
	fs := &Goofys{flags: flags}
	t.Assert(flags.Cheap, Equals, false)
	t.Assert(flags.NoDirObject, Equals, false)
	t.Assert(flags.ExplicitDir, Equals, false)

	for i := 0; i < 1000; i++ {
		started := make(chan struct{}, lookupProbes)
		release := make(chan struct{})
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

		for n := 0; n < lookupProbes; n++ {
			select {
			case <-started:
			case <-time.After(lookupProbeTimeout):
				t.Fatalf("iteration %d: %d of %d lookup probes started", i, n, lookupProbes)
			}
		}
		close(release)

		item, err := <-result, <-errs
		t.Assert(err, IsNil)
		t.Assert(NilStr(item.Key), Equals, "child")
	}
}

func (s *DirTest) TestIntelligentListCut(t *C) {
	// Output is not truncated
	// => Paging marker should be empty
	// (No matter what Items and Prefixes are present)
	lastName, err := intelligentListCut(&ListBlobsOutput{
		IsTruncated: false,
		Items:       []BlobItemOutput{{Key: PString("item.jpg")}},
		Prefixes:    []BlobPrefixOutput{{Prefix: PString("prefix-has-dash/")}},
	}, nil, nil, "")
	t.Assert(lastName, Equals, "")
	t.Assert(err, IsNil)

	// Last prefix is larger than last item and it's "normal".
	// All chars in its name are > '/'
	// => Paging marker is the last item, list is not cut
	lastName, err = intelligentListCut(&ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/item-has-dash")},
			{Key: PString("w-o-w/item.jpg")}},
		Prefixes: []BlobPrefixOutput{
			{Prefix: PString("w-o-w/prefix-has-dash/")},
			{Prefix: PString("w-o-w/prefix/")}},
	}, nil, nil, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/prefix/")
	t.Assert(err, IsNil)

	// Same, but prefix is larger than item
	lastName, err = intelligentListCut(&ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/item-has-dash")},
			{Key: PString("w-o-w/item")}},
		Prefixes: []BlobPrefixOutput{
			{Prefix: PString("w-o-w/dir/")}},
	}, nil, nil, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/item")
	t.Assert(err, IsNil)

	// 6th item has larger prefix before 5th position of '.' ('.' < '/')
	// => List should be cut at 5 items
	resp := &ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/l180404688.req")},
			{Key: PString("w-o-w/l180404690.req")},
			{Key: PString("w-o-w/l180404692.req")},
		},
		Prefixes: []BlobPrefixOutput{
			{Prefix: PString("w-o-w/l180404687.req/")},
			{Prefix: PString("w-o-w/l180404689.req/")},
			{Prefix: PString("w-o-w/l180404691.req/")},
		},
	}
	lastName, err = intelligentListCut(resp, nil, nil, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/l180404691.req/")
	t.Assert(len(resp.Items), Equals, 2)
	t.Assert(len(resp.Prefixes), Equals, 3)
	t.Assert(err, IsNil)

	// 6th item has the same prefix as 5th before '.', but the next character is larger than '>'
	// => List should be cut at 5 items
	resp = &ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/l180404687.req")},
			{Key: PString("w-o-w/l180404688.req")},
			{Key: PString("w-o-w/l180404689.req")},
			{Key: PString("w-o-w/l180404690.req")},
			{Key: PString("w-o-w/l180404691.req")},
			{Key: PString("w-o-w/l1804046910.req")},
		},
	}
	lastName, err = intelligentListCut(resp, nil, nil, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/l180404691.req")
	t.Assert(len(resp.Items), Equals, 5)
	t.Assert(err, IsNil)

	// 6th item has the same prefix as 5th before '.', and the next character is still smaller than '/'
	// => List can't be cut at 5th item, it should be cut at 4th one
	resp = &ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/l180404687.req")},
			{Key: PString("w-o-w/l180404688.req")},
			{Key: PString("w-o-w/l180404689.req")},
			{Key: PString("w-o-w/l180404690.req")},
			{Key: PString("w-o-w/l180404691-1.req")},
			{Key: PString("w-o-w/l180404691.req")},
		},
	}
	lastName, err = intelligentListCut(resp, nil, nil, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/l180404690.req")
	t.Assert(len(resp.Items), Equals, 4)
	t.Assert(err, IsNil)

	// 6th item has larger prefix than 5th before '.', but it's shorter
	// => List should be cut at 5 items
	resp = &ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/l180404687.req")},
			{Key: PString("w-o-w/l180404688.req")},
			{Key: PString("w-o-w/l180404689.req")},
			{Key: PString("w-o-w/l180404690.req")},
			{Key: PString("w-o-w/l180404691.req")},
			{Key: PString("w-o-w/l1805.req")},
		},
	}
	lastName, err = intelligentListCut(resp, nil, nil, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/l180404691.req")
	t.Assert(len(resp.Items), Equals, 5)
	t.Assert(err, IsNil)

	// All items have the same prefix
	// => List can't be cut
	// => It should make an additional request to cloud
	resp = &ListBlobsOutput{
		IsTruncated: true,
		Items: []BlobItemOutput{
			{Key: PString("w-o-w/2019-0000")},
			{Key: PString("w-o-w/2019-0002")},
			{Key: PString("w-o-w/2019-0004")},
		},
		Prefixes: []BlobPrefixOutput{
			{Prefix: PString("w-o-w/2019-0001")},
			{Prefix: PString("w-o-w/2019-0003")},
			{Prefix: PString("w-o-w/2019-0005")},
		},
	}
	listCalled := 0
	checkedPrefix := "w-o-w/2019/"
	cloud := &TestBackend{
		ListBlobsFunc: func(param *ListBlobsInput) (*ListBlobsOutput, error) {
			t.Assert(NilStr(param.StartAfter), Equals, "w-o-w/2019.\xEF\xBF\xBD")
			t.Assert(param.MaxKeys, NotNil)
			t.Assert(*param.MaxKeys, Equals, uint32(1))
			listCalled++
			return &ListBlobsOutput{
				IsTruncated: true,
				Items: []BlobItemOutput{
					{Key: PString(checkedPrefix)},
				},
			}, nil
		},
	}
	flags := cfg.DefaultFlags()
	lastName, err = intelligentListCut(resp, flags, cloud, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/2019-0005")
	t.Assert(len(resp.Items), Equals, 3)
	t.Assert(len(resp.Prefixes), Equals, 4)
	t.Assert(*resp.Prefixes[3].Prefix, Equals, "w-o-w/2019/")
	t.Assert(err, IsNil)
	t.Assert(listCalled, Equals, 1)

	// Same, but list request doesn't return 2019/
	listCalled = 0
	checkedPrefix = "w-o-w/2020-0000"
	resp.Prefixes = resp.Prefixes[0:3]
	lastName, err = intelligentListCut(resp, flags, cloud, "w-o-w/")
	t.Assert(lastName, Equals, "w-o-w/2019-0005")
	t.Assert(len(resp.Items), Equals, 3)
	t.Assert(len(resp.Prefixes), Equals, 3)
	t.Assert(err, IsNil)
	t.Assert(listCalled, Equals, 1)
}
