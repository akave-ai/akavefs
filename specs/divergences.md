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
- **Introduced by:** #18 (`20566b8`).
- **Origin:** original. The watchdog itself is inherited, from Goofys `be3a8b6`.
- **Why:** `setUpTestTimeout` starts a goroutine that panics the test binary when a test runs past its timeout, and stops it by closing the channel in `GoofysTest.timeout`. Upstream's goroutine reads that field when it starts waiting, while `TearDownTest` closes the channel and sets the field to nil, and a second `setUpTestTimeout` call in the same test (`TestBenchLs` makes one) closes it and stores a new channel, with no synchronisation on either side. That is a data race; the race detector reports it in a run of the fixture-free tests on upstream's code, and the run then exits non-zero with every test passing. It is also a leak: a goroutine that first reads the field after it was set to nil waits on a nil channel, never stops, outlives its test and panics the binary during a later one. AkaveFS creates the channel as a local, stores it in the field, and has the goroutine receive from the local, so the goroutine always holds the channel that is later closed. `TearDownTest` is unchanged, and so are the timeout, the panic message and the traceback setting.
- **On sync conflict:** Keep the local channel and the goroutine's receive from it; take upstream's other changes to `setUpTestTimeout` and to `TearDownTest`. If upstream changes how the watchdog is stopped, check that its goroutine still does not read `GoofysTest.timeout`, then run the JVM-free gate from `AGENTS.md`: a race report that names `TearDownTest` or `setUpTestTimeout` means the fix was lost.
- **Upstream status:** No upstream fix was found when this entry was written. If upstream fixes the same race, audit its fix and compare it with ours as [README.md](README.md) describes.

## file-handles-atomic

- **Status:** `ours — no upstream fix`
- **Files:** `core/file.go`, `core/dir.go`, `core/handles.go`, `core/cluster_fs.go`, `core/file_handles_race_test.go`
- **Symbols:** the field `Inode.fileHandles`; the readers `sendUpload`, `sendUploadParts` and `patchObjectRanges` in `core/file.go`, `renameInCache` in `core/dir.go`, `DumpThis` in `core/handles.go` and `ClusterFs.tryYield` in `core/cluster_fs.go`; the two writers that set the count to one for a new file, `CreateOrOpen` in `core/dir.go` and `ClusterFs.createFile` in `core/cluster_fs.go`; the tests `TestUploadDecisionVersusReleaseNoCloud`, `TestDumpVersusReleaseNoCloud` and `TestCreateVersusEvictEntryNoCloud`, which pin the reads on the upload path, the read in `DumpThis` and the write in `CreateOrOpen`.
- **Introduced by:** #19 (`c51beef`).
- **Origin:** the three reads in `sendUpload` and `sendUploadParts` match TigrisFS `a736b74e668c`, which converts those and leaves the rest plain. The other conversions and the tests are original.
- **Why:** `FileHandle.Release` decrements `Inode.fileHandles` with an atomic operation and without `inode.mu`; its inherited comment says atomics are the discipline for this field. The upload path did not follow it: `sendUpload`, `sendUploadParts` and `patchObjectRanges` run under `inode.mu` (reached through `TryFlush`, from the `Flusher` and from `SyncFile`) and read the field plainly, so a file being closed while its inode is considered for upload is a data race, and the race detector reports it. AkaveFS reads the field with `atomic.LoadInt32` there, and at every other place that read or wrote it plainly — the readers and the two writers named under **Symbols** — so that no plain access to the field is left and that can be checked with the search below.

  This changes synchronisation only: no lock is added, removed or reordered, and the upload decision is the one it was. What the value decides is when to upload and which dirty parts or buffers go in this pass: whether a small object is flushed now, whether a multipart upload is completed now, whether `sendUploadParts` sends the part still being written and the zero-range parts, and whether `patchObjectRanges` sends its buffers. Whatever a pass skips stays dirty and goes in a later one, and `Release` wakes the flusher after its decrement. The reader sees the count either before or after each concurrent change, which are the two orders a lock would have allowed.

  Not addressed here: `renameInCache` reads the count and moves the open-file protection (`ModifiedChildren`) from the old parent directory to the new one, while `Release`, on reaching zero, takes that protection off `inode.Parent`, which it reads without a lock. The two can interleave so that the counters of the two directories end up wrong in either direction. Atomic access removes the detector's report on the count in `renameInCache`; it does not make that check-then-act sequence safe, and the unlocked `Parent` read in `Release` is still a race.

  Two more things are left as they were. Both are inherited and identical on `origin/master`:

  - `TryFlush` reads `inode.Parent` before it takes any lock, while `renameInCache` writes `fromInode.Parent` under the locks `Rename` holds. Together with the unlocked `Parent` read in `Release`, that leaves the rename path with races this change does not touch, and no test here runs a rename against `Release`.
  - In cluster mode `ClusterFs.openFile` increments the count under the owner lock only, not `inode.mu`, so the upload decision can see zero while an open is in progress.
- **On sync conflict:** Take upstream's logic, then re-apply atomic access on every line that reads or writes `Inode.fileHandles`, including any new one upstream adds. Afterwards run the search below from the repository root: on a correct tree it prints one line only, the log format string in `tryYield` that spells the field's name. The search is line-based, so also read the conflicting hunks; then run the JVM-free gate from `AGENTS.md`, where a race report that names `sendUpload`, `sendUploadParts`, `patchObjectRanges`, `DumpThis` or `CreateOrOpen` on this field means a conversion was lost.

  ```sh
  /usr/bin/grep -nE '\.fileHandles\b' core/*.go | /usr/bin/grep -vE '_test\.go:|(fs|Goofys)\.fileHandles|atomic\.(Load|Add|Store)Int32\(&[][a-zA-Z0-9_.]*\.fileHandles'
  ```
- **Upstream status:** No upstream fix was found when this entry was written. If upstream fixes the same race, audit its fix and compare it with ours as [README.md](README.md) describes; a fix that covers only the upload path leaves the other plain accesses to re-convert.

## lookup-cached-cache-state

- **Status:** `ours — no upstream fix`
- **Files:** `core/dir.go`, `core/lookup_cached_race_test.go`
- **Symbols:** `LookUpCached`; the field `Inode.CacheState`; the test `TestLookUpCachedVersusCacheStateNoCloud`.
- **Introduced by:** #22 (`c0dcfbc`).
- **Origin:** the atomic read matches TigrisFS `a736b74e`, which changes only that load. TigrisFS also moves the check under `inode.mu`, in a different commit, `daf4258a`; that restructure is left out, and `attr-time-atomic` records why. The test is original.
- **Why:** `SetCacheState` stores `Inode.CacheState` with an atomic operation, under the inode's own lock. `LookUpCached` holds only the parent's lock when it finds a child whose attributes have expired and reads the child's state, to decide whether to return the cached inode or recheck it against the backend, and it read the field plainly. A lookup of such an entry while its state changes — a small-object flush finishing is the case seen in CI — is a data race, and the race detector reports it in the full-suite step of the `race` job. AkaveFS reads the field with `atomic.LoadInt32` there. No lock is added, removed or reordered, and the decision is the one it was: the lookup sees the state from before or after the concurrent change, which are the two orders a lock would have allowed.

  This does not make `Inode.CacheState` race-free. Other plain reads of the field remain in `core/dir.go`, `core/file.go`, `core/handles.go`, `core/goofys_fuse.go` and `core/cluster_fs.go`. Those in the first four sit under the inode's own lock, the lock the writer holds, as far as reading the lock and unlock lines around them shows; that is not a proof over every path. Some of those in `core/cluster_fs.go` show no `inode.mu` around them; cluster mode was not audited. The read in `LookUpCached` is the only one the race detector reported in the CI runs that led to this change.

  Two more reads in the same function were looked at. The same condition calls `inode.isDir()`, which reads the child's `dir` field under the parent's lock only. That is not a race: `dir` is assigned in `ToDir` alone, every call of `ToDir` in the non-test code acts on an inode that `NewInode` has just returned and that no directory lists yet, and an inode becomes reachable by a lookup when it is inserted under its parent's lock, the lock `LookUpCached` holds. `LookUpCached` also reads the child's `AttrTime` under the parent's lock only; that read was a race, the race detector has since reported it in CI, and `attr-time-atomic` covers it.
- **On sync conflict:** Take upstream's logic in `LookUpCached`, then re-apply the atomic load on the read of the child's `CacheState`. Afterwards run the JVM-free gate from `AGENTS.md`: a race report that names `LookUpCached` means the conversion was lost.
- **Upstream status:** No upstream fix was found when this entry was written: upstream's `LookUpCached` has the plain read. If upstream fixes the same race, audit its fix and compare it with ours as [README.md](README.md) describes.

## core-suite-test-races

- **Status:** `ours — no upstream fix`
- **Files:** `core/goofys_test.go`, `core/goofys_unix_test.go`, `core/goofys_fs_test.go`, `core/switch_backend_test.go`
- **Symbols:** the helper `getRoot` and the test `TestRenamePreserveMetadata` in `core/goofys_test.go`; the helper `testReadMyOwnWriteFuse` in `core/goofys_fs_test.go`, which two tests call; the test `TestConcurrentRefDeref` in `core/goofys_unix_test.go`. For the backend swap: the helper `setS3` (and `disableS3`, which calls it) and the tests `TestReadDirCacheLookup`, `TestXAttrGetCached`, `TestReadDirSlurpSubtree`, `TestReadDirCached`, `TestSlurpFileAndDir`, `TestWriteUnlinkFlush`, `TestSlurpDisappear`, `TestWriteLargeTruncateMem20M` and `TestMultiStreamMem100M` in `core/goofys_test.go`; the type `switchBackend`, the helpers `useSwitchBackend` and `switchBackendOrPanic`, and the test `TestSetS3VersusCloudNoCloud` in `core/switch_backend_test.go` (new file).
- **Introduced by:** #22 (`c0dcfbc`); the backend swap by #29 (`34a7ac4`).
- **Origin:** the same four test races are fixed in TigrisFS `a736b74e`. Its production changes that go with them (`getCloud`/`setCloud`, an atomic `MaxFlushers`) are left out; the changes here are in test code only. `getCloud`/`setCloud` were audited again for the backend swap and again not taken: they are not a small accessor. That commit removes the per-directory backend field, keeps one atomic pointer on the filesystem, rewrites `Inode.cloud()` around it and disables nested mounts, which our tests exercise; and its `setS3` still sleeps and still reads `fs.inodes` without the lock. The switchable wrapper is original.
- **Why:** These inherited tests and helpers create data races of their own, which the race detector reports in the full-suite step of the `race` job. None is a defect in production code, and each hides whatever that step would otherwise show.

  - `getRoot` read the map `fs.inodes` without `fs.mu`, while the filesystem inserts inodes into it under that lock; some tests call the helper from several goroutines. Beyond the report, an unlocked map read against a write can abort the test binary. It now takes the read lock around the map read and asserts after releasing it.
  - `TestRenamePreserveMetadata` paused flushing by writing `flags.MaxFlushers`, which the `Flusher` reads without a lock; production code does not write flags once the filesystem runs. It now pauses and resumes the way `TestListBeforeFlushRename` in the same file does: by adding the limit to the count of active flushers and taking it off again, then waking the flusher. Both places that compare the count with the limit decide the same way as with a limit of zero.
  - `TestConcurrentRefDeref` shared one lookup op between a goroutine that looks up and one that forgets, so the id to forget was read while the lookup wrote it. The kernel sends a separate op for each request. The id is now read before the goroutines start; until the forget has run, the lookup can only write that same id.
  - `testReadMyOwnWriteFuse` wrapped the root's backend in a `TestBackend` and stored it into the root's `dir.cloud` under a live mount. `Inode.cloud()` reads that field without a lock, and production code writes it only when the filesystem is created, when a backend is mounted and in `ResetForUnmount`, so nothing the test could hold would order its write. The wrapper only served an error injection that upstream `8195960` removed, and nothing read it afterwards. It was not transparent: its `Delegate()` returns the wrapper itself, so `OpenDir` no longer recognised an S3 backend while it was installed. The test opens no directory after that point — it opens the file, reads it and unmounts — so removing the wrapper leaves what the test asserts unchanged.
  - `setS3` replaced the root's backend by assigning the root's `dir.cloud` after a one-second sleep, and read `fs.inodes` without `fs.mu` to find the root. The race detector reported the assignment in the full-suite step, in `TestWriteUnlinkFlush`, against the read in `Inode.cloud()` that the `Flusher` reaches through `TryFlush` and `SendMkDir`. The read had finished long before the write: what was missing was an ordering between the two goroutines, which a sleep does not give. Two more tests, `TestWriteLargeTruncateMem20M` and `TestMultiStreamMem100M`, made the same assignment to install a `TestBackend` around the root's backend.

    A test that replaces the root's backend now starts with `useSwitchBackend`. It shuts down the filesystem `SetUpTest` created and creates another — same bucket, same root mount prefix, same flags, backend built the same way — whose root backend is a `switchBackend`: a `StorageBackend` that forwards every method to a delegate held behind an atomic pointer. The root's `dir.cloud` is then written once, in `newGoofys`, before the `Flusher` is started, and `setS3` and the two wrapping tests replace the delegate. `setS3` keeps its sleep and panics if the wrapper is not installed; it does not fall back to the assignment. The two wrapping tests build their `TestBackend` around the delegate they take from the wrapper before the swap: around the root's `dir.cloud` it would wrap the wrapper, and its hooks would call themselves without end.

    Compared with the assignment, one thing stays the same and two differ. `OpenDir` asks the backend's `Delegate()` whether it is S3; the wrapper forwards the question to its delegate, so the answer is the one it was. `Inode.cloud()` recognises a disabled backend by its type and then leaves the mount prefix out of the key; behind the wrapper it no longer sees that type, so the root's mount prefix stays in. None of these tests gives the root a mount prefix, so the keys are the same. And an operation that makes several backend calls sends those after a swap to the new delegate, where it used to keep the backend it had read; none of these tests has dirty work spanning a swap.

    `TestSetS3VersusCloudNoCloud` is fixture-free. It swaps the delegate, directly and through `setS3`, while a goroutine calls `Inode.cloud()` on the root; the race detector is its oracle. Run with the wrapper's `swap` changed to write the root's `dir.cloud` as well, or with the old body of `setS3`, it produces a race report with `Inode.cloud()` on the reading side. The nine tests themselves need the storage emulator.

    Production code does not have this race today, but it is latent there: `Goofys.MountAll`, `Mount` and `Unmount` write the `dir.cloud` of a mount point under that inode's lock or its parent's, which does not order them against `Inode.cloud()` called on a descendant, and none of the three has a caller outside tests.

  Not addressed here. The suite is not free of unsynchronised writes by tests after these fixes — only of the ones the race detector has reported. Tests still write, with the filesystem running and without a lock that the reader holds:

  - `dir.cloud`: the root's is set to `nil` and restored in `TestMountsList`; a mount point's is written by `mount` and `ResetForUnmount` when tests call `MountAll`, `Mount` or `Unmount`; and `TestVFS`, which is skipped, assigns one;
  - the root's `dir.mountPrefix`, assigned at the start of several tests and read by `Inode.cloud()`, the same reader as in the `setS3` race;
  - the root's `dir.seqOpenDirScore`;
  - fields of the flags the filesystem was created with: `MaxFlushers` in `TestWriteReplicatorThrottle`, `StatCacheTTL` in many tests across `core/goofys_test.go`, `core/goofys_fs_test.go` and `core/goofys_unix_test.go`, and `Cheap`, `ExplicitDir`, `UseContentType`, `EmulateHardlinks`, `ReadRetryAttempts`, `ReadAheadLargeKB` and `MountPoint`;
  - the configuration of a backend in use: its storage class, its ACL and its credentials.
- **On sync conflict:** Take upstream's changes to these tests, then re-check each of the four hunks: the read lock in `getRoot`, the pause through the count of active flushers in `TestRenamePreserveMetadata`, the id read before the goroutines in `TestConcurrentRefDeref`, and the absence of the backend swap in `testReadMyOwnWriteFuse`. If upstream restores an error injection in `testReadMyOwnWriteFuse`, it needs a synchronised way to swap the backend; do not restore the bare assignment to `dir.cloud`. The same holds for `setS3`: keep its body going through `switchBackendOrPanic` and `swap`, and keep `useSwitchBackend` at the start of each test listed under **Symbols** — as its first statement, or right after the test's skip. A test upstream adds that calls `setS3` or `disableS3` needs that line too, and panics with "call useSwitchBackend first" until it has it. In the two wrapping tests keep the `TestBackend` built around the delegate taken from the wrapper, not around the root's `dir.cloud`.
- **Upstream status:** No upstream fix was found when this entry was written. `core/goofys_test.go` and `core/goofys_fs_test.go` were identical to upstream before this change. If upstream fixes any of these races, audit its fix and compare it with ours as [README.md](README.md) describes.

## cluster-readdir-double-unlock

- **Status:** `ours — no upstream fix`
- **Files:** `core/cluster_fs.go`, `core/cluster_readdir_error_test.go`
- **Symbols:** `ClusterFs.readDir`; the test `TestClusterReadDirListingErrorNoCloud`.
- **Introduced by:** #25 (`78689cf`).
- **Origin:** original.
- **Why:** `ClusterFs.readDir` takes the directory handle's lock and releases it with a deferred unlock. Upstream's read loop also unlocks it explicitly when `DirHandle.ReadDir` returns an error, so on that return the deferred unlock runs on a mutex that is already unlocked. Go treats that as a fatal error, which ends the process and cannot be recovered: in cluster mode a listing that fails part-way through a directory ends the process of the node serving it, where both callers — `ClusterFsFuse.ReadDir` and `ClusterFsGrpc.ReadDir` — expect an error back. The function's other error return, after `loadChildren`, has no explicit unlock and was never affected. AkaveFS removes the explicit unlock, so every return path releases the lock once, through the defer. Nothing else in the function changes: no lock is added or reordered. On that one path the lock is now still held across the call to `mapAwsError`, which takes none of the filesystem's locks.

  The crash was reproduced by a test, not only read from the code. `TestClusterReadDirListingErrorNoCloud` is fixture-free: `readDir` uses nothing of `ClusterFs` but its `Goofys`, so the test needs no peers or connections. Its backend returns a truncated first page and fails the next one, which makes `loadChildren` succeed and the loop fail. On upstream's code the test binary dies with `fatal error: sync: unlock of unlocked mutex` and a stack through `ClusterFs.readDir`; with the fix the test passes. What was not run is cluster mode itself: the test calls `readDir` directly, so the FUSE and gRPC callers were read, not exercised.
- **On sync conflict:** Keep the single deferred unlock. If upstream reworks the function, make sure no return path unlocks `dh.mu` twice, then run the JVM-free gate from `AGENTS.md`: a run that dies with `unlock of unlocked mutex` in `ClusterFs.readDir` means the explicit unlock is back. A failed assert is not how this regression shows; the whole run ends.
- **Upstream status:** No upstream fix was found when this entry was written: upstream's `ClusterFs.readDir` still has the explicit unlock in the loop's error branch. If upstream fixes it, audit its fix and compare it with ours as [README.md](README.md) describes.

## attr-time-atomic

- **Status:** `ours — no upstream fix`
- **Files:** `core/attr_time.go`, `core/handles.go`, `core/dir.go`, `core/lookup_cached_race_test.go`, `core/dir_handle_generation_test.go`
- **Symbols:** the type `atomicTime` and its methods `Load`, `Store`, `Before` and `Unix` (new, in the new file); the field `Inode.AttrTime`, its comment, `NewInode` and `SetAttrTime` in `core/handles.go`; the read in `LookUpCached` in `core/dir.go`; the tests `TestLookUpCachedVersusAttrTimeNoCloud` and `TestAtomicTimeNoCloud`; the direct stores to the field in `TestLookUpCachedVersusCacheStateNoCloud` and `TestDirHandleSealKeepsLockOrderNoCloud`.
- **Introduced by:** #26 (`a6c5476`).
- **Origin:** original. TigrisFS `daf4258a` was audited and not taken; **Why** says what it does and why not.
- **Why:** `SetAttrTime`, the only writer of `Inode.AttrTime` after `NewInode`, is annotated `LOCKS_REQUIRED(inode.mu)`. `LookUpCached` reads the field of a child while holding the parent's lock only, to decide whether the cached entry is still fresh. The two locks do not order the read against the write, so a lookup of an entry whose attributes are being set is a data race; the race detector reports it in the full-suite step of the `race` job, with `LookUpCached` on one side and `SetAttrTime` on the other. Upstream's comment on the field called such reads acceptable in practice, on the ground that a `time.Time` taken from the clock compares on one word. That does not hold for two values the code stores in this field, the zero time (to invalidate) and `TIME_MAX` (never expire): neither carries a monotonic reading, so a read that mixes the words of two writes can yield a time that was never stored. Read from the Go `time` source and not executed: mixing a clock time with the zero time can make `expired` answer "fresh" for an entry that was just invalidated, which neither value alone would; the lookup then returns the cached entry to the kernel for another full TTL. It is not a crash and it does not touch the write path.

  AkaveFS makes the field an `atomicTime`, a small type in a file of its own that keeps the value behind an atomic pointer. Every read returns a value that some store was given, or the zero time when nothing was stored; a reader can still see a value that is about to be replaced, which the readers already accepted. The change to inherited code is the field's type and comment, the store in `NewInode` (the type cannot be set in the struct literal), the store in `SetAttrTime`, and the load in `LookUpCached`. The other readers are not edited: `SetFromBlobItem` and `DumpThis` in `core/handles.go`, and `handleListResult`, `removeExpired` and `insertSubTree` in `core/dir.go`, call `Before` or `Unix` on the field, which the type provides, so each of them became an atomic read without a change to its line. `Load` checks for a field that was never stored because `Inode` literals that bypass `NewInode` exist — `Inode.LookUp` makes one, and so do tests — and read as the zero time before this change. No lock is added, removed or reordered, and the lock annotations are unchanged: `SetAttrTime` still requires `inode.mu`, which protects `ExpireTime`. Each store allocates the value it points to.

  Two other shapes were considered and rejected:

  - *Take the child's lock inside the parent's in `LookUpCached`.* It cannot be proven deadlock-free. `Rename` locks its two directories in the order of their inode Ids, not of their ancestry, and `renameRecursive`, which `Rename` uses for directories, creates the destination directory and then swaps the two inodes' Ids, so the directory at the destination carries the old, lower Id and can have a lower Id than its parent. A rename between such a pair locks the child directory and waits for the parent, while a lookup that nested would hold the parent and wait for the child.
  - *TigrisFS `daf4258a`.* It releases the parent's lock in `LookUpCached` first and then takes the child's lock around the checks, which does not nest. But TigrisFS's `handleListResult` still calls `SetAttrTime` holding the parent's lock only, as ours does, so taking that shape would move our read out from under the lock that orders it against the listing paths today, and trade one race for another.

  Not addressed here, and this list is not exhaustive. All of it is inherited:

  - The lock order of `Rename` described above is already a hazard for the places that do take a child's lock under its parent's: `insertSubTree` and `removeExpired`, and our `refreshCurrentChild`.
  - `handleListResult` and `insertSubTree` call `SetAttrTime` on a child under the parent's lock only, against its annotation. For `AttrTime` that is now harmless; `SetAttrTime` also writes `ExpireTime` through `SetExpireTime`, and that field is still plain.
  - `findChildMaxTime` reads the times in the children's `Attributes` under the parent's lock only, and `GoofysFuse.LookUpInode` calls `InflateAttributes` without `inode.mu`.
  - Whether `inode.mu` is held at every caller of `resetCache` and `updateFromFlush`, which call `SetAttrTime`, was not traced.

  The race was reproduced by a test, not only read from CI. `TestLookUpCachedVersusAttrTimeNoCloud` is fixture-free and has the race detector as its oracle; its first phase fails on a plain field with the report described above and passes with the fix. Its second phase writes under the parent's lock, as the listing paths do, and cannot fail on the code as it stands: it is there to fail if the read in `LookUpCached` is later moved out from under the parent's lock without synchronising the field. `TestAtomicTimeNoCloud` pins the zero value: the rest of the fixture-free tests pass with a `Load` that does not check for a field never stored, and this one does not.
- **On sync conflict:** Keep `core/attr_time.go` and the field's type. Take upstream's logic, then re-apply the two stores (`NewInode`, `SetAttrTime`) and the load in `LookUpCached`. A new upstream line that assigns the field or passes it as a `time.Time` does not compile until it uses `Store` or `Load`, so the compiler finds those; a new line that only calls `Before` or `Unix` on it needs nothing. If upstream uses a `time.Time` method on the field that `atomicTime` lacks, call it on `Load()` at that line and leave the type as it is. Do not resolve a conflict in `LookUpCached` by taking a lock on the child inside the parent's. Afterwards run the JVM-free gate from `AGENTS.md`: a race report that pairs `LookUpCached` with `SetAttrTime` means the conversion was lost.
- **Upstream status:** No upstream fix was found when this entry was written: upstream's field is a plain `time.Time` and `LookUpCached` reads it plainly. If upstream fixes the same race, audit its fix and compare it with ours as [README.md](README.md) describes; a fix that takes the child's lock under the parent's has to answer the `Rename` lock order first.

## stale-listing-page

- **Status:** `ours — no upstream fix`
- **Files:** `core/dir.go`, `core/dir_stale_listing_test.go`
- **Symbols:** `listObjectsFlat`; the test `TestDirHandleDuplicatePageKeepsListingNoCloud`.
- **Introduced by:** PR #TBD (`fix/stale-listing-page`).
- **Origin:** original.
- **Why:** `listObjectsFlat` reads the directory's `listMarker`, releases `dh.mu` and the directory's lock around the backend request, and upstream applies the answer whatever happened to the directory meanwhile. Nothing marks a listing as in flight, so two handles that read the same unlisted directory both request the page that follows the same marker. The first answer is applied: the children grow, `lastFromCloud` names the last entry of the page and `listMarker` moves to it. `DirHandle.ReadDir` serves the cached children and, on handing out the entry `lastFromCloud` names, clears it, which is how it asks for the next page on the following call. When the second, duplicate answer is applied after that, `handleListResult` finds `lastFromCloud` cleared and sets it to the end of the same page again. The handle that was served that entry already stands at the end of the cached children; its next `ReadDir` skips the listing loop because `lastFromCloud` is set, and returns end-of-directory with the rest of the directory unlisted. The reader gets a listing cut on a page boundary and no error, while the other handle gets the whole directory. An answer that is merely late, arriving after another handle has moved the marker further, has the same effect.

  AkaveFS compares, under the directory's lock and before `handleListResult`, the marker the request was sent with against the directory's `listMarker`, and drops the page when they differ. A dropped page changes nothing in the directory, so the caller is nearly where a handle that had not sent the request would be: the loop in `loadListing`, and the one in `DirHandle.loadChildren` in `core/cluster_fs.go`, test `lastFromCloud` and `listDone` again and, when both still ask for a listing, request the page after the current marker. `listMarker` is written in two places only, where `listObjectsFlat` advances it after applying a page and in `sealDir`, so a marker that differs means that another caller applied a page or sealed the directory during the request; a caller is not made to list again unless another one made progress. Nothing caps how often one handle can lose that way; each loss costs one more listing request. The entry of the in-flight listings is completed on the new return as on every other. No lock is added, removed or reordered: the new return releases the directory's lock that the function has just taken.

  The inherited test `TestListParallelExpireNoCloud` reads one directory through two handles in parallel and was failing intermittently because of this defect; it is unchanged. `TestDirHandleDuplicatePageKeepsListingNoCloud` is fixture-free and forces the order described above by parking both requests in the backend: on upstream's `listObjectsFlat` the first handle's second read returns nothing, and with the check both handles list the whole directory.

  Not addressed here: After a dropped page the caller's loop can begin a new pass from the empty marker without going through the refresh-start step of `loadListing`, so the refresh start time and the listing gaps keep their previous values for that pass; `removeExpired` then removes less, and an entry deleted remotely can survive one more pass. Also, the check compares marker values, so it does not recognise an answer whose marker is current again after the directory was sealed and the marker went back to empty. Such an answer is applied, as it was before this change.
- **On sync conflict:** Keep the marker check between the lock of the directory and the call of `handleListResult` in `listObjectsFlat`, with its completion of the in-flight listing and its unlock, and take upstream's other changes to the function. If upstream changes how the request's start is built, compare against what the request was sent with. Afterwards run the JVM-free gate from `AGENTS.md`: a failure of `TestDirHandleDuplicatePageKeepsListingNoCloud` means the check was lost.
- **Upstream status:** No upstream fix was found when this entry was written: upstream's `listObjectsFlat` applies every answer. If upstream fixes the same defect, audit its fix and compare it with ours as [README.md](README.md) describes.
