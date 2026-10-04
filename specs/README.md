# AkaveFS specs

AkaveFS is a fork of GeeseFS (`yandex-cloud/geesefs`) that keeps merging upstream changes.
This folder records how AkaveFS differs from GeeseFS, why each difference exists, and what
to do when an upstream sync touches it.

## Purpose

The folder holds three kinds of document:

- [divergences.md](divergences.md) — the register. One entry for each difference
  between AkaveFS and GeeseFS, covering every file that differs. Most differences are
  deliberate; the entry's **Status** says when one is not.
- [upstream-syncs.md](upstream-syncs.md) — the sync log. One entry for every upstream
  sync, listing what came in, how each change was audited, and which register entries
  were re-checked.
- Per-divergence specs — one file for each divergence that changes behaviour and needs
  more room than a register entry: [stale-inode-refresh.md](stale-inode-refresh.md),
  [lookup-maybe-dir-race.md](lookup-maybe-dir-race.md),
  [rename-cold-target.md](rename-cold-target.md) and
  [dir-handle-invalidation.md](dir-handle-invalidation.md).

Behavioural specs of AkaveFS itself (what the filesystem promises, POSIX semantics) are a
separate piece of work. When they are written, they go under `specs/behaviour/`.

## Read this first

Before any change, read this file and the [divergences.md](divergences.md) entries for
the files you touch (and the linked spec, if the entry has one). They tell you whether
the code you are about to touch already differs from upstream, and what that means for
the next sync between this repository and upstream. A file that no entry lists should
not differ from upstream; the check under "Regenerating the diff" is how to confirm it.

A change that adds, alters or removes a divergence updates `specs/` in the same pull
request. A register that lags behind the code is worse than no register, because the next
sync will trust it.

## Upstream policy

Points 1 and 2 say how a defect in inherited code is handled when we work on it. They are
not a duty to fix every inherited defect met in passing.

1. **When fixing a defect in inherited code that upstream has not fixed, fix it here.**
   We do not wait for upstream. The fix is recorded in the register with the status
   `ours — no upstream fix`, so the next sync knows the code is ours and why.
2. **When upstream has fixed it, audit their fix before taking it.** Read upstream's
   fix and judge whether it is good. If we already have our own fix for the same defect,
   compare the two. Then adopt upstream's, keep ours, or combine them, and record the
   outcome and the reason in `specs/`. An upstream fix is never taken on trust, and ours
   is never kept out of habit.
   [rename-cold-target.md](rename-cold-target.md) is the worked example: we had our own
   fix for a rename deadlock, upstream fixed the same deadlock differently, the two were
   compared, upstream's was adopted, and the one case only our tests covered was kept as
   a regression test.
3. **Upstream is synced when a client asks, or roughly every one to two months.** Every
   sync audits each incoming upstream change as in point 2, re-checks the `specs/`
   entries against it, and records both in [upstream-syncs.md](upstream-syncs.md).

## Regenerating the diff

The register is kept by hand. The commands below give the ground truth to check it
against. They assume the remotes are named `origin` (this repository) and `upstream`
(GeeseFS); `AGENTS.md` says how to add `upstream`.

```sh
git fetch upstream
BASE=$(git merge-base origin/master upstream/master)

# Every file that differs from the last synced upstream commit.
git diff --stat $BASE origin/master

# The difference in one file.
git diff $BASE origin/master -- <path>

# Upstream commits that have not been synced yet.
git log --oneline origin/master..upstream/master
```

`BASE` is the upstream commit that the most recent sync merged. It is computed, never
written down here, because it moves with every sync.

Every path printed by `git diff --name-only $BASE origin/master` must appear in the
**Files** field of at least one register entry. If one does not, either the register is
missing an entry or a change slipped into an inherited file unrecorded.

## Entry format

Each entry in [divergences.md](divergences.md) is a `## <slug>` heading followed by these
fields, each written as a `- **Field:** …` bullet:

- **Status** — exactly one of:
  - `ours — no upstream fix`: our own fix for a defect that upstream has not fixed.
  - `ours — addition`: ours, with nothing equivalent upstream, and no defect involved —
    for example tooling, CI or documentation.
  - `upstream fix adopted`: upstream fixed it and we took their fix. What remains ours is
    listed in the entry.
  - `ours kept over upstream`: upstream fixed it, we audited their fix and kept ours.
  - `combined`: the code carries parts of both fixes.
  - `branding`: the AkaveFS name in place of the GeeseFS name, including the build and
    packaging mechanisms needed to deliver that name. An entry with this status says
    which of its parts are naming and which are mechanism.
  - `kept GeeseFS name`: a GeeseFS name that is still in the tree. This is not a
    divergence. The entry separates the names left unchanged on purpose, recorded so
    that nobody "finishes" the rename, from the names that were found afterwards and
    whose keep-or-rename has not been decided.
  - `incidental`: a difference with no purpose, kept because removing it is not worth
    a change of its own.
- **Files** — every repository path the entry covers, each written out in full in
  backticks.
- **Symbols** — the functions, types, flags or settings involved.
- **Introduced by** — the pull request and commit that brought it in.
- **Origin** — where the idea came from: original work, a commit in a sibling fork, or
  an upstream commit.
- **Why** — the reason the divergence exists.
- **On sync conflict** — what to do when an upstream merge conflicts with it.
- **Upstream status** — whether upstream has an equivalent, and what would change this
  entry.

## Conventions

- Entries and spec files are named by slug, not by number, so two pull requests can each
  add one without colliding. New entries are appended.
- A file may appear in several entries when it carries several divergences.
- No measured numbers that go stale: no totals of files, lines, tests or races, and no
  line numbers. Name the function or the test instead. Saying how many things a sentence
  then names is fine.
- A statement about the state of a tree at some commit ("as of") belongs only in the sync
  log. Commit hashes that record provenance — the **Introduced by** and **Origin**
  fields, and references to an audited commit — are allowed anywhere, because they do not
  change.
- Pull request numbers written as `#n` refer to this repository unless the text says
  upstream.
