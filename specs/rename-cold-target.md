# Rename onto a cold destination directory

Register entry: `rename-cold-target` in [divergences.md](divergences.md).

This file is the worked example for the upstream policy in [README.md](README.md): we had
a fix, upstream fixed the same defect, the two were compared, and upstream's was adopted.

## Problem

Renaming a directory onto an existing directory is only allowed when the destination is
empty, so `Rename` has to check. It did so by calling `isEmptyDir` on the destination
while holding the lock of the parent directory and the lock of the inode being renamed.

When the destination's cached listing had expired (it was "cold"), `isEmptyDir` had to
list it from the object store, and it did so with a slurp: a wide listing that also fills
in the surrounding tree. The slurp tried to lock inodes that `Rename` already held, and
the rename deadlocked on itself. Where it blocked depended on the parent:

- When the parent is the mount root, the slurp locks the root itself.
- When the parent is a nested directory, the root lock succeeds and the slurp blocks
  later, when it inserts the listed subtree and locks the nested parent.

## Upstream behaviour

Upstream fixed this in `c0f0e84` (upstream pull request #199, "fix(rename): avoid slurp
while checking destination directory"). `isEmptyDir` sets `noSlurp` on the directory
handle it uses, so the check performs a flat listing of the destination and takes no lock
that `Rename` holds. This covers both the root and the nested case.

Upstream's test, `TestRenameDirExpiredDestinationNoCloud`, covers only a parent that is
the mount root.

## AkaveFS behaviour

The production code is upstream's, unchanged. AkaveFS adds only
`core/rename_cold_target_test.go`, which covers the nested-parent case.

## Audit outcome

- **Our fix:** #2, "Prevent cold-cache rename self-deadlock", inspired by TigrisFS
  `e3b5259`. It added a separate emptiness check (`isEmptyDirFast` and `hasLiveChild`)
  that listed the destination with a scoped, paginated listing without holding the inode
  lock, and a regression test, `TestRenameDirColdTarget`, for a nested parent.
- **Upstream's fix:** `c0f0e84`, described above. It reached master through the sync in
  #7.
- **Decision:** adopt upstream's fix. #2 was closed without being merged.
- **Why**, from the closing comment on #2:
  - Upstream's fix resolves the deadlock for a root and for a nested parent.
  - It is smaller, and it was already on master through the sync.
  - Keeping both would conflict in `core/dir.go` on every future sync.
  - Our analysis of #2 suggested a hazard: when a listing page comes back truncated and
    empty with no continuation token, #2's check can treat a non-empty directory as empty
    and let a rename overwrite it. This was an analysis result, not a reproduced failure.
- **What we kept:** the scenario. #2 found the nested-parent case, which upstream's test
  does not cover. #8 rewrote #2's test as two fixture-free tests and merged them.

## Invariants and locking

- `isEmptyDir` must not slurp. `Rename` calls it with the parent's lock and the renamed
  inode's lock held, and a slurp locks inodes on the path it fills in.
- An empty destination directory is replaced by the rename.
- A non-empty destination directory is refused with `ENOTEMPTY`, and both directories are
  left as they were.
- The check must list the cold destination from the backend. An answer taken from an
  expired cache is not a check.

## Tests that pin it

In `core/rename_cold_target_test.go`, both fixture-free and both inside a directory that
is not the mount root:

- `TestRenameNestedColdEmptyTargetNoCloud` — the empty destination is replaced; the rename
  succeeds and the target name then refers to the source's inode.
- `TestRenameNestedColdNonEmptyTargetNoCloud` — the non-empty destination is refused with
  `ENOTEMPTY`.

Both assert that the cold destination really was listed from the backend. Each rename is
bounded by a timeout, so a deadlock fails the test instead of hanging the whole run.

The description of #8 states that both tests were observed failing with upstream's
`core/dir.go` change reversed, and passing with it.

Upstream's `TestRenameDirExpiredDestinationNoCloud`, in `core/goofys_fs_test.go`, covers
the root-parent case and is inherited.

## Upstream status

Fixed upstream in `c0f0e84` and adopted. The nested-parent tests exist only here.

## Sync notes

- The test file exists only here and cannot conflict.
- If either test fails after a sync, upstream has changed how `Rename` checks the
  destination, or has reintroduced a slurp on that path. Find the upstream change and
  audit it. Do not weaken or delete the tests, or raise the timeout, to make them pass.
- If upstream adds its own nested-parent test, ours can be removed once theirs is shown to
  fail on the unfixed code. Record that in the register and the sync log.
