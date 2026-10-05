# Directory handle invalidation

Register entry: `dir-handle-invalidation` in [divergences.md](divergences.md).

## Problem

An open directory is read through a `DirHandle`. The handle keeps its place in the
directory's sorted list of children (`Children`) as an index, `lastInternalOffset`. Next
to it the handle keeps the offset the kernel sees, `lastExternalOffset`, and the name of
the last entry it handed out, `lastName`. Offsets 0 and 1 stand for "." and ".."; the
children start at offset 2.

When a child is removed, or inserted in front of an existing child, the positions in
`Children` shift and a handle's index no longer points at the same entry. The inherited
code handles this by invalidation: the function that changes `Children` sets the index of
every open handle to -1, and on the next read `checkDirPosition` finds the position again
from `lastName`.

This code had three defects. The first is the reason for the change; the other two were
fixed along the way because they sit in the same few lines.

**The invalidation is a data race.** The three child mutators — `removeChildUnlocked`,
`removeAllChildrenUnlocked`, and `insertChildUnlocked` when the new child goes in front
of an existing one — wrote `lastInternalOffset` of each handle without the handle's own
lock, `dh.mu`. The mutators are annotated `LOCKS_REQUIRED(parent.mu)`, which is the
directory's lock; whether every caller honours that is not established, and that lock
does not protect the field in any case. The field is documented as
protected by `dh.mu`, and `DirHandle.Next`, which requires `dh.mu` and nothing else,
reads the field and writes it back incremented. The FUSE `ReadDir` handler calls `Next`
after `DirHandle.ReadDir` has returned and released the directory's lock. So a mutator
and `Next` can run at the same time: a mutator whose caller holds the directory's lock,
as the annotation requires, still holds a different lock from `Next`. This happens when
one thread reads a directory while an operation on another thread reaches a mutator for
the same directory: for example a create, an unlink or a rename in it, or a listing that
adds or expires children.

If the mutator's write lands between the read and the write in `Next`, `Next` overwrites
the -1 and the invalidation is lost. The handle then goes on with an index into a list
that has shifted, so the reader gets one entry twice (after an insert before its
position) or misses one (after a removal before its position). This outcome is derived
from the code. What was observed is the race detector's report; a repeated or skipped
entry was not reproduced, because there is no hook between the read and the write in
`Next`.

**The dot entries are skipped when a directory changes before the first read.** A handle
that has not returned anything yet — freshly opened, or rewound to offset 0 — has an
empty `lastName`. When such a handle was invalidated, upstream's `checkDirPosition`
treated the empty name as a child name. The search for an empty name matches at the start
of the list, so the handle was placed on the first child and "." and ".." were not
returned. It happens when one of the three mutators runs on the directory between the
moment the handle is opened or rewound and its first read.

**A rewind during the listing step indexes `Children` below zero.** `DirHandle.ReadDir`
serves "." and ".." first and then, if the directory's cache has expired, calls
`loadListing`. That step releases `dh.mu` while the backend is listed. A second user of
the same handle can move it in that window, and there are two cases:

- It seeks the handle to offset 1. Upstream's `ReadDir` comes back from the listing with
  an index of 1, is already past its checks for the dot entries, and indexes `Children`
  at the index minus two. That is a panic with an index out of range. A mutator that
  runs on the directory after the seek does not prevent it: the handle is reset, and
  `checkDirPosition` finds index 1 again from the name ".".
- It rewinds the handle to offset 0, and no mutator runs on the directory between the
  rewind and the end of the listing step. The index is then 0 and the same indexing
  panics. If a mutator does run in that time, for example because the listing itself
  inserts a child in front of an existing one or removes an expired one, the handle is
  reset, the search for its empty name places it on the first child, and upstream does
  not panic; it skips the dot entries instead, which is the second defect.

Either case needs two requests on the same handle to overlap; whether the kernel's FUSE
client lets that happen was not established.

## Upstream behaviour

Each of the three mutators runs a loop over the directory's handles that sets
`lastInternalOffset` to -1, and runs it before it writes `Children`: in
`removeChildUnlocked` after the child has been found and before it is cut out of the
list, in `removeAllChildrenUnlocked` after its loop over the children and before the
list is cleared, and in `insertChildUnlocked` before the list is grown for the
insert in front of an existing child. The loop takes no lock itself; the mutators are
annotated `LOCKS_REQUIRED(parent.mu)`. `checkDirPosition` finds a
negative index again from `lastName`: "." gives offset 1, ".." gives offset 2, and any
other name, the empty one included, is searched for among the children.
`DirHandle.ReadDir` calls `checkDirPosition` once at its start and once more after
`loadListing`, and has no check for a small index after the second call.

Some changes to `Children` do not reset handles upstream, and none of them is changed by
this divergence:

- `insertChildUnlocked` when the new child sorts after every existing one (an append),
  and when the directory is empty.
- `sealDir` itself. It can remove expired children through `removeExpired`, and that
  removal resets handles through `removeChildUnlocked`.
- `ClusterFs.applyStolenInode`, which appends to `Children`, and `ClusterFs.tryYield`,
  which clears it. Both write `Children` directly.

## AkaveFS behaviour

**A counter instead of a write into the handle.** `DirInodeData` has a counter,
`generation`, and `DirHandle` has a field of the same name that holds the counter value
the handle last saw. A mutator adds one to the directory's counter where upstream had
its loop. `checkDirPosition` starts by comparing the two values. If they differ, it sets
the handle's own index to -1, stores the new value, and falls into the inherited code
that finds the position again. The mutators no longer touch a handle, so nothing writes a
handle's fields without `dh.mu`.

The counter is bumped in the same three places where upstream reset the handles:

- `removeChildUnlocked`, when it removes a child. It returns earlier, without a bump,
  when the directory has no children.
- `removeAllChildrenUnlocked`.
- `insertChildUnlocked`, on the path that inserts in front of an existing child.

It is not bumped in the places where upstream did not reset the handles either:

- `insertChildUnlocked` on the append path and on the empty-directory path. Neither moves
  an existing position.
- `sealDir`. A removal inside it bumps through `removeChildUnlocked`.
- `ClusterFs.applyStolenInode` and `ClusterFs.tryYield`.

A bump that is not needed is not harmless, because finding the position again is not
exact for every handle; see the first known limit below. That is why the list above is
pinned by tests.

**How a handle finds its position again.** A handle is not stamped with the counter when
it is created or when `Seek` moves it. A new handle holds zero, so on a directory whose
counter has moved its first `checkDirPosition` finds the position again, whether or not
`Seek` moved the handle before that. In the cases the tests cover this gives the position
the handle already had: a fresh or rewound handle restarts at ".", a handle created by
`isEmptyDir` (which seeks past the dot entries) stays past them, and a handle sought into
the middle of the directory stays on the entry it was sought to.

**"Nothing returned yet" restarts at ".".** `checkDirPosition` has one new branch in
front of the inherited ones: when `lastExternalOffset` is zero, the index becomes 0, so
the handle starts with "." and "..". This fixes the second defect. Every other state goes
through the inherited branches unchanged.

The external offset is what identifies this state, and `lastName` is not. The external
offset is set to zero when a handle is created, by `Seek` with offset 0, and by
`ClusterFs.readDir` when it is called with offset 0; `Next` increments it, and the read
loops call `Next` after each entry they hand out. An empty `lastName` says less. The
root's name is empty, and `Next` records whatever name its caller passes. When this
check was written, two callers passed the inode's name for the dot entries instead of
"." and "..": `RefreshInodeCache` and `ClusterFs.readDir`. A root handle used by one of
them had an empty `lastName` after it had returned "." and "..", exactly like a handle
that has returned nothing. Both callers pass the dot names now (see `dot-entry-names`
in the register), but nothing in `Next` enforces that, so the check stays on the offset. A restart keyed on the empty name would
make that handle return the dot entries a second time. The FUSE and Windows read loops
pass "." and "..".

**After the listing step, a small index is served as a dot entry.** In
`DirHandle.ReadDir`, after `loadListing` and the second `checkDirPosition`, an index
below 2 returns what the start of `ReadDir` returns for it: the directory itself for
index 0, and for index 1 the directory's parent, or the directory itself when it has no
parent. This fixes the third defect. It
also covers a case this divergence would otherwise have added: a handle rewound during a
listing that changes the directory is placed on index 0 by the new branch above, where
upstream placed it on the first child.

**Known limits.** These are open, not solved:

- Closed since: a handle that had returned a dot entry through `RefreshInodeCache` or
  `ClusterFs.readDir` remembered the directory's own name, so an invalidation in that
  state skipped "..". Both callers now pass "." and ".."; see `dot-entry-names` in the
  register. The tests here that check for a needless bump call `Next` themselves with the
  inode's name, so they still use that effect as their detector and were not changed.
- Closed since: `ClusterFs.readDir` used to unlock `dh.mu` on the error path of its read
  loop although the unlock is already deferred. That is fixed and covered by a test; see
  `cluster-readdir-double-unlock` in the register. It was not part of this divergence.
- The inherited comment above the second `checkDirPosition` in `ReadDir` says the index
  may be -1 after `loadListing`. That described the mutators' direct write. The index now
  becomes -1 only inside `checkDirPosition`. The comment was left as it is, because it is
  an inherited line.
- The lock-order test can pass on code it should reject; see "Tests that pin it".

## Invariants and locking

- The annotations in `core/dir.go` are as upstream has them. `checkDirPosition` is
  `LOCKS_REQUIRED(dh.mu)` and `LOCKS_REQUIRED(dh.inode.mu)`. `DirHandle.ReadDir` is
  `LOCKS_REQUIRED(dh.mu)` and `LOCKS_EXCLUDED(dh.inode.mu)`: it takes the directory's
  lock itself. `Next` and `Seek` are `LOCKS_REQUIRED(dh.mu)`. `removeChildUnlocked`
  requires `parent.mu` and `inode.mu`; `removeAllChildrenUnlocked` and
  `insertChildUnlocked` require `parent.mu`.
- The fields of a `DirHandle` below its `mu` are protected by `dh.mu`. That includes the
  handle's `generation`, which is read and written in `checkDirPosition` and nowhere
  else. No code may write a handle's fields while holding the directory's lock without
  `dh.mu`.
- The directory's `generation` is bumped by the mutators, which are annotated as
  requiring the directory's lock, and read by `checkDirPosition`, whose only caller,
  `DirHandle.ReadDir`, holds that lock. It is accessed with atomic operations
  all the same, so that the counter cannot itself be a data race if some caller of a
  mutator does not hold the lock the annotation requires. That each caller holds it has
  not been proven.
- A mutator that shifts a position must bump the counter, and a change that shifts no
  position should not.
- An external offset of zero means that the handle has returned nothing since it was
  opened or rewound. The new branch in `checkDirPosition` depends on that, and so on the
  places that write `lastExternalOffset`, listed under "AkaveFS behaviour".
- The change adds no lock operation and removes none, and it changes no annotation. The
  lock order stays as `AGENTS.md` states it: `dh.mu` before `dh.inode.mu`.
- This divergence leaves `listObjectsFlat`, `sealDir`, `loadListing`, `Seek`, `Next` and
  `NewDirHandle` as upstream has them. It does not touch `core/cluster_fs.go` or
  `core/cluster_fs_fuse.go`; the latter differs from upstream only in the string recorded
  under `branding-runtime`.

## Tests that pin it

All in `core/dir_handle_generation_test.go`. They need no object-store fixture, so they
run locally and under the race detector. The command that selects them, run from inside
`core/`, is:

```sh
CGO_ENABLED=1 go test -race -count=1 -check.f 'DirTest|NoCloud' .
```

That is the JVM-free command in the Gates section of `AGENTS.md`, and the filter of the
fixture-free step of the CI `race` job (see `ci-race-job` in the register), which runs
them in CI. A filter of `'DirTest'` alone does not run them: these tests belong to the
`GoofysTest` suite and are matched only by the `NoCloud` in their names. The grouping
below was observed by running the file against upstream's `core/dir.go`.

**(a) Fail on upstream's code.**

- `TestDirHandleChangeBeforeFirstReadKeepsDotEntriesNoCloud`,
  `TestDirHandleFirstReadAfterChangeListsAllNoCloud` and
  `TestDirHandleSubdirChangeBeforeFirstReadNoCloud` — a directory that changes between
  the opening of a handle and its first read is still listed from ".", on the root and on
  a directory below it.
- `TestDirHandleRewindDuringListingNoCloud`,
  `TestDirHandleSubdirRewindDuringListingNoCloud` and
  `TestDirHandleSubdirSeekDuringListingNoCloud` — a handle that is rewound to offset 0,
  or sought to offset 1, during the listing step gets the dot entries and then the
  children. The tests recover a panic and report it as a failure, so the
  index-out-of-range panic of upstream's code fails them without taking the suite down.
  `TestDirHandleSubdirRewindDuringListingNoCloud` compares inodes, because on a
  directory below the root "." and ".." are different inodes.

**(b) Pass on upstream's code, but the race detector reports the race there.**

- `TestDirHandleNextVersusChildChangeNoCloud` — `Next` against `removeChildUnlocked` and
  against the insert in front of an existing child.
- `TestDirHandleNextVersusRemoveAllNoCloud` — `Next` against
  `removeAllChildrenUnlocked`.

These two reader-versus-mutator tests have no asserts. The race detector is their only
oracle: without `-race` they pass on any code, and with it they report the defect as a
`DATA RACE` between `(*DirHandle).Next` and a mutator while the tests themselves still
count as passed. The exit status is what tells: a run of the fixture-free tests under the
race detector in which every test passes exits non-zero only when a race is reported.
Read the frames of the report in the log: a report that names `Next` and one of the
mutators is this defect. In the CI `race` job the fixture-free step, which runs these
tests, is blocking and fails on such a report; the full-suite step is advisory, so there
the step's conclusion does not show a report and the log has to be read.

How many reports the log holds depends on where the tests run. The fixture-free step of
the CI `race` job sets `GORACE=halt_on_error=1`, so there the process stops at the first
race report and the log holds at most one. A log from that step whose one report is
another race therefore says nothing about this defect. Locally, with `GORACE` unset, the
run continues and every report is printed.

**(c) Characterization tests.** They pass on upstream's code too. Each guards against a
mistake this fix could make, and several of those mistakes were present in the first form
of the port.

- `TestDirHandleRemoveEarlierChildKeepsPositionNoCloud` and
  `TestDirHandleSubdirRemoveEarlierChildNoCloud` fail when the bump is missing from
  `removeChildUnlocked`.
- `TestDirHandleRemoveAllResetsPositionNoCloud` fails when it is missing from
  `removeAllChildrenUnlocked`.
- `TestDirHandleFlatListingKeepsPositionNoCloud` fails when it is missing from the
  insert in front of an existing child. It also fails when `listObjectsFlat` stamps the
  handle after sealing, which hides the listing's own inserts from the handle and makes
  it return an entry twice.
- `TestDirHandleNoopSealKeepsDotEntriesNoCloud`,
  `TestDirHandleAppendKeepsDotEntriesNoCloud` and
  `TestDirHandleFirstChildKeepsDotEntriesNoCloud` fail when a bump is added to `sealDir`,
  to the append path or to the empty-directory path.
- `TestDirHandleSealKeepsLockOrderNoCloud` fails when `listObjectsFlat` releases `dh.mu`
  and takes it again while it holds the directory's lock, which is against the lock
  order.
- `TestDirHandleRewindDuringChangingListingNoCloud` fails when the check after the
  listing step is missing. It compares only the children, because upstream skips the dot
  entries in that situation and the test has to pass there.
- `TestDirHandleRootChangeAfterDotEntriesNoCloud` checks that a root handle past the dot
  entries does not return them again after a change.
- `TestDirHandleRootRefreshAfterChangeNoCloud` checks only that refreshing the root while
  a listing changes it does not panic and returns no error.
- `TestDirHandleSubdirListingKeepsFirstChildNoCloud` checks that a listing during which
  nobody moves the handle is followed by the first child, not by a dot entry.
- `TestDirHandleSeekAfterChangeListsRestNoCloud` checks that a seek into a directory that
  changed after the handle was opened lists from the entry sought to.

The lock-order test relies on timing. Its second user of the handle has to be scheduled
within the test's `callbackWait`. On a starved runner it may not be, and then the test
passes on code that releases `dh.mu`. The opposite error needs more: on correct code
nothing in the test waits for a lock that is not released, so the test fails there only
if the goroutine that runs the listing is held up for longer than `driverWait`.

Two rules hold in the test file, and its header comment explains them: asserts run only
after the filesystem locks are released, and `CloseDir` is called explicitly, not
deferred. A failed assert would otherwise leave a lock held and block the suite's
teardown.

## Upstream status

Upstream has no fix for this: its three mutators still reset the handles in a loop. See
the register entry for when that was checked.

The fix started as a port of TigrisFS `b6eba91` and `d3e4661`, in the first form of #3,
and was reworked in review, so it no longer matches those commits. The counter, the bump
in the three mutators and the comparison in `checkDirPosition` are from the port. The
restart at "." and the check after the listing step are not. These parts of the port were
left out:

- **The bump in `sealDir`.** Sealing does not change `Children` by itself, and a removal
  inside it already bumps. The extra bump invalidated handles each time a directory was
  sealed, which makes a handle skip ".." in the situation described under the first known
  limit.
- **The change to `listObjectsFlat`.** The port released `dh.mu` around `sealDir` and
  took it again while holding the directory's lock, and then stamped the handle with the
  counter value read after the seal. The first is against the lock order and lets two
  users of a handle block each other. The second hides the inserts of the listing itself
  from the handle.
- **The counter comparisons in `loadListing`**, across the windows in which it releases
  the locks, and the reset of the start marker for `removeExpired` that went with one of
  them. `ReadDir` is the caller of `loadListing` and calls `checkDirPosition` right after
  it, so the comparisons add nothing. With an empty start marker `removeExpired` scans
  the directory from its start. The wider scan was not shown to be needed or to be safe, so
  it was left out.
- **The stamps** that copied the counter into a handle in `NewDirHandle`, in `Seek` with
  offset 0, in the cluster `OpenDir` and in `ClusterFs.readDir`. With the restart at "."
  in place an unstamped handle finds the same position, and without the stamps those
  functions and the two cluster files stay as upstream has them.
- **The port's two tests in `core/goofys_test.go`.** They needed the object-store
  fixture. The fixture-free tests in the new file replace them.

The port also deleted the inherited comment above each reset loop. The comments are kept
here, with one added line each, so that the difference from upstream stays small.

## Sync notes

- The change is inside the inherited `core/dir.go`, in `checkDirPosition`,
  `DirHandle.ReadDir`, the three mutators and the two structs `DirInodeData` and
  `DirHandle`. A sync that touches them can conflict. Resolve by hand, never by taking
  one side of the file; `LookUpInodeMaybeDir` in the same file carries another
  divergence (`lookup-maybe-dir-race`).
- Keep the bump in the three mutators and the comparison at the start of
  `checkDirPosition`. Never accept code that writes a field of a `DirHandle` from a
  function that holds the directory's lock without `dh.mu`.
- A merge can bring such code in without any conflict, for example a new function that
  changes `Children` and resets the handles upstream's way. After a sync, search the
  non-test code for an assignment of -1 to `lastInternalOffset`. The one in
  `checkDirPosition` is expected. Replace any other by a bump of the directory's
  `generation`.
- The code depends on inherited code that a sync can change without any conflict. The
  list below is kept here only; the register entry points here instead of repeating it.
  It was derived from the call sites, in the non-test files of `core/`, of the three
  mutators, `checkDirPosition`, `loadListing`, `listObjectsFlat`, `slurpOnce`, `sealDir`,
  `Seek`, `Next` and `NewDirHandle`, and from every read and write of
  `lastInternalOffset`, `lastExternalOffset` and `lastName`. Derive it again the same
  way after a sync, because a merge can add a call site without any conflict. If a sync
  touches any of the code below, re-read this file and run the tests with the command
  under "Tests that pin it"; a filter of `'DirTest'` alone does not run them.
  - Code that writes `Children`: the three mutators in `core/dir.go`, and
    `ClusterFs.applyStolenInode` and `ClusterFs.tryYield` in `core/cluster_fs.go`. For a
    new one, decide whether it can shift a position. If it can, it needs the bump.
  - The callers of the mutators. Each has to hold the directory's lock, as the
    annotation requires; that every one of them does has not been established.
    - `removeChildUnlocked`: `removeExpired`, `removeChild`, `doUnlink`, `Rename`,
      `renameInCache` and `insertSubTree` in `core/dir.go`, and `EvictEntry` in
      `core/goofys.go`. `removeChildUnlessDirty` in `core/refresh_inode_cache.go` calls
      it too; that function is ours (`stale-inode-refresh`), not inherited.
    - `removeAllChildrenUnlocked`: `removeExpired` in `core/dir.go`, and the function
      itself for each child that is a directory.
    - `insertChildUnlocked`: `insertChild`, `doMkDir` and `renameInCache` in
      `core/dir.go`, `insertInode` in `core/goofys.go`, and `ClusterFs.mkDir` in
      `core/cluster_fs.go`.
  - The handle's position fields. All three are written by `Seek` and `Next` in
    `core/dir.go` and by `ClusterFs.readDir` in `core/cluster_fs.go`, which rewinds the
    handle itself when it is called with offset 0; `lastInternalOffset` is also written
    by `checkDirPosition`. No function that creates a handle writes them:
    `NewDirHandle` sets only the inode, so a new handle has zero in all three. The
    restart at "." relies on a `lastExternalOffset` of zero meaning "nothing returned
    yet". The readers are:
    - `lastExternalOffset`: `checkDirPosition` and `Seek`, and the read loops of the
      FUSE `ReadDir` in `core/goofys_fuse.go`, `ClusterFs.readDir` and `Readdir` in
      `core/goofys_windows.go`, which take the offset of the entry they hand out from
      it. The Windows loop, and `makeDirEntry` in `core/goofys_fuse.go` for the other
      two, also choose between ".", ".." and the inode's name by it.
    - `lastInternalOffset`: `checkDirPosition`, `Seek`, `Next`, `DirHandle.ReadDir`, and
      `RefreshInodeCache` in `core/goofys.go`, which uses it to tell the dot entries
      from the children.
    - `lastName`: `checkDirPosition` only.
  - The functions that create a handle with `NewDirHandle`: `Inode.OpenDir`,
    `isEmptyDir` and `RmDir` in `core/dir.go`, and `ClusterFs.openDir` in
    `core/cluster_fs.go`.
  - The callers of `Seek`: the FUSE `ReadDir` and the Windows `Readdir`, which pass the
    offset the kernel asked for, and `isEmptyDir` and `RmDir`, which seek their own
    handle past the dot entries and read one entry from it.
  - The read loops that call `DirHandle.ReadDir` and then `Next`: the FUSE `ReadDir` in
    `core/goofys_fuse.go`, `RefreshInodeCache` in `core/goofys.go`, `ClusterFs.readDir`
    in `core/cluster_fs.go` and `Readdir` in `core/goofys_windows.go`. These four are all
    the callers of `Next`. What matters is that `Next` follows each entry handed out,
    and which name each loop passes to it.
  - `DirHandle.ReadDir`, the only caller of `checkDirPosition` and of `loadListing`.
  - The listing code, for where `dh.mu` and the directory's lock are released during a
    listing and which mutators a listing reaches:
    - `loadListing` in `core/dir.go`. It calls `slurpOnce` with both `dh.mu` and the
      directory's lock released, and `listObjectsFlat` with the directory's lock
      released. The check after the listing step in `ReadDir` exists because `dh.mu` is
      released there.
    - `slurpOnce` and `listObjectsSlurp`, which it calls, in `core/dir.go`.
      `loadListing` is the only caller of `slurpOnce`. `listObjectsSlurp` inserts
      through `insertSubTree` and calls `sealDir`, on the directory being read and on
      others. `Rename` and `LookUp` call `listObjectsSlurp` as well.
    - `listObjectsFlat` in `core/dir.go`, which releases `dh.mu` while the backend is
      listed and calls `sealDir`. It has a second caller outside `DirHandle.ReadDir`:
      `DirHandle.loadChildren` in `core/cluster_fs.go`, which `ClusterFs.readDir` runs
      before its read loop, holding `dh.mu` and not the directory's lock.
    - `sealDir`, called by `listObjectsSlurp`, `listObjectsFlat` and `loadListing`, and
      `removeExpired`, called by `sealDir` and `loadListing`. `removeExpired` is where
      a listing reaches `removeChildUnlocked` and `removeAllChildrenUnlocked`.
  - `findInodeFunc`, which the inherited search in `checkDirPosition` uses, and the
    sorted order of `Children` it relies on.
- If upstream changes how `ReadDir` indexes `Children` after the listing step, keep a
  check that an index below 2 is served as a dot entry, or confirm that upstream's new
  code cannot reach the indexing with such an index.
- If upstream fixes the race itself, follow the policy in [README.md](README.md): audit
  their fix and compare it with ours. The points to compare are that no handle field is
  written without `dh.mu`, that no lock operation is added against the lock order, that
  a handle which has returned nothing restarts at ".", that a rewind during the listing
  step cannot index `Children` below zero, and which changes to `Children` invalidate
  handles. Run the tests in `core/dir_handle_generation_test.go` against their code; the
  tests do not refer to the counter, so they compile without it. Record the outcome in
  the register and in the sync log.
