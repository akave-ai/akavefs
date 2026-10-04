# Divergences from GeeseFS

This is the register of the differences between AkaveFS and upstream GeeseFS
(`yandex-cloud/geesefs`), one entry each. Most are deliberate; an entry's **Status** says
when one is not. [README.md](README.md) explains the entry format, the policy
behind the **Status** values, and the commands that regenerate the underlying diff.

Before any change, read [README.md](README.md) and the entries here for the files you
touch (and the linked spec, if the entry has one). A change that adds, alters or removes
a divergence updates `specs/` in the same pull request.

## branding-runtime

- **Status:** `branding`
- **Files:** `core/cfg/flags.go`, `core/goofys_fuse.go`, `core/cluster_fs_fuse.go`, `core/backend_s3.go`, `core/cfg/flags_test.go`
- **Symbols:** `NewApp` (the application name `akavefs` and the flag usage strings that name the product); `mountFuseFS` and `MountCluster` (FUSE `Subtype: "akavefs"`); `newS3` (the `AkaveFS` User-Agent product name); `TestNewAppUsesAkaveFSName`.
- **Introduced by:** #1 (commits `650b125`, `b577179`, `34d6497`).
- **Origin:** original.
- **Why:** The product is called AkaveFS, so these run-time names were changed: the command name in help output and the two flag descriptions that named the product, the FUSE subtype shown in the mount table, and the product name in the User-Agent sent to the object store. That is what was renamed, not every run-time string: `kept-geesefs-names` lists the GeeseFS names still in the tree, among them an error message in `core/goofys_windows.go`. The FUSE subtype also has to match the name that the xfstests configuration mounts (see `scripts-and-test-harness`). The User-Agent keeps the inherited version constant; only the product name changed.
- **On sync conflict:** Keep the AkaveFS strings and take everything else from upstream. Each change is a single string, so resolve by hand rather than choosing one side of the file. If upstream adds a new user-visible string that names GeeseFS, rename it in the sync pull request and add its file here. `TestNewAppUsesAkaveFSName` fails if the application name is lost.
- **Upstream status:** Not applicable; upstream keeps its own name. This entry stays for as long as the fork has its own name.

## cli-help

- **Status:** `ours — no upstream fix`
- **Files:** `main.go`, `main_test.go`, `.github/workflows/test.yml`
- **Symbols:** the `app.Action` closure in `main`; `cli.ShowAppHelp`; `TestHelpUsesAkaveFSBranding`; the `Verify CLI help` step of the `build` job.
- **Introduced by:** #1 (commit `650b125`).
- **Origin:** original.
- **Why:** The application hides the CLI library's built-in help and declares its own `help` flag, so `--help` reaches the action like any other flag. Upstream's action checks first that exactly two arguments were given, so `--help` on its own is treated as a usage error: the help text is printed and the process exits with a failure status. AkaveFS returns the help text before that check, so `akavefs --help` succeeds. CI relies on this: the `Verify CLI help` step runs `./akavefs --help` and would fail the build otherwise, and `TestHelpUsesAkaveFSBranding` builds the binary and checks both the exit status and the name in the output.
- **On sync conflict:** Keep the early `help` return as the first statement of the action, ahead of the argument-count check, and take upstream's other changes to the action. This is a change to inherited logic, not a string, so re-read the whole action after merging: if upstream starts handling `--help` itself, audit its version, and drop ours only if `TestHelpUsesAkaveFSBranding` still passes without it.
- **Upstream status:** No upstream equivalent was found when this entry was last re-checked; the sync log records when that was.

## build-packaging-release

- **Status:** `branding`
- **Files:** `Makefile`, `Dockerfile.build`, `.gitignore`, `.github/workflows/release.yml`, `.github/workflows/test.yml`, `debian/control`, `debian/copyright`, `debian/install`, `debian/rules`, `debian/upstream/metadata`, `debian/changelog`
- **Symbols:** `Makefile`: the `BINARY` and `GOBIN` variables, the `build` and `install` targets, and the `akavefs-builder` image name in the docker targets. `.github/workflows/release.yml`: the `akavefs-*` artefact names. `.github/workflows/test.yml`: the `Build linux amd64` step of the `build` job. `debian/rules`: `override_dh_auto_build` and the install override.
- **Introduced by:** #1 (commit `650b125`); the newest `debian/changelog` entry by #7 (merge commit `e046564`).
- **Origin:** original.
- **Why:** The built, packaged and released executable is named `akavefs`. The entry has two kinds of change.

  Naming — the name, and the repository and contact details that go with it: the `BINARY` value, the `akavefs-builder` image name and the `akavefs-*` release artefact names; the binary name in `Dockerfile.build` and `.gitignore`; the package name, the description and the repository addresses in `debian/control`, `debian/copyright` and `debian/upstream/metadata` (`debian/copyright` also gained a comment that AkaveFS is derived from GeeseFS); the documentation directory in `debian/install`; and the `akavefs` entries in `debian/changelog`, interleaved with upstream's `geesefs` entries.

  Mechanism — changes to how the binary is built, which are easy to lose in a merge:
  - `make build` now writes an explicitly named binary, and `make install` depends on `build` and copies that binary into `GOBIN` with `install -Dm755`. Upstream's `install` target runs `go install`, which would name the binary after the module path, that is `geesefs`. This mechanism is needed to deliver the name.
  - `debian/rules` builds the binary itself with `go build` into `_build/akavefs` and installs it through `debian/install`, for the same reason: the default Debian Go build would produce a binary named after the module path. This mechanism is needed to deliver the name.
  - The CI `build` job builds with `make build` instead of its own `go build` line, so CI exercises the same target a developer uses. This one is not needed for the name — a renamed `go build` line would have delivered it — and is recorded here because it arrived with the rename and touches the same step.
- **On sync conflict:** Keep the `akavefs` names and the three mechanism changes above; take upstream's unrelated changes (new targets, new build flags, new release platforms) and rename any new artefact to `akavefs-*`. `debian/changelog` conflicts whenever upstream releases: keep both histories verbatim and add a new `akavefs` entry on top for the merged upstream version, as the sync in #7 did. Do not delete or rewrite upstream's `geesefs` entries. After resolving, run the build gate from `AGENTS.md`.
- **Upstream status:** Not applicable; upstream builds and ships under its own name.

## scripts-and-test-harness

- **Status:** `branding`
- **Files:** `bench/format_bench.sh`, `bench/format_bench3.sh`, `bench/run_bench.sh`, `contrib/ftp-gateway/Dockerfile`, `contrib/ftp-gateway/start.sh`, `contrib/ftp-gateway/README.md`, `test/cluster/mount.sh`, `test/run-xfstests.sh`, `test/xfstests.config`, `core/goofys_unix_test.go`
- **Symbols:** `PROG` and `PROG1` in the bench scripts; `FS_BIN` in `test/cluster/mount.sh`; `FUSE_SUBTYP` in `test/xfstests.config`; the default executable path used by the out-of-process mount in `core/goofys_unix_test.go`.
- **Introduced by:** #1 (commits `650b125`, `37d5f16`).
- **Origin:** original.
- **Why:** Every script and test that launches or looks for the executable has to use its new name, otherwise it fails to find the binary. Two of these are functional rather than cosmetic. `FUSE_SUBTYP=.akavefs` in `test/xfstests.config` must match the FUSE subtype set in `branding-runtime`, and `test/run-xfstests.sh` links the binary under the name xfstests then mounts. `core/goofys_unix_test.go` launches `../akavefs` as the FUSE server when tests run out of process, which is why the `build` job must build the binary before the tests. The FTP gateway image downloads the AkaveFS release binary instead of the GeeseFS one.
- **On sync conflict:** Keep the `akavefs` binary name, path and subtype; take upstream's logic changes. In `core/goofys_unix_test.go` only the default path differs, so resolve that one line by hand. If upstream adds a script that calls `geesefs`, rename the call in the sync pull request and add the file here.
- **Upstream status:** Not applicable.

## user-docs

- **Status:** `branding`
- **Files:** `README.md`, `README-azure.md`
- **Symbols:** none.
- **Introduced by:** commit `8216d2f`, made directly on master before #1, rewrote `README.md`; #1 (commit `650b125`) renamed the command in both files.
- **Origin:** original.
- **Why:** `README.md` was rewritten for AkaveFS, not just renamed. Upstream's sections on the POSIX compatibility matrix, stability, common issues, benchmarks and references were removed or reshaped, and sections on project lineage, Akave object storage and acknowledgements were added. `README-azure.md` has the product name and the command name changed, and one sentence lost the word "still" along the way.
- **On sync conflict:** `README.md` is expected to conflict on almost any upstream edit, because the two files no longer share a structure. Keep ours. Then read what upstream changed: if it documents a new flag, a new behaviour or a corrected instruction, port that information into the matching AkaveFS section by hand. In `README-azure.md`, take upstream's text and re-apply the name.
- **Upstream status:** Not applicable.

## working-agreement

- **Status:** `ours — addition`
- **Files:** `AGENTS.md`, `CLAUDE.md`, `specs/README.md`, `specs/divergences.md`, `specs/upstream-syncs.md`, `specs/stale-inode-refresh.md`, `specs/lookup-maybe-dir-race.md`, `specs/rename-cold-target.md`, `specs/dir-handle-invalidation.md`
- **Symbols:** none.
- **Introduced by:** `AGENTS.md` and `CLAUDE.md` by commit `190a27a`, then edited by `d163a06` (in #7) and by #5 (`ddd4732`). The `specs/` folder by #13, which also edited `AGENTS.md`: the sync cadence, the rule to read `specs/` before any change, the "Defects" rule, and the sync rule's pointer to this register and to the sync log.
- **Origin:** original.
- **Why:** These files say how work is done in this repository and how the fork relates to upstream. `AGENTS.md` is the working agreement and the gate commands; `CLAUDE.md` only points to it. `specs/` is this register, the sync log and the per-divergence specs. Upstream has no counterpart to any of them. They are listed here, including this file, so that every path that differs from upstream has an entry.
- **On sync conflict:** None is expected, because upstream has no files at these paths. If upstream ever adds a file with one of these names, keep ours and report the collision in the sync pull request. A sync that changes upstream's CI commands does need a matching edit to the Gates section of `AGENTS.md`, which mirrors `.github/workflows/test.yml`.
- **Upstream status:** No upstream counterpart.

## ci-race-job

- **Status:** `ours — addition`
- **Files:** `.github/workflows/test.yml`, `AGENTS.md`
- **Symbols:** the `race` job and its steps `Run fixture-free race tests`, `Fetch s3proxy` and `Run core race tests`; `SAME_PROCESS_MOUNT`; the "Races" bullet under Gates in `AGENTS.md`.
- **Introduced by:** #5 (commit `ddd4732`).
- **Origin:** original.
- **Why:** The CI jobs inherited from upstream run no race detector, so a regression test that only fails under `-race` (such as the ones for `lookup-maybe-dir-race`) could not fail in CI. The `race` job runs the fixture-free tests and then the full core suite under the race detector. It is advisory: the two test steps carry `continue-on-error`. The races inherited from GeeseFS that remain are in the full-suite step, and clearing them is separate work; the comment on that step says when to make it blocking: when a run reports zero races. The fixture-free step is expected to report none, and stays advisory until a separate decision makes it blocking. The job sets `SAME_PROCESS_MOUNT=1` on purpose. The `build` job no longer sets it, following upstream, but the `race` job builds no binary, so without the variable the suite would try to launch an executable that does not exist, and an out-of-process FUSE server would not be instrumented anyway.
- **On sync conflict:** Keep the whole `race` job; it is appended after upstream's jobs, so a conflict means upstream added a job or changed the end of the file — keep both. If upstream changes how the `build` job runs tests (environment variables, scripts, the s3proxy version or cache key), check each change against the `race` job's steps and comments, which depend on `test/run-tests.sh`, the `s3proxy.jar` make target and `SAME_PROCESS_MOUNT`. Because the step conclusions are advisory, read the job log for the race reports and the package result rather than trusting the green mark. Update the "Races" bullet in `AGENTS.md` in the same pull request as any change to the job.
- **Upstream status:** No upstream equivalent was found when this entry was last re-checked; the sync log records when that was. If upstream adds its own race job, audit it and combine, keeping the fixture-free step.

## stale-inode-refresh

- **Status:** `ours — no upstream fix`
- **Files:** `core/refresh_inode_cache.go`, `core/refresh_inode_cache_test.go`, `core/goofys.go`
- **Symbols:** `refreshCurrentChild`, `removeChildUnlessDirty`, `isDirtyLocked` (all new, in the new file); the single call in `RefreshInodeCache` that used to call `recheckInode`.
- **Introduced by:** #4 (commit `0650d7d`).
- **Origin:** ported from TigrisFS `97e8dc9`, then reworked in review.
- **Why:** Invalidating an inode (`setfattr -n .invalidate`) silently did nothing when the kernel still held an inode that AkaveFS had already replaced under the same name, so a deleted object stayed visible. See [stale-inode-refresh.md](stale-inode-refresh.md).
- **On sync conflict:** Follow the "Sync notes" section of [stale-inode-refresh.md](stale-inode-refresh.md). It is the single source for what to keep in `core/goofys.go` and for the inherited functions this code depends on, which a sync can change without any conflict; the list is not repeated here.
- **Upstream status:** No upstream fix was found when this entry was last re-checked; the sync log records when that was. If upstream fixes the same defect, audit and compare as the spec's sync notes describe.

## lookup-maybe-dir-race

- **Status:** `ours — no upstream fix`
- **Files:** `core/dir.go`, `core/dir_lookup_test.go`
- **Symbols:** `LookUpInodeMaybeDir`; inside it the `lookupResult` type, the `lookupObject`, `lookupDirObject` and `lookupPrefixList` kinds, and the `receiveResult` closure; the tests `TestLookUpInodeMaybeDirConcurrentResults`, `TestLookUpInodeMaybeDirObjectResult` and `TestLookUpInodeMaybeDirDirObjectResult`.
- **Introduced by:** #6 (commit `a81b4e8`).
- **Origin:** original.
- **Why:** The probe goroutines in `LookUpInodeMaybeDir` wrote shared variables that the caller read without synchronisation, which is a data race. See [lookup-maybe-dir-race.md](lookup-maybe-dir-race.md).
- **On sync conflict:** Follow the "Sync notes" section of [lookup-maybe-dir-race.md](lookup-maybe-dir-race.md). It is the single source for how to resolve a conflict in `LookUpInodeMaybeDir` and what to do when upstream changes a probe; it is not repeated here.
- **Upstream status:** No upstream fix was found when this entry was last re-checked; the sync log records when that was.

## rename-cold-target

- **Status:** `upstream fix adopted`
- **Files:** `core/rename_cold_target_test.go`
- **Symbols:** `TestRenameNestedColdEmptyTargetNoCloud`, `TestRenameNestedColdNonEmptyTargetNoCloud`. The production fix is upstream's and is not a divergence: `isEmptyDir` and `DirHandle.noSlurp`.
- **Introduced by:** #8 (commit `a402515`) for the tests. The production fix arrived through the sync in #7.
- **Origin:** upstream `c0f0e84` (upstream pull request #199) for the fix. The tests are a fixture-free rewrite of the test in our closed #2, which was inspired by TigrisFS `e3b5259`.
- **Why:** A rename onto a directory whose listing had expired could deadlock. We had our own fix (#2); upstream fixed the same deadlock differently; after comparing them we adopted upstream's and kept only the tests for the case upstream's own test does not cover. See [rename-cold-target.md](rename-cold-target.md).
- **On sync conflict:** Follow the "Sync notes" section of [rename-cold-target.md](rename-cold-target.md). It is the single source for what to do when these tests fail after a sync; it is not repeated here.
- **Upstream status:** Fixed upstream and adopted. The nested-parent tests exist only here.

## whitespace

- **Status:** `incidental`
- **Files:** `Dockerfile.build`, `Makefile`, `test/xfstests.config`
- **Symbols:** none.
- **Introduced by:** #1 (commit `650b125` for the final newline in `Dockerfile.build` and `Makefile`; commit `37d5f16` for the blank line in `test/xfstests.config`).
- **Origin:** original.
- **Why:** There is no reason beyond history. The rename commits rewrote these files and changed how each one ends. The differences are: in `Dockerfile.build` and in `Makefile`, our last line ends with a newline and the other side's does not; in `test/xfstests.config`, ours has one more blank line after the last line. It is recorded so that a difference at the end of these files is not mistaken for a meaningful change.
- **On sync conflict:** Take either side for the whitespace; it carries no meaning. Do not take a whole side of the file to resolve it, because all three files also carry `branding` changes recorded above. Do not "fix" it in an unrelated change either: reverting it costs a diff and gains nothing.
- **Upstream status:** Not applicable; there is nothing to adopt or to audit. `git diff $BASE origin/master -- <path>` (see [README.md](README.md)) shows whether a file still differs at its end. When a sync leaves one of these files ending the same way on both sides, remove that file from this entry, and remove the entry when no file is left.

## kept-geesefs-names

- **Status:** `kept GeeseFS name`
- **Files:** kept on purpose: `go.mod`, `core/pb/fs_grpc.proto`, `core/pb/recovery.proto`, `debian/control`, `.github/workflows/release.yml`, `core/cfg/flags.go`, `core/backend_s3.go`, `core/handles.go`, `core/goofys_unix_test.go`, `core/backend_s3_test.go`, `core/backend_azblob_test.go`, `test/xfstests.config`, `test/run-xfstests.sh`, `debian/changelog`, `bench/README.md`, `bench/bench.geesefs`, `AUTHORS`, `contrib/dump-bufs.star`, `core/dir.go`, `core/goofys_test.go`, `core/ycs3ext/types.go`, and the Go files that import the module, which are a category and are not listed one by one. Found, undecided: `core/goofys_windows.go`, `debian/rules`, `bench/Dockerfile`, `bench/Dockerfile.azure`, `bench/run_bench.sh`, `test/cluster/test_read_write_ffmpeg.sh`, `doc/geesefs.png`, `doc/geesefs.svg`, `doc/geesefs.txt`, and two sentences in `bench/README.md`.
- **Symbols:** given per file under **Why**.
- **Introduced by:** #1. Its description has a section "Intentionally retained GeeseFS references", called "the list" below. The names under "found, undecided" were not introduced by anything: the rename in #1 did not reach them, and the list does not name them.
- **Origin:** upstream names, left as they were.
- **Why:** These are not divergences. Each is a place where the GeeseFS name is still in the tree, and they fall into three groups.

  **Kept on purpose (per #1).** Each item is named by the list, in the list's words, except where the entry says otherwise:
  - *The Go module path, imports, protobuf `go_package` values and Debian `XS-Go-Import-Path`:* the `module` line in `go.mod`; the import lines in the Go files that import the module's packages; `go_package` in `core/pb/fs_grpc.proto` and `core/pb/recovery.proto`; `XS-Go-Import-Path` in `debian/control`. The `Build tests` step of `.github/workflows/release.yml` belongs here although the list does not name the file: it passes `go test -c` a package import path, which has to start with the module path. The module path is spelled in import lines throughout the Go tree, so changing it would make a future sync conflict across that tree; `AGENTS.md` forbids it without an explicit decision.
  - *The `geesefs` xattr, the `geesefs-lock` backend tag, the `GEESEFS_VERSION` constant and the `GEESEFS_BINARY` test override:* the extended attribute answered by `GetXattr` in `core/handles.go`; the tag in `core/backend_s3_test.go` and `core/backend_azblob_test.go`; the constant defined in `core/cfg/flags.go` and used there, in `core/handles.go` and in `core/backend_s3.go`; the environment variable read in `core/goofys_unix_test.go`. The list gives protocol and configuration compatibility as the reason.
  - *Historical changelog entries:* upstream's own entries in `debian/changelog`.
  - *Benchmark result labels:* the column headings of the result tables in `bench/README.md`, and the result file `bench/bench.geesefs`, which carries the name in its file name.
  - *Test fixture paths:* the temporary directories, the log file and the xfstests checkout path named after `geesefs` in `test/xfstests.config`, and the same directories and path in `test/run-xfstests.sh`.
  - *Source comments:* comments inherited from upstream in `core/dir.go`, `core/goofys_test.go`, `core/ycs3ext/types.go` and `contrib/dump-bufs.star`.
  - *AUTHORS:* `AUTHORS`.

  **Found, undecided.** These carry the GeeseFS name and the list does not name them. They are leftovers that were found when this entry was written, not deliberate keeps. Nobody has decided whether each should be kept or renamed; until someone does, leave them as they are, and record the decision here when it is made.
  - `core/goofys_windows.go`: the error message `GeeseFS initialization failed` returned by `MountWin`. A user on Windows can see it at run time, which is why `branding-runtime` points here.
  - `debian/rules`: `DH_GOLANG_BUILDPKG` is set to the module path. The file overrides the build, test and install steps, and whether the variable still has any effect was not checked.
  - `bench/Dockerfile` and `bench/Dockerfile.azure`: the source directory inside the image, and the `ENTRYPOINT` under it, are spelled as the module path.
  - `bench/run_bench.sh`: the default value of `BUCKET`.
  - `bench/README.md`: two sentences of prose that name GeeseFS as the filesystem that was measured. They describe upstream's measurements, so a rename would also change what they claim.
  - `test/cluster/test_read_write_ffmpeg.sh`: the name of the upstream-owned bucket the script downloads its test video from. It is an external name; renaming it here would break the download.
  - `doc/geesefs.png`, `doc/geesefs.svg` and `doc/geesefs.txt`: the diagrams keep the name in their file names, and `doc/geesefs.svg` also in its metadata.

  **Not kept names: our own text that refers to upstream.** The search below also prints lines where AkaveFS text names GeeseFS in order to refer to it: `README.md` and `debian/copyright` (lineage and attribution, which the list also mentions), `AGENTS.md`, the `akavefs` entry in `debian/changelog` that records a sync, comments in `core/refresh_inode_cache.go`, `core/rename_cold_target_test.go` and the `race` job in `.github/workflows/test.yml`, and this folder. Those are correct as they stand and are neither kept names nor leftovers.

  To re-derive the list, run these and sort every line they print into one of the three groups. A line that fits none is a new leftover: add it to "found, undecided".

  ```sh
  # Every line that carries the name.
  git grep -n -i geesefs origin/master
  # The same without the Go import lines, which are the bulk of the output.
  git grep -n -i geesefs origin/master | grep -v -E '\.go:[0-9]+:[[:space:]]*([a-z_.]+ )?"github.com/yandex-cloud/geesefs[^"]*"$'
  # Files that carry the name in their file name.
  git ls-tree -r --name-only origin/master | grep -i geesefs
  ```
- **On sync conflict:** The names themselves are the same strings on both sides, so they cause no conflict of their own; several of these files conflict for the reasons given in other entries. The risk runs the other way: a well-meant "finish the rename" change that touches the module path or the imports would create a divergence across the Go tree. Reject it unless there is an explicit decision, and if there is one, record it here as a new `branding` entry. If a sync brings in a new GeeseFS name, classify it with the commands above.
- **Upstream status:** Not applicable to the first group, which uses upstream's names by design, and to the second, which uses them by omission. A decision on a "found, undecided" item moves it to the first group or into a `branding` entry.

## dir-handle-invalidation

- **Status:** `ours — no upstream fix`
- **Files:** `core/dir.go`, `core/dir_handle_generation_test.go`
- **Symbols:** the `generation` fields of `DirInodeData` and `DirHandle`; `checkDirPosition`; the check that follows the listing step in `DirHandle.ReadDir`; the child mutators `removeChildUnlocked`, `removeAllChildrenUnlocked` and `insertChildUnlocked`; the tests named `TestDirHandle…NoCloud` in `core/dir_handle_generation_test.go`.
- **Introduced by:** #3 (`9e37512`).
- **Origin:** ported from TigrisFS `b6eba91` and `d3e4661` in the first form of #3, then reworked in review. The spec lists what was left out of the port and why.
- **Why:** The child mutators invalidated open directory handles by writing into each handle without holding the handle's lock, which is a data race with a concurrent readdir and can lose the invalidation. Two related defects in the same code were fixed with it. See [dir-handle-invalidation.md](dir-handle-invalidation.md).
- **On sync conflict:** Follow the "Sync notes" section of [dir-handle-invalidation.md](dir-handle-invalidation.md). It is the single source for how to resolve a conflict in these functions and for the inherited functions this code depends on, which a sync can change without any conflict; the list is not repeated here.
- **Upstream status:** No upstream fix was found when this entry was written. It was checked against the same upstream commit that the sync log names for the check of the rest of the register. If upstream fixes the same defect, audit and compare as the spec's sync notes describe.

## test-timeout-watchdog

- **Status:** `ours — no upstream fix`
- **Files:** `core/goofys_common_test.go`
- **Symbols:** `setUpTestTimeout`, `GoofysTest.timeout`, `TearDownTest`.
- **Introduced by:** #18.
- **Origin:** original. The watchdog itself is inherited, from Goofys `be3a8b6`.
- **Why:** `setUpTestTimeout` starts a goroutine that panics the test binary when a test runs past its timeout, and stops it by closing the channel in `GoofysTest.timeout`. Upstream's goroutine reads that field when it starts waiting, while `TearDownTest` closes the channel and sets the field to nil, and a second `setUpTestTimeout` call in the same test (`TestBenchLs` makes one) closes it and stores a new channel, with no synchronisation on either side. That is a data race; the race detector reports it in a run of the fixture-free tests on upstream's code, and the run then exits non-zero with every test passing. It is also a leak: a goroutine that first reads the field after it was set to nil waits on a nil channel, never stops, outlives its test and panics the binary during a later one. AkaveFS creates the channel as a local, stores it in the field, and has the goroutine receive from the local, so the goroutine always holds the channel that is later closed. `TearDownTest` is unchanged, and so are the timeout, the panic message and the traceback setting.
- **On sync conflict:** Keep the local channel and the goroutine's receive from it; take upstream's other changes to `setUpTestTimeout` and to `TearDownTest`. If upstream changes how the watchdog is stopped, check that its goroutine still does not read `GoofysTest.timeout`, then run the JVM-free gate from `AGENTS.md`: a race report that names `TearDownTest` or `setUpTestTimeout` means the fix was lost.
- **Upstream status:** No upstream fix was found when this entry was written. If upstream fixes the same race, audit its fix and compare it with ours as [README.md](README.md) describes.
