# Upstream sync log

This file has one entry for every sync of upstream GeeseFS (`yandex-cloud/geesefs`) into
AkaveFS. It exists so that the next person to sync can see what was audited last time,
what was decided and why, and which entries of [divergences.md](divergences.md) were
re-checked.

`AGENTS.md` describes how to perform a sync (merge, never rebase or squash).
[README.md](README.md) states the policy: syncs happen when a client asks or roughly every
one to two months, and no upstream change is adopted without an audit.

This is the only file in `specs/` where statements about the state of a tree at a given
commit ("as of") belong. It does not list upstream commits that have not been synced yet;
that list goes stale, and the command in [README.md](README.md) prints it.

## Template

Copy this block to the end of the file for each sync and fill in every field.

```markdown
## Sync of `<upstream short sha>` (<yyyy-mm-dd>)

- **Upstream commit merged:** `<short sha>` (<upstream version tag, if any>)
- **Sync pull request:** #<n>
- **Merge commit:** `<short sha>`
- **Conflicts:** <each conflicting file and how it was resolved, or "none">

| Upstream change | What it does | Audit outcome | Reason |
|---|---|---|---|
| <upstream PR or sha> | <one sentence> | adopt / keep ours / combine | <why> |

- **Specs entries re-checked:** <each slug in the register, with "unchanged", "updated"
  or "removed", and what changed>
```

The audit outcome is one of:

- **adopt** — upstream's change is taken as it is. If we had our own fix for the same
  defect, it is removed, and the register entry moves to `upstream fix adopted`.
- **keep ours** — upstream's change to that code is not taken; the register entry moves to
  `ours kept over upstream`.
- **combine** — the result carries parts of both; the register entry moves to `combined`.

## Sync of `dd84777` (2026-09-17)

- **Upstream commit merged:** `dd84777` — upstream release `v0.43.9` plus the changes
  merged upstream after it.
- **Sync pull request:** #7
- **Merge commit:** `ab34211` on master. The merge of upstream itself is `e046564`, on the
  sync branch.
- **Conflicts:** one, in `debian/changelog`. It was resolved by keeping both histories: a
  new `akavefs` entry for the merged version on top, then upstream's `geesefs` entry and
  our earlier `akavefs` entry unchanged. The other files that both sides had changed
  merged cleanly.

This sync predates the audit policy and this log. The outcomes below are reconstructed
from the description of #7 and from the closing comment on #2; they were not written down
as audits at the time.

| Upstream change | What it does | Audit outcome | Reason |
|---|---|---|---|
| Upstream #199 (`c0f0e84`) | Stops `Rename` from deadlocking on itself when the destination directory's listing is cold: `isEmptyDir` uses a flat listing (`DirHandle.noSlurp`) instead of a slurp. | adopt | It fixes the same deadlock as our #2, for a root and a nested parent alike, with a smaller change, and keeping both would conflict on every sync. #2 was closed unmerged and its nested-parent case was kept as tests in #8. See [rename-cold-target.md](rename-cold-target.md). |
| `80c214e` (release `v0.43.9`) | Version bump and changelog. | adopt | Release bookkeeping. It causes the `debian/changelog` conflict described above. |
| Upstream #201 (`fbf052a`) | Fixes a lost wakeup in `SyncFile`, on the write path, and adds `core/file_sync_test.go`. | adopt | We had no fix of our own to compare. Its test, `TestSyncFileDoesNotMissFlushCompletion`, passed locally under the race detector on the merged tree. |
| Upstream #201 (`c3746fd`) | Runs the FUSE tests out of process: CI no longer sets `SAME_PROCESS_MOUNT=1`. | adopt | It is upstream's CI default and we followed it. Commit `d163a06`, in the same pull request, updated the test gate in `AGENTS.md` to match. The `race` job added later keeps the variable on purpose; see `ci-race-job` in the register. |

- **Specs entries re-checked:** none at the time, because the register did not exist. It
  was first written afterwards, as of origin master `ddd4732`, and each entry's
  **Upstream status** was checked against upstream master as of `2fe4d9c`. That is the
  check the entries mean when they refer to this log.

## Sync of `2fe4d9c` (2026-10-05)

- **Upstream commit merged:** `2fe4d9c` — three commits after the previous sync; no new
  upstream release.
- **Sync pull request:** #32
- **Merge commit:** `a15b8d3` on master. The merge of upstream itself is `a6dd193`, on the
  sync branch.
- **Conflicts:** none.

| Upstream change | What it does | Audit outcome | Reason |
|---|---|---|---|
| Upstream #206 (`b3a2cec`) | On the S3 backend, a self-copy — used only to rewrite an object's metadata — that the server rejects with `EntityTooLarge` is retried as a multipart copy. The multipart copy helper also aborts an upload it created itself when the copy fails. Adds `core/backend_s3_copy_test.go`. | adopt | We had no fix of our own to compare. The retry is limited to a self-copy, to that one error code and to non-GCS backends, and the forced pass cannot retry again. The object is replaced only when the multipart upload completes, parts are copied on condition that the source's ETag still matches, and an abort discards parts and never the object. The abort leaves an upload id supplied by the caller alone. TigrisFS took the same change as a cherry-pick (its #53, `8455161`), so there was no second design to weigh. Its test passed locally under the race detector on the merged tree, and failed when the retry was disabled. Two limits are shared with the large-copy path that already existed and are not new: the forced pass reads the size and ETag afresh, so it guards against a change during the copy and not before it, and a multipart copy does not carry object tags. |
| Upstream #209 (`599ad44`, `2fe4d9c`) | Deletes `.gitmodules` and `core/import os.py`. | adopt | `.gitmodules` described submodules under `vendor/` that the tree does not contain, and nothing in the build, the workflows or the test scripts refers to either file. |

- **Specs entries re-checked:** every entry, all unchanged. The incoming commits touch
  four files. Of those only `core/backend_s3.go` is named by the register, in
  `branding-runtime` and `kept-geesefs-names`, and what those entries describe in it is
  as it was. No incoming commit fixes a defect that an `ours — no upstream fix` entry
  covers, so each such entry's **Upstream status** now holds as of `2fe4d9c`.
