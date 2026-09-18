package core

// Regression tests for Rename onto a directory whose listing has expired
// ("cold"), when the rename happens inside a directory that is NOT the mount
// root.
//
// Before upstream GeeseFS c0f0e84 (#199), Rename called isEmptyDir on the
// cold destination while holding parent.mu and fromInode.mu. isEmptyDir went
// through ReadDir -> loadListing -> slurpOnce, and the slurp re-locked
// inodes Rename already held:
//   - when the parent is the mount root, listObjectsSlurp locks the root
//     itself. Upstream's TestRenameDirExpiredDestinationNoCloud covers this.
//   - when the parent is nested (these tests), the root lock succeeds and the
//     slurp deadlocks later, in insertSubTree, on the nested parent.
// c0f0e84 makes isEmptyDir use a flat listing (DirHandle.noSlurp), which
// fixes both. These tests pin the nested case so a future sync or local
// change cannot bring the insertSubTree deadlock back unnoticed.
//
// PR akave-ai/akavefs#2 found and first fixed the nested case; its
// s3proxy-backed test is the origin of this scenario.

import (
	"context"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	. "gopkg.in/check.v1"

	"github.com/yandex-cloud/geesefs/core/cfg"
)

// renameColdTargetTimeout bounds each Rename. A passing Rename takes
// milliseconds against the in-memory backend; 5s leaves room for -race and a
// loaded CI runner while still turning a deadlock into a failed test rather
// than a run that hangs until PerTestTimeout panics the whole test binary.
const renameColdTargetTimeout = 5 * time.Second

// renameColdTargetBackend serves a fixed key set with S3 ListObjectsV2
// semantics (Prefix, StartAfter, and Delimiter roll-up into CommonPrefixes)
// and records every ListBlobs request, so a test can prove the cold
// destination really was listed rather than answered from cache.
type renameColdTargetBackend struct {
	keys []string // sorted once at construction; never mutated afterwards

	mu    sync.Mutex // protects lists: the flusher may call ListBlobs too
	lists []ListBlobsInput
}

func newRenameColdTargetBackend(keys ...string) *renameColdTargetBackend {
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	return &renameColdTargetBackend{keys: sorted}
}

func (b *renameColdTargetBackend) ListBlobs(param *ListBlobsInput) (*ListBlobsOutput, error) {
	b.mu.Lock()
	b.lists = append(b.lists, *param)
	b.mu.Unlock()

	prefix := NilStr(param.Prefix)
	after := NilStr(param.StartAfter)
	out := &ListBlobsOutput{}
	seen := map[string]bool{}
	for _, k := range b.keys {
		if !strings.HasPrefix(k, prefix) || k <= after {
			continue
		}
		if param.Delimiter != nil {
			// S3 rolls every key that has the delimiter after the prefix up
			// into one CommonPrefix, "x/" directory markers included; only a
			// key with no further delimiter is returned as an item.
			rest := k[len(prefix):]
			if i := strings.Index(rest, *param.Delimiter); i >= 0 {
				p := prefix + rest[:i+1]
				if !seen[p] {
					seen[p] = true
					out.Prefixes = append(out.Prefixes, BlobPrefixOutput{Prefix: PString(p)})
				}
				continue
			}
		}
		key := k
		out.Items = append(out.Items, BlobItemOutput{Key: &key, ETag: PString("e"), Size: 1})
	}
	return out, nil
}

// listedPrefix reports whether any ListBlobs request used exactly this prefix.
func (b *renameColdTargetBackend) listedPrefix(prefix string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range b.lists {
		if NilStr(l.Prefix) == prefix {
			return true
		}
	}
	return false
}

// renameColdTargetSetUp mounts a fixture-free fs over keys and builds
// dir2/{a_src,a_tgt} in the inode cache, with a_tgt cold. StatCacheTTL is
// non-zero because the pre-fix deadlock needs loadListing to choose slurp.
func (s *GoofysTest) renameColdTargetSetUp(t *C, keys ...string) (dir2 *Inode, fake *renameColdTargetBackend) {
	fake = newRenameColdTargetBackend(keys...)
	flags := cfg.DefaultFlags()
	flags.StatCacheTTL = time.Minute
	// err makes every call other than ListBlobs fail with ENOSYS, so nothing
	// can reach a real backend. The flusher's directory-marker writes for the
	// doMkDir inodes fail and are logged, as in upstream's NoCloud tests.
	backend := &TestBackend{err: syscall.ENOSYS, ListBlobsFunc: fake.ListBlobs}
	s.cloud = backend
	var err error
	s.fs, err = newGoofys(context.Background(), "test", flags, func(string, *cfg.FlagStorage) (StorageBackend, error) {
		return backend, nil
	})
	t.Assert(err, IsNil)

	// doMkDir returns the new inode locked and leaves the parent locked.
	root := s.getRoot(t)
	root.mu.Lock()
	dir2 = root.doMkDir("dir2")
	root.mu.Unlock()
	src := dir2.doMkDir("a_src")
	src.mu.Unlock()
	tgt := dir2.doMkDir("a_tgt")
	// Cold: an expired DirTime with no list in progress is exactly the state
	// in which loadListing used to pick slurp (listMarker == "").
	tgt.dir.DirTime = time.Time{}
	tgt.dir.listMarker = ""
	tgt.dir.listDone = false
	tgt.mu.Unlock()
	dir2.mu.Unlock()
	return dir2, fake
}

// renameColdTargetWithin runs dir2.Rename(from -> to) and fails the test if it does not
// return within renameColdTargetTimeout. On timeout the Rename goroutine stays
// blocked forever: a sync.Mutex wait cannot be cancelled, so it keeps holding
// the root, dir2 and a_src locks of this test's private fs. Nothing else can
// reach that fs: s.fs and s.cloud are replaced by the next test, and
// TearDownTest does not touch the fs of a NoCloud test.
func renameColdTargetWithin(t *C, dir2 *Inode, from, to string) error {
	done := make(chan error, 1) // buffered: a late return must not block
	go func() { done <- dir2.Rename(from, dir2, to) }()
	select {
	case err := <-done:
		return err
	case <-time.After(renameColdTargetTimeout):
		t.Fatalf("Rename(%q -> %q) under a nested parent deadlocked on a cold destination (no return within %v)",
			from, to, renameColdTargetTimeout)
		return nil
	}
}

func (s *GoofysTest) TestRenameNestedColdEmptyTargetNoCloud(t *C) {
	// "dir2/a_tgt/" is only a directory marker, so the target is empty.
	// "dir2/z_sib/f2" sorts after the target: before c0f0e84 the slurp that
	// starts after "dir2/a_tgt/" returned it and descended into dir2.
	dir2, fake := s.renameColdTargetSetUp(t, "dir2/a_src/", "dir2/a_tgt/", "dir2/z_sib/f2")
	dir2.mu.Lock()
	srcId := dir2.findChildUnlocked("a_src").Id
	dir2.mu.Unlock()

	t.Assert(renameColdTargetWithin(t, dir2, "a_src", "a_tgt"), IsNil)
	t.Assert(fake.listedPrefix("dir2/a_tgt/"), Equals, true)

	dir2.mu.Lock()
	srcAfter := dir2.findChildUnlocked("a_src")
	tgtAfter := dir2.findChildUnlocked("a_tgt")
	dir2.mu.Unlock()
	t.Assert(srcAfter, IsNil)
	t.Assert(tgtAfter, NotNil)
	t.Assert(tgtAfter.Id, Equals, srcId)
}

func (s *GoofysTest) TestRenameNestedColdNonEmptyTargetNoCloud(t *C) {
	// "dir2/a_tgt/g" makes the target non-empty, so POSIX requires ENOTEMPTY.
	dir2, fake := s.renameColdTargetSetUp(t, "dir2/a_src/", "dir2/a_tgt/g", "dir2/z_sib/f2")
	dir2.mu.Lock()
	srcId := dir2.findChildUnlocked("a_src").Id
	tgtId := dir2.findChildUnlocked("a_tgt").Id
	dir2.mu.Unlock()

	t.Assert(renameColdTargetWithin(t, dir2, "a_src", "a_tgt"), Equals, syscall.ENOTEMPTY)
	t.Assert(fake.listedPrefix("dir2/a_tgt/"), Equals, true)

	// A refused rename must leave both directories where they were.
	dir2.mu.Lock()
	srcAfter := dir2.findChildUnlocked("a_src")
	tgtAfter := dir2.findChildUnlocked("a_tgt")
	dir2.mu.Unlock()
	t.Assert(srcAfter, NotNil)
	t.Assert(srcAfter.Id, Equals, srcId)
	t.Assert(tgtAfter, NotNil)
	t.Assert(tgtAfter.Id, Equals, tgtId)
}
