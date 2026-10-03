# Stale inode refresh

Register entry: `stale-inode-refresh` in [divergences.md](divergences.md).

## Problem

A user can ask AkaveFS to re-check one file or directory against the object store by
setting the special extended attribute `.invalidate` on it (`setfattr -n .invalidate`).
The handler passes the inode the kernel named to `RefreshInodeCache`.

That inode can be stale. `removeChildUnlocked` drops a child from its parent's list of
children without marking it dead. If the kernel still references the dropped inode, it
stays in the filesystem's inode table, and a later listing inserts a different inode
object under the same name. The kernel then still names the old object, while the parent
directory holds the new one.

In that situation the refresh did nothing. When the object was gone from the store, the
code tried to remove the stale inode from the parent, and `removeChild` only removes an
inode that is the one currently registered under its name. The stale inode is not, so the
call returned without doing anything, and the deleted object stayed visible in the
directory cache.

## Upstream behaviour

`RefreshInodeCache` calls `recheckInode(inode, name)` with the inode the kernel passed in.
`recheckInode` looks the name up, and on a lookup error removes that inode through
`removeChild`. Two properties follow:

- If the inode is stale, the removal is a no-op, as described above.
- If the inode is current, it is removed on any lookup error, not only on "not found".

## AkaveFS behaviour

`RefreshInodeCache` calls `refreshCurrentChild(inode, name)` instead. It first finds the
child currently registered under the name.

If that child is the inode the kernel passed in, or there is no child, nothing is
different: it calls the inherited `recheckInode`, including its removal on any lookup
error. That part is left to upstream on purpose.

If the registered child is a different object — the stale case — it does this:

| Situation | Child | Kernel notification | Returned |
|---|---|---|---|
| Object is gone, child is clean | removed | delete, naming the stale inode | success |
| Object still exists | kept | invalidate entry | success |
| Lookup fails for another reason (throttling, server error, network) | kept | invalidate entry | the error |
| Child already has unflushed changes | kept, and no request is sent | invalidate entry | success |
| Lookup reports the object gone, but the child gained unflushed changes during the lookup | kept | invalidate entry | success |
| Lookup reports the object gone, but the child is no longer the registered one (it was replaced or removed during the lookup) | nothing is removed here | delete, naming the stale inode | success |

The last row is the only one where a change made during the lookup alters the outcome,
because it is the only one where a removal is attempted. When the lookup succeeds, or
fails for another reason, no removal is attempted and the second or third row applies
whether or not the child gained unflushed changes meanwhile: the child is kept, and the
result is success or the error.

The reasons behind the table:

- **The delete notification names the stale inode.** On this path the kernel's directory
  entry holds the inode it passed in. If the kernel had looked the name up again it would
  hold the current inode, and the call would not be on the stale path at all. Upstream
  already notified with that inode; this is unchanged.
- **Removal happens only on "not found".** A throttle or a server error says nothing about
  whether the object exists, so it must not unlink a valid child. For a directory, a wrong
  removal detaches the whole subtree until the next listing. This makes the stale path
  stricter than the inherited one.
- **A child with unflushed changes is not removed**, with one open exception for
  directories, described under "Invariants and locking". Removal goes through
  `removeChildUnlessDirty`, which checks again, under the locks, that the child is still
  the registered one and still clean.
- **No slurp listing is issued on this path**, as upstream.

## Invariants and locking

- `isDirtyLocked` requires the inode's own lock. It reports an inode as dirty when it is
  not in the cached state, or when it is a directory with unflushed descendants. It is the
  same test `LookUpCached` applies before dropping a cached child, written out again
  instead of being factored out of `LookUpCached`, because `LookUpCached` is inherited.
- `refreshCurrentChild` and `removeChildUnlessDirty` must be called with none of the
  filesystem lock, the parent's lock or the inode's lock held. Both take the parent's lock
  and then the child's lock, which is the repository's lock order and the order
  `removeChild` uses.
- `removeChildUnlessDirty` returns true only when it kept the child because it was dirty.
  It returns false both when it removed the child and when the inode was not the
  registered child and nothing was done.
- For a file, the `NOTE:` on `removeChildUnlessDirty` states that the check is closed: the
  cache state changes through `SetCacheState`, under the inode's lock, which is held
  across the check and the removal.
- For a directory, a window remains, and the code carries a `NOTE:` on
  `removeChildUnlessDirty` that says so. A directory's count of modified children is
  changed by a plain atomic add in `addModified`, which holds no lock that
  `removeChildUnlessDirty` can take: a write or an open under the directory holds only the
  lock of the inode being written. So a directory can become dirty after the check and
  still be removed. The cost is a detached subtree, not lost data. The directory is
  dropped from its parent until the next listing re-creates it, while its dirty
  descendants are still queued for flushing and still flush. The re-listing creates a
  different inode object for the name, so until a detached dirty descendant has flushed, a
  traversal through the new object does not show it. This window is open, not resolved.
  Closing it means changing the inherited `addModified`, which has not been done. Do not
  delete or reword the `NOTE:` without approval.

## Tests that pin it

All in `core/refresh_inode_cache_test.go`. The tests whose names end in `NoCloud` need no
object-store fixture and run locally under the race detector.

- `TestRefreshInodeCacheRemovesCurrentChildNotifiesStaleIdNoCloud` — the object is gone:
  the current child is removed and the notification names the stale inode.
- `TestRefreshInodeCacheKeepsCleanChildThatStillExistsForStaleInodeNoCloud` — the object
  exists: the child is kept.
- `TestRefreshInodeCacheKeepsCleanChildOnLookupErrorForStaleInodeNoCloud` — a lookup
  error other than "not found" keeps the child and is returned.
- `TestRefreshInodeCacheKeepsDirtyCurrentChildForStaleInodeNoCloud` and
  `TestRefreshInodeCacheKeepsDirtyCurrentDirForStaleInodeNoCloud` — a dirty file or
  directory is kept.
- `TestRefreshInodeCacheKeepsChildDirtiedDuringLookupForStaleInodeNoCloud` and
  `TestRefreshInodeCacheReturnsLookupErrorForChildDirtiedDuringLookupNoCloud` — a child
  that becomes dirty while the lookup is in flight is kept, and the right result is
  returned.
- `TestRemoveChildUnlessDirtyNoCloud` — the helper's outcomes.
- `TestRefreshInodeCacheRemovesCurrentChildForStaleInode` — the original test, which needs
  the object-store fixture.

The description of #4 states that each behaviour in the table was observed failing on the
unfixed code and passing with the fix.

## Upstream status

Upstream has no fix for this; the sync log in [upstream-syncs.md](upstream-syncs.md)
records when that was last checked. The fix was first ported from TigrisFS `97e8dc9` and
then reworked in review of #4, so it no longer matches the TigrisFS commit either.

## Sync notes

- The logic is in files that exist only here, so a sync has nothing to conflict with
  there. The change to inherited code is the call in `RefreshInodeCache` in
  `core/goofys.go`, which replaced the call to `recheckInode`, and the comment above it;
  keep both.
- The code depends on inherited functions that a sync can change without any conflict.
  This is the single list of them; the register entry points here instead of repeating
  it. If a sync touches any of them, re-read this file and run the tests above.
  - Called from `core/refresh_inode_cache.go`: `findChildUnlocked`, `findInodeFunc`,
    `recheckInode`, `LookUp`, `mapAwsError`, `removeChildUnlocked` and `isDir`.
    `removeChildUnlessDirty` uses `findInodeFunc` to search the parent's children the
    way `removeChild` does, so it relies on that list staying sorted in the same order.
  - Not called, but mirrored or relied on: `removeChild` (its lock order and its check
    that the inode is the registered child), `LookUpCached` (its test for a dirty child:
    if that changes, `isDirtyLocked` must follow), `SetCacheState` (the file half of the
    dirty check relies on the cache state changing only under the inode's lock) and
    `addModified` (the open directory window).
- If upstream adds locking to `addModified`, the directory window described above can be
  closed; update the `NOTE:` (with approval) and this file together.
- If upstream fixes the stale-inode defect itself, follow the policy in
  [README.md](README.md): audit their fix and compare it with ours against the table
  above — in particular removal only on "not found" and keeping a dirty child — then
  adopt, keep ours or combine, and record the outcome.
