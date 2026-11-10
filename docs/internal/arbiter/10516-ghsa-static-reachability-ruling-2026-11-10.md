# Arbiter ruling: GHSA static-fetch reachability (#10516)

Date: 2026-11-10. Arbiter: `arbiter-eshu` (Muse, `muse-wonder-full-1.0`).
Question: blanket refusal (A), anchor-only enforcement as the accepted end
state (B), or a hybrid (C), for the gap where a GHSA target without
`derive_ghsa_rolling_windows` still plans a static fetch.
Design: `docs/internal/design/10037-ghsa-window-scope-decision.md` section 1.
Branch in question: `drive/9959-ghsa-rolling-wiring` at `11a2966c36`.

**RULING: (C) hybrid — (A)'s blanket refusal lands in core inside the wiring
PR and blocks its merge; the satellite mirror is a sequenced follow-up that
blocks activation (#10465), not the wiring merge. (B) is rejected.**

## Verified facts (arbiter read, 2026-11-10)

1. Design section 1 already decided this question. "The planner drops it
   from the plan whenever `derive_ghsa_rolling_windows.enabled` is set, and
   core refuses a `ghsa` target without that flag. A static GHSA claim
   becomes unreachable, so the hold is enforced in code, not only in a
   README" (`10037-ghsa-window-scope-decision.md:55-60`). The implementation
   plan puts "validator rules" in the same step as the planner (S3b, line
   312) and says "Avoid: … any fallback from a held window to the static
   anchor" (line 327). (B) would amend an Accepted design by decay.
2. On `main`, `deriveGHSAWindowTargets` returns untouched when disabled
   (`ghsa_window_derivation.go:98-99`) and only matches per-type anchors via
   `isGHSAWindowAnchorTarget` (lines 43-54); any other `ghsa` target passes
   through. The wiring branch adds the config surface and
   `applyGHSAWindowDerivation` (`ghsa_window_scheduler.go` on the branch),
   which likewise drops only anchors and appends every other target.
3. The wiring branch affirmatively pins the forbidden behavior:
   `TestWorkPlannerKeepsStaticGHSAAnchorsWhenRollingDisabled` calls flag-off
   static planning "the rollback state". Rollback for GHSA must mean "GHSA
   off" (remove the targets or disable the instance), never "static claims
   resume", because a static claim stores an arbitrary modified-window slice
   as a full snapshot of the per-type scope — the measured 10-then-6
   supersession loss the design exists to close (design lines 14-20).
4. Configured non-anchor scopes are already refused at admission by both
   validators (core `vulnerability_intelligence_ghsa_config.go:92-94`;
   satellite `ghsa_config.go`, `ScopeID != ghsaScopePrefix+target.AdvisoryType`,
   checked in `/tmp/vi-81` at `drive/81-hostminted-admission` `3a4e405`).
   The planner pass-through is therefore a defence-in-depth gap for
   code-constructed targets, not a config-reachable hole today. Planner
   inputs are validated first: `runtimeConfigFromConfig`
   (`derivation_config.go:51-54`) runs the full workflow validator.
5. The workflow validator is not on the readiness read path. Its non-test
   callers are instance admission (`collector_instance.go:148`) and
   coordinator config decodes (`derivation_config.go:52`,
   `intelligence_refresh_cadence.go:41`,
   `service_installed_advisory_targets.go:55`). A document-level refusal
   cannot break readiness reads of stored scopes.
6. `max_requests` and `results_per_page` on the anchor are live connection
   fields, not dead static config: the satellite's window fetch target
   inherits both from the anchor (`ghsaWindowFetchTarget` in
   `ghsa_window_scope.go`), and design section 1 names them anchor fields
   (line 56). Only `modified_start`/`modified_end` are dead on a rolling
   anchor. Core's minted window target does not copy `max_requests`
   (`ghsa_window_derivation.go:138-155`) because the satellite inherits from
   its own configured copy; that asymmetry is correct and stays.
7. The satellite reads no `derive_*_rolling_windows` section (search of
   `/tmp/vi-81/*.go` finds none); it infers window mode from scope shape in
   `ResolveTarget` (`runtime.go:229-246`) and still has a live static claim
   path (`collectGHSA`: "static anchor claims omit it [covered_through]").
   Criterion 3 therefore needs a satellite change; it cannot be mirrored by
   core alone.
8. No in-repo production configuration carries a `ghsa` target (search over
   JSON/YAML configs finds only SARIF testdata). `eshu-deploy` pins and any
   live stored instance are NOT_CHECKED by this ruling.
9. Issue theory verdict: partially proven. The parity baseline friction is
   real but mechanical (fact 10). The claimed readiness breakage is
   disproven by the caller trace (fact 5). The omission was also a choice,
   encoded as "rollback state" in the new test (fact 3).
10. The parity harness runs whole documents through
    `ValidateVulnerabilityIntelligenceCollectorConfiguration` and already
    has divergence kinds for this shape (`ghsaCoreStricter`,
    `ghsaOutsideOneTarget` in
    `vulnerability_intelligence_ghsa_parity_rows_test.go`). The collector
    column is a snapshot at satellite `3f28ef090c`, stale against
    `/tmp/vi-81` HEAD; the satellite repo's own `parity_test.go` carries no
    GHSA rows. "Satellite parity rows" in #10516 means this core table plus
    a re-snapshot when the satellite mirror lands.

## Decisions

1. **Refusal wins; (B) is rejected.** A `ghsa` target in a collector
   configuration whose `derive_ghsa_rolling_windows.enabled` is not true
   fails `ValidateVulnerabilityIntelligenceCollectorConfiguration`. The
   error names the section key and quotes no configured value, matching the
   existing GHSA error discipline.
2. **Rolling anchor shape.** With the section enabled, a `ghsa` anchor
   validates without `modified_start`/`modified_end`. Bounds present on an
   anchor are still parsed and bounded (RFC 3339, end after start, 168h) so
   a stale static window cannot smuggle garbage; they are otherwise
   ignored. `results_per_page`, `max_requests`, and the base-URL pin stay
   required (fact 6). `refresh_interval` (required, 1h floor) moves to
   admission: the workflow validator enforces it for rolling anchors
   instead of first failing at plan time in `validateGHSAWindowAnchor`;
   the coordinator check stays as defence in depth.
3. **Planner guard (defence in depth), refuse — never silently drop.**
   `deriveGHSAWindowTargets` refuses with an error when (a) derivation is
   disabled and any configured target has source `ghsa`, or (b) derivation
   is enabled and a `ghsa` target is neither a per-type anchor nor minted
   by the derivation itself. Refusal matches the existing duplicate-anchor
   error; a silent drop would hide misconfiguration on an accuracy surface.
   Case (a) also fences stored instances admitted before this change: after
   upgrade they fail loudly at admission/plan instead of planning static
   claims. Operators roll back by removing `ghsa` targets or disabling the
   instance; flipping the flag off with targets present is a validation
   failure by design, and the wiring PR docs must say so.
4. **Satellite mirror (follow-up, pre-activation).** In
   `eshu-collector-vulnerability-intelligence`: parse
   `derive_ghsa_rolling_windows.enabled` from the instance config; admit a
   boundless anchor only when it is enabled (bounds stay required
   otherwise); refuse an anchor-scope claim at dispatch in rolling mode so
   no static fetch is reachable satellite-side either; window-scope
   resolution is unchanged. Implicit boundless-anchor admission without the
   section is rejected: a boundless static target must never reach
   `collectGHSA`. This ships as a satellite release, then core re-snapshots
   the parity collector column at that revision.
5. **Blocking.** Decisions 1-3 and their tests/parity updates land in the
   `drive/9959-ghsa-rolling-wiring` PR and block its merge: the PR adds the
   very section whose off-state must be safe, and it currently pins the
   unsafe off-state in a test. Decision 4 does not block the wiring merge
   (different repo, GHSA not activatable until #10465 S6/S7) but blocks
   activation and the S7 "not activatable" note removal. Until decision 4
   lands, a boundless-anchor document is core-admitted and
   satellite-refused; the wiring PR must state that plainly and must not
   claim boundless anchors work end to end.

## #10516 acceptance criteria, dispositioned

- "Decide refusal versus anchor-only enforcement with an arbiter ruling
  recorded in the repo." — Done by this record: refusal (hybrid sequencing
  above). Coordinator lands it at
  `docs/internal/arbiter/10516-ghsa-static-reachability-ruling-2026-11-10.md`.
- "If refusal wins: a GHSA target without the flag fails validation with a
  test, and satellite parity rows are updated in the same change." — Core
  half lands in the wiring PR: refusal test plus parity-table updates
  (baseline document gains the enabled section; a flag-off document row
  declared core-refuses/collector-accepts with reason; a boundless-anchor
  flag-on document row declared core-accepts/collector-refuses
  `modified_start`, kind `ghsaOutsideOneTarget`, reason pointing at the
  satellite follow-up issue). The collector column for new rows must be
  checked by running them through the satellite's `ParseSettings` at the
  pinned snapshot revision, per the table's own contract — the arbiter did
  not run this (NOT_CHECKED). The satellite-side table does not exist
  (fact 10); the satellite mirror PR plus core re-snapshot close this
  criterion fully, tracked under #10465 if #10516 closes earlier.
- "A rolling anchor validates without a static modified window." — Core:
  yes, in the wiring PR (decision 2), with `max_requests` deliberately
  still required — this narrows the issue's looser "modified_start and
  max_requests" phrasing per fact 6. End-to-end (satellite admits the same
  document): only after decision 4.

## Required changes

In the wiring PR (`drive/9959-ghsa-rolling-wiring`), before merge:

1. `go/internal/workflow`: document-level refusal (decision 1) and
   rolling-anchor mode (decision 2) in the GHSA validator path; update the
   `validateVulnerabilityGHSATarget` doc comment (drop the
   "configuration parity only" framing for the refusal contract).
2. `go/internal/coordinator/vulnerability`: planner guard (decision 3) in
   `deriveGHSAWindowTargets`; scheduler already propagates its error.
3. Tests: invert `TestWorkPlannerKeepsStaticGHSAAnchorsWhenRollingDisabled`
   into a refusal test; drop the static bounds from
   `testGHSAWindowAnchorJSON` so the wiring tests prove criterion 3; add
   validator tests (flag-off refusal, flag-on boundless accept, flag-on
   bounds-present still bounded e.g. >168h refusal, flag-on missing
   `refresh_interval` refusal, non-anchor `ghsa` scope refusal unchanged).
4. Parity rows per the criterion disposition above.
5. Docs touched by the branch (`README.md`/`AGENTS.md` rows it already
   edits) state the rollback rule and the core-admitted/satellite-refused
   interim for boundless anchors.

Follow-up, before activation (#10465 gate):

6. Satellite mirror (decision 4), release, `eshu-deploy` pin.
7. Core parity re-snapshot at the new satellite revision; remove the
   interim divergence rows or re-declare them as agreed.
8. File/link the satellite issue from #10516 before #10516 closes; #10516
   may close on items 1-5 plus the filed satellite issue, with #10465
   carrying items 6-7 as activation blockers.

## Required proof

- Failing tests first, per repo rules: the flag-off refusal and boundless
  anchor tests red against the branch, then green.
- Focused gates post-change (executor/coordinator evidence, not run by the
  arbiter): `go test ./internal/workflow/ -run GHSA`,
  `go test ./internal/coordinator/vulnerability/ -run GHSAWindow`, then the
  full `workflow` and `coordinator/vulnerability` packages (readiness and
  instance-admission tests that build GHSA configs surface here).
- Collector column of new parity rows verified by running the documents
  through the satellite parser at the snapshot revision (fact 10).
- No performance or concurrency proof required: refusal is admission-time
  O(targets); the planner guard is O(targets) per tick beside the existing
  anchor scan. No serialization is introduced. No telemetry change:
  admission refusal is the existing config-error surface; if the planner
  guard's error path proves silent in the scheduler logs, that is a finding
  for the wiring PR review, NOT_CHECKED here.

## What this ruling will not do

- Accept anchor-only enforcement as the end state (contradicts design
  section 1; leaves the supersession loss reachable by config).
- Drop `max_requests`/`results_per_page` requirements (live anchor fields,
  fact 6).
- Silently drop non-anchor or flag-off `ghsa` targets in the planner
  (hides misconfiguration; refusal is the honest surface).
- Let the satellite admit boundless anchors without the section, or claim
  core-only boundless anchors as working end to end before decision 4.

## NOT_CHECKED

- `eshu-deploy` pins/configs and any live stored collector instance
  carrying a `ghsa` target (upgrade impact is fail-loud by design, but the
  inventory was not taken).
- `collectGHSA` behavior on zero bounds (not run; decision 4 makes it
  unreachable rather than relying on it).
- Parity collector column re-run against any satellite revision (arbiter
  read code only; executor must run it).
- Scheduler log visibility of planner-guard refusals.
