RULING (D-9432-1, arbiter-eshu, 2026-11-20): Ratify the pinned virtual-slot naming already on main, `vuln-intel://osv/<ecosystem>/_slot/<vslot>/<chunk>` with V=1024 decoupled from #9422's P, for cutover. The cutover is gated on a truncation fail-closed fix, satellite-first deployment, and a #9431-rule additive migration. No new naming scheme is invented.

## Provenance of this record

The original ruling file was cited by the #9432 issue comment of 2026-11-20 and by later rulings, but it was never committed. Its full text was not recovered: it is in no git ref, no worktree, and no session log that was searched. This record is rebuilt from the sources below and says which part comes from which.

| Part | Source | Status |
|---|---|---|
| "The ruling" and "Key terms" | The #9432 issue comment posted 2026-11-20T12:05:41Z, "Arbiter design ruling D-9432-1 recorded" | Carried over from the comment, lightly reformatted. The comment is itself a summary, so detail the original held may be missing. |
| "What the code shows today" | `go/internal/coordinator/vulnerability/` and `go/internal/workflow/` at `f2ec26f68b` | Re-read for this record. |
| "Follow-up ruling, 2026-11-22" | The keyed-lag arbiter ruling of 2026-11-22 (decisions 1 and 2, the satellite finding, and its "will not do" list), inlined here because committed docs do not cite agent scratch files | Reconstructed from that ruling's text. The QA figures in it are not re-measured here. |

Nothing below is invented. Where a source gave no detail, this record gives none.

## The ruling

Ratify the pinned virtual-slot naming already on main (`vuln-intel://osv/<ecosystem>/_slot/<vslot>/<chunk>`, V=1024 decoupled from #9422's P) for cutover. The cutover is gated on a truncation fail-closed fix, satellite-first deployment, and a #9431-rule additive migration. No new naming scheme is invented.

## Key terms

- **Format.** Exactly `vuln-intel://osv/<ecosystem>/_slot/<vslot>/<chunk>`, with no query string.
- **P never appears in scope ids.** When P exceeds V, the window falls back to legacy `_batch` minting.
- **Truncation fail-closed is a required code fix, failing tests first.** Truncated or budget-cut windows, and windows with release-bearing queries, mint legacy `_batch` scopes and never `_slot` scopes.
- **Chunk-shift transients are accepted and bounded.** After an ack, the union of the slot generations must equal the new window's target set. Orphans are retired by the #9431 pattern, never by empty generations.
- **Migration is a replacement-first retire**, run only after one full rotation with slot coverage. It sets `status = 'superseded'`, clears the pointer, and writes an audit marker. It uses no DELETE, no DDL, and no rewrites.
- **Rollback** is the flag flip back.
- **Readers are unchanged.** They take the union of batch and slot scopes.
- **The satellite consumes `_slot` claims** as one querybatch per scope and keeps parsing `_batch`.

## Required proof

- A failing-first storage proof: two windows leave one active generation per slot scope, including the RED truncated and release-bearing cells.
- A read-only QA census before the flip: re-measured shapes, T and P with P > 1024 headroom, and multi-chunk virtual slots.
- A census after one full rotation: one generation per scope, fallback reasons named on the plan span, the legacy remainder enumerated, and one advisory spot-check per ecosystem for the criterion-2 claim of #9209.

## Not verified when the ruling was made

The implement lane had to prove each of these or gate on it:

- QA live counts.
- Deployed satellite `_slot` resolution, which must be checked before any flip.
- Duplicate findings while batch and slot scopes coexist.
- Whole-virtual-slot containment of `slot_progress`.

## What the code shows today

Read at `f2ec26f68b`:

- The pinned modulus is `OwnedPackageVirtualSlotCount = 1024` in `go/internal/workflow/owned_target_slot_progress.go`. The coordinator reuses it as `ownedPackageVirtualSlotCount`.
- The scope id is built by `vulnerabilityOSVSlotScopeID` in `go/internal/coordinator/vulnerability/intelligence_slot_scopes.go`. It carries no queries.
- The mint decision is `slotScopeMintFallback` in the same file. It returns a fallback reason for four cases: `reader_slot_count_exceeds_virtual_slots`, `truncated_window`, `budget_exhausted`, and `release_bearing_queries`. Each case mints legacy `_batch` scopes. This is the truncation fail-closed fix the ruling required.
- The flag is `SlotScopes bool` with JSON key `slot_scopes` on the derivation configuration in `go/internal/coordinator/vulnerability/intelligence_scheduler.go`. It is read in `intelligence_derivation.go`. It defaults to off.

## Follow-up ruling, 2026-11-22

The 2026-11-22 keyed-lag ruling made the winners-watermark lag on QA (#11075, #10972) part of this issue's work. It checked D-9432-1 against the state of the satellite and recorded these points.

- **Root cause: batch scopes never supersede (#9432).** Legacy `_batch` scopes are additive and supersede nothing at ack, so each key keeps many active copies. The core side of the cutover is merged: #9587, #9730 (claim transport), and #11040 (fail-closed windows).
- **The cutover is not a flag change today.** The vulnerability-intelligence satellite on its main branch (v0.1.13) parses only `_batch` scopes. It refuses an unknown scope id as "not configured", so it cannot resolve a `_slot` claim. The satellite-first precondition of D-9432-1 is unmet. The ruling checked the SDK and found the `Claim.ScopeQueries` field already present, so the satellite change is parsing and fail-closed handling, not a new transport.
- **Order of work, each step gated on the one before:**
  1. A satellite change that parses `_slot` claims, takes the queries only from `Claim.ScopeQueries`, makes one querybatch per scope, fails closed on an absent, empty, over-max, or blank query list, and keeps `_batch` parsing. It needs failing tests first for each fail-closed case, a claim-path test that emits the same facts as the `_batch` scope for the same queries, and a release tag.
  2. A deployment change that pins the new satellite release. It is reversible by the previous digest.
  3. This record committed to the repository, so the flip cites a file that exists.
  4. A read-only pre-flip census on the replica: T, the reader slot count P against 1024, the per-window candidate count against the owned-package `target_limit`, multi-chunk virtual slots, and the check for any `vuln-intel://` scope grant.
  5. `slot_scopes: true` for the QA vulnerability-intelligence instance only, as a named and reversible act, followed by a readback after one full rotation.
  6. Retirement of the legacy `_batch` scopes, which is a production data change and needs its own review.
- **Enabling slot naming stops the growth of `_batch` scopes. It does not retire the existing ones.** Retirement keeps the #9431 shape named above: mark superseded, clear the pointer, write an audit marker, no deletes, replacement-first. Installed-evidence scopes (alpine, debian, ubuntu, rocky, alma) and failed or pending scopes are excluded. The first batch is five scopes, after a census, a dry run, and a before-and-after key differential.
- **Will not do, per the ruling:** flip `slot_scopes` before the satellite resolves `_slot`, because every slot claim would be refused; retire or delete any scope by age; and add a dedup or recency rule to the winner select as a substitute for the cutover.
- **Gain from the cutover is a projection.** The ruling treats the earlier 25 to 60 times estimate as unproven. The keyed probe reads every fact of an identifier before the active filter, and retention keeps superseded generations, so a retired copy still has a cost. A measurement (#11142 M16) decides whether retirement alone is enough.

## Related

- #9432 (this design), #9431 (retire pattern), #9209 (criterion 2).
- #11075, #10972, #11141, #11142, #11143 (the winners-watermark lag that the cutover addresses).
