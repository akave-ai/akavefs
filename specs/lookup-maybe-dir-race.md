# LookUpInodeMaybeDir result race

Register entry: `lookup-maybe-dir-race` in [divergences.md](divergences.md).

## Problem

`LookUpInodeMaybeDir` finds out whether a name is a file, an explicit directory object or
an implicit directory (a key prefix). It starts one probe goroutine per enabled strategy:
a `HeadBlob` on the key, a `HeadBlob` on the key followed by a slash, and a prefix
`ListBlobs`. Which probes run depends on the backend's `DirBlob` capability and on the
`Cheap`, `NoDirObject` and `ExplicitDir` flags.

In the inherited code each goroutine assigned its result directly to variables of the
calling function and then sent a plain token on a channel. Receiving one token only
synchronises with the goroutine that sent it. The other goroutines may still be writing
their own variables while the caller reads them, which is a data race.

The description of #6 reports that, before the fix, this race was the dominant source of
race-detector reports in the fixture-free tests, which made the detector's output hard to
use for anything else.

## Upstream behaviour

Probes write the shared variables and signal with a token, as described above.

Whether the probes overlap depends on the configuration, and upstream and AkaveFS start
them the same way:

- With the `DirBlob` capability, the object probe is the only one that starts, and its
  token is received before the function goes on.
- With `Cheap` on, the probes run one at a time: each token is received before the next
  probe starts, and the next one starts only if the previous probe reported "not found".
- Otherwise every enabled probe is started before any token is received, so they run
  concurrently.

The race exists in the third case. There, when a key exists both as an object and as a
directory prefix, the answer depends on timing and on an unsynchronised read, so it is
undefined. In the first two cases no two probes run at the same time, and the answer for
such a key is the object.

## AkaveFS behaviour

Each probe sends a `lookupResult` value on the channel, carrying its kind, its output and
its error. The `receiveResult` closure receives one value and assigns it to the matching
variables. It is called from the function's own goroutine, so that goroutine is the only
writer of those variables.

The result kinds start at one, not zero, so a zero-valued `lookupResult` matches no case
and can never be routed into the object result.

Behaviour is unchanged, with one documented exception, which applies when the probes
run concurrently (no `DirBlob` capability and `Cheap` off). Each result is applied only
when its own probe reports, so the loop returns the first usable answer in arrival order.
The priority of object over directory object over prefix is a tiebreak among results
that have already arrived, not a global priority. For a key that exists both as an object
and as a directory prefix, the answer in that configuration therefore depends on which
probe finishes first. This defines what the unsynchronised read left undefined; the
description of #6 reports that measurements against the inherited code showed the same
set of outcomes in practice. A comment at the receive loop in `core/dir.go` records the
arrival-order rule.

In the other two configurations there is no tie to break, and the answer for such a key
is the object, as it is upstream:

- With `DirBlob`, the object probe is the only one started. Its result is the answer: the
  object if it exists, otherwise its error.
- With `Cheap` on, the probes run one at a time in the fixed order object, directory
  object, prefix. The next probe starts only when the previous one reported "not found",
  so an object that exists is returned before the prefix probe is started.

## Invariants and locking

- `receiveResult` is the only writer of the outer result and error variables. A probe
  goroutine must never assign them.
- Inside the probe goroutines the local variables are deliberately named differently from
  the outer ones. The outer names stay in scope there, and a shadowing name would let a
  later edit silently restore the cross-goroutine write.
- Every probe that starts sends exactly one result.
- There is at most one receive per probe started, never more, so a receive cannot wait
  for a result that no probe will send. There can be fewer. When the probes run
  concurrently (no `DirBlob` capability and `Cheap` off), the final loop receives one
  result per pass and returns on the first usable answer, leaving the results of the
  slower probes unreceived. With `DirBlob` or `Cheap`, each probe's result is received
  right after that probe is started, before the final loop, which then receives nothing.
- The channel is buffered for every probe that can start. This is what makes the early
  return safe: a probe whose result is never received still completes its send and
  exits, instead of blocking forever.
- No lock is involved. The fix adds none and changes no lock annotation.

## Tests that pin it

All in `core/dir_lookup_test.go`, in the fixture-free `DirTest` suite:

- `TestLookUpInodeMaybeDirConcurrentResults`
- `TestLookUpInodeMaybeDirObjectResult`
- `TestLookUpInodeMaybeDirDirObjectResult`

There is one test per result kind. Each releases all probes from a barrier at the same
moment and repeats the lookup many times, and each makes exactly one kind of result the
only usable answer, so the expected key holds whichever probe reports first. They cover
the concurrent configuration only: the helpers assert that the backend reports no
`DirBlob` capability and that `Cheap`, `NoDirObject` and `ExplicitDir` are off.

The tests are built for a fixed number of probes, the constant `maybeDirLookupProbes`.
Two separate mechanisms catch the production code drifting from it, and they are not
equally strong:

- **Too few probes.** `maybeDirLookupRunIteration` waits at the barrier until that many
  probes have started, bounded by `maybeDirLookupProbeTimeout`. If fewer start, the
  iteration fails when the timeout expires, instead of hanging.
- **One probe too many.** The barrier cannot see this, so after the lookup returns
  `maybeDirLookupAssertNoExtraProbe` makes one non-blocking check for a further probe
  start. It is an aggregate check, not a per-iteration one: the lookup can return while
  an extra probe goroutine has not yet reached the backend, so a single iteration can
  miss it. The repetition is what catches it, and the helper's own comment says no single
  iteration is guaranteed to.

The defect that was fixed is visible only under the race detector: without `-race` these
tests pass on the unfixed code, so they guard the fix only when run with it. They are not
blind without it, though. They also fail, with or without `-race`, when `receiveResult` drops a
result — the lookup then returns "not found" where the test expects a key — and on a
change in the number of probes, through the two mechanisms above. Run them with the
JVM-free command in the Gates section of `AGENTS.md`; in CI they are exercised by the
`race` job (see `ci-race-job` in the register).

All helpers, types and constants in the test file start with `maybeDirLookup` to stay
clear of names upstream may add to the same package. The test methods themselves are
named `TestLookUpInodeMaybeDir…`.

## Upstream status

Upstream has no fix for this; the sync log in [upstream-syncs.md](upstream-syncs.md)
records when that was last checked.

## Sync notes

- This change sits inside the inherited `core/dir.go` and rewrites a block of
  `LookUpInodeMaybeDir`, so a sync that touches that function is likely to conflict.
  The other changes to inherited Go logic are a few added lines each (the `help` return in
  `main.go`, the `refreshCurrentChild` call in `core/goofys.go`); this is the largest of
  them. Resolve by hand, never by taking one side of the file.
- Keep the typed channel: every probe sends a `lookupResult`, and `receiveResult` is the
  only code that assigns the outer result variables. Never accept a goroutine that
  assigns them directly.
- If upstream adds, removes or reorders a probe, give the new probe its own result kind,
  have it send a `lookupResult`, route it in `receiveResult`, and keep the channel's
  buffer equal to the number of probes that can start. Then update
  `maybeDirLookupProbes` and the test cases to match. Do not rely on the tests to report
  a mismatch cleanly: a removed probe fails them at the barrier timeout, and an added one
  is caught only by the aggregate check described under "Tests that pin it".
- If upstream fixes the race itself, follow the policy in [README.md](README.md): audit
  their fix and compare it with ours. The points to compare are that a single goroutine
  writes the results, that a probe can never block or leak when the function returns
  early, and what happens on the object-and-prefix tie. Record the outcome in the register
  and the sync log.
- After any sync that touches this function, run the fixture-free tests under `-race`.
