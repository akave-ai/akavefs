# Working Agreement for AkaveFS

These rules apply equally to all agents used on this repo, including Codex, Claude, and Gemini.

Several rules below name a skill. Load it if it is available in whatever configuration you run
under; this repository does not dictate where a skill is loaded from. Where a named skill is
unavailable, the rule stated in its bullet still applies in full — the skill is the long form of
a rule, never its only source.

## What this repo is

AkaveFS is a FUSE filesystem over S3-compatible object storage, forked from
[GeeseFS](https://github.com/yandex-cloud/geesefs) (itself a Goofys fork). Most of the code is
inherited, and **we keep merging upstream GeeseFS changes**. That shapes every rule below: the
smaller and more local our diff against upstream, the cheaper every future sync is.

## How we work

- **Every session runs under the `manager` skill.** Work moves through plan → develop → analyze → integrate → review → commit → push, each stage dispatched to its own agent, and the session lead manages rather than implements. It governs *how* work is done, not *whether* — questions are answered directly, and trivial changes (roughly five lines with no architectural impact) are made directly.
- Follow the `guardrails` skill on all substantive work — verify before claiming done, confirm before irreversible actions, check callers before deleting seemingly-dead code, surface uncertainty and tradeoffs, report failures faithfully, and stay in scope.
- Follow the `security` skill on any change that touches data, the write path, resource lifecycle or destructive operations — and on every claim you make about your own work. Prove what you assert (paste the command and its real output, or write `UNVERIFIED:` and why), and do not break the durability rules. In this repo the write path (buffers, flushing, multipart uploads, partial writes, retries) is top severity by default: corruption there lands in the user's bucket and verifies against itself.
- Follow the `comments` skill whenever you write or edit code. Comment the decision rather than the mechanism, and never delete or rewrite a `TODO`, `FIXME`, `HACK`, `NOTE` or `XXX` marker without approval.
- Follow explicit user instructions as requirements, not suggestions. Mirror them back if there is any doubt before implementing.
- When unsure or when a change could be risky, ask instead of assuming.

## Upstream GeeseFS

- **Keep changes to inherited code minimal and local.** Fix the bug; do not refactor, reformat, rename, reorder or split inherited files in the same change. A drive-by cleanup in `core/dir.go` becomes a merge conflict on every future sync.
- Put Akave-specific functionality in new files where practical, rather than weaving it through inherited ones.
- Do not change the Go module path (`github.com/yandex-cloud/geesefs`) or mass-edit imports without an explicit decision — it touches every file and conflicts with every sync.
- When a fix is ported from upstream or a sibling fork (GeeseFS, TigrisFS, Goofys), cite the source commit in the PR description, and say what was left out of the port.
- **Syncing upstream:** add the remote once (`git remote add upstream https://github.com/yandex-cloud/geesefs`), then `git fetch upstream` and **merge** `upstream/master` into a `sync/geesefs-<yyyymmdd>` branch — merge, never rebase or squash, so the next sync has a merge base. Resolve conflicts in favour of keeping AkaveFS's deliberate divergences (CLI name and FUSE subtype `akavefs` in `core/cfg/flags.go`, `core/goofys_fuse.go`, `core/cluster_fs_fuse.go`; binary names in `.github/workflows/release.yml`; branding in `README.md`), run the gates below, and open a PR that lists the conflicts and how each was resolved.

## Code

- Match the style of the surrounding code. New files follow standard Go conventions (`gofmt`, short clear names, doc comments on exported symbols).
- **Keep the lock annotations true.** Functions document their locking with `// LOCKS_REQUIRED(x.mu)`, `// LOCKS_EXCLUDED(x.mu)` and `// ACQUIRES_LOCK(x.mu)`. Add them to new functions that take or assume a lock, update them when locking changes, and respect the existing order: parent inode before child inode, inode before `fs.mu`, and `dh.mu` before `dh.inode.mu`. A concurrency hazard is dismissed by proving it impossible, never by arguing it is unlikely.
- Be especially careful with file handles, network connections and buffers. Verify ownership and lifecycle, and make sure a change does not introduce FD leaks or share a buffer across requests.
- Before adding a new helper, look for an existing one (`core/utils.go` and the package you are in) and reuse it when it fits.
- When changing existing logic inside a function, recheck its call sites across the project for panic/crash risk and logical regressions before considering the work complete.
- Where backend behaviour depends on S3 API semantics (pagination, continuation tokens, multipart limits, error codes), check it against the AWS S3 API reference and cite the page in the PR or a comment. Backends other than AWS vary; do not assume a quirk is universal.

## Tests

- Bug fixes come with a regression test where practical — one that fails before the fix and passes after. Say which of those you actually observed.
- Tests use gocheck (`gopkg.in/check.v1`). Hand-written fakes such as `TestBackend` in `core/backend_test.go` are the existing pattern; use them rather than introducing a mock generator.
- Prefer fixture-free tests (like the `DirTest` suite in `core/dir_test.go`) where the code allows: they run without s3proxy, so they run anywhere and under the race detector.
- Concurrency changes should be exercised under `-race` (requires `CGO_ENABLED=1`).

## Gates

These mirror `.github/workflows/test.yml`. Use them, not commands you compose yourself.

- Lint: `test -z "$(gofmt -l .)"` and `go vet ./...`
- Build: `make build && ./akavefs --help`
- Tests: `make run-test` and `make run-xfstests` — both need a JVM (s3proxy) and FUSE. Where those are unavailable, say so, and use the PR's GitHub Actions logs as the execution evidence.
- Locally runnable without a JVM: `cd core && CGO_ENABLED=1 go test -race -count=1 -check.f 'DirTest' .` (`-check.f` only works from inside `core/`).

## Commits

- Commit messages must use Conventional Commits format: `<type>(<scope>): <subject>`.
- Allowed commit types are: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`, `build`, `ci`, `chore`.
- Keep commit scopes short, lowercase, and component-oriented.
- Keep commit subjects imperative, concise, lowercase after the colon, and without a trailing period.
- Add a short body only when it improves clarity; use brief bullet points for the main changes.
- Upstream sync merges are the exception: keep the merge commit, titled `chore(upstream): merge geesefs <short-sha>`.
- Do not mention AI, assistants or autogenerated text in commit messages, and do not invent issue references.
- End every commit message with an `Authored-by:` trailer naming the committing identity, read at commit time and never hardcoded. Read it with `git var GIT_AUTHOR_IDENT`, which prints `Name <email> <timestamp> <tz>`; the trailer takes everything before the timestamp:  
  `Authored-by: Jane Doe <jane@example.com>`  
  Do not read it from `git config user.name` / `user.email`: where the identity comes from the environment instead, both exit 1 while the commit author stays correct, and the trailer renders `Authored-by:  <>`. If `git var GIT_AUTHOR_IDENT` fails or its output does not parse, stop and ask rather than committing. This replaces the `Co-Authored-By` trailer some tools add by default.
