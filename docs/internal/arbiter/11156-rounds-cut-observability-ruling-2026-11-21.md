# Round-cap frontier observability for #11156: arbiter ruling (2026-11-21)

Provenance: the passages below are the arbiter's decision 9 and the source
facts it rests on, recorded by the coordinator from the ruling on #11158 and
#11156 (epic #10860). Replica row counts and repository identifiers in the
original are left out; nothing in the ruling changed. This note covers only
the observability PR. The fix for #11156 is deferred under the same ruling.

## Verified source

- The rounds cut fires when the follow-up filter is still non-empty after
  `maxSupplyChainImpactActiveEvidenceLoads` (8) rounds, and each follow-up is
  derived from everything loaded so far.
- The follow-up is a graph closure over 13 arms: a loaded `affected_package`
  adds package and CVE ids, a `package_consumption` adds package and
  repository ids, and workload, image and SBOM facts add repository ids and
  digests. The cap is a depth cap on that walk, not a size cap on the seed.
- A partial pass upserts only; one pass-level stamp. `partial_since` is kept
  across partial passes and cleared only by a full complete pass.
- `active_evidence_loads` is a per-pass sub-signal, but a truncated pass only
  reports 8. Nothing recorded the size or arm of the unrequested frontier at
  the cut.

## Decision 9

Approved now: an observability-only PR. At the rounds cut, log and emit the
per-arm sizes of the unrequested frontier and the envelope count at the cut.
No behavior change, no migration, no contract bump. Gates: focused tests,
telemetry-coverage, docs build. Required proof: a test that fails without the
new fields and passes with them, on a pass cut at round 8.

Not approved by this ruling: raising or adapting the cap, retiring rows after
N partial passes, splitting the batch, a resumable frontier, and any change to
the winner statements or the stamp.
