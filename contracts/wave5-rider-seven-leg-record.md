# Wave-5 rider seven-leg record (core side)

Refs `eshu-hq/eshu#9669`, satellite `eshu-hq/eshu-api#101`.
Parent plan: `docs/internal/9669-removal/step2-wave-plan.md` (Wave 5).
Ruling: #9669 (retired evidence record)
(decisions 3, 7, 8). Frozen rows:
`contracts/step2-frozen-scope.md` FORWARDED-READS R1-R3.

This lane proves 3 rows core-side with the W8a seven-leg shape
(satellite `api/w8a_*_test.go` at `08ebde4` is the template,
reference only — every line of proof here is new core-side code per
the oracle discipline): the R1 wrong-method twin (PATCH
/api/v0/code/bundles, judged 405 with `Allow: POST`) and the R2/R3
slash twins (GET /api/v0/auth/sign-in-policy/ and GET
/api/v0/auth/providers/, judged 404 — a twin never serves). The base
spellings keep their lanes: bundles POST stays Wave-4-owned (its
serving proof rides the code-graph-scan pin; this lane only proves it
stays mounted), and the R4-R7 freshness/surface-inventory riders
belong to Wave 1, never this wave.

## Per-route record

| # | Method+Path | Family | Pin | Core fragment |
| --- | --- | --- | --- | --- |
| 1 | `PATCH /api/v0/code/bundles` | Code (R1) | `CodeGraphScanReadAdmission v0` (base leg) | `go/internal/query/openapi/paths/code/graph.go` |
| 2 | `GET /api/v0/auth/sign-in-policy/` | SignInPolicyReads (R2) | `SignInPolicyReadAdmission v0` (base leg) | `go/internal/query/openapi/paths/auth/sign_in_policy.go` |
| 3 | `GET /api/v0/auth/providers/` | AuthProviders (R3) | `SignInPolicyReadAdmission v0` (base leg) | `go/internal/query/openapi/paths/auth/routes.go` |

The riders are spellings, not routes: the table pins each rider to its
base leg's pin and fragment. The slash-twin 404s need no base-leg
change: the Go mux mounts no slash-twin spelling, and the judgment
lands before any admission is touched.

## What this lane proves (one leg per row family)

1. Conformance (`w5_riders_conformance_test.go`): frozen 3-row table,
   native judgments (PATCH 405 with `Allow: POST`, twins 404 with
   zero admission contact), predicate-vs-mux agreement, unknown routes
   and deeper subtrees (e.g. `/api/v0/auth/providers/oidc`) 404, and
   the base legs serving through their pins on the same mux.
2. Auth (`w5_riders_auth_test.go`): rider spellings stay off the public
   list (bases stay public), the scoped allowlist admits no rider
   (bundles POST stays grant-bound), keyed riders judge with zero
   contact, credential-less/junk-credential riders stay 401 with zero
   contact, and the public R2/R3 bases serve keyless with pin contact.
3. Fail-closed (`w5_riders_failclosed_test.go`): nil admissions keep
   the same judgments with no `Retry-After`; the base legs fail closed
   (admin sign-in-policy 503, public/provider safe defaults).
4. Parity-GET (`w5_riders_parity_get_test.go`): twin judgments byte
   identical across lanes × 2 envelopes, query params intact (a twin
   with params is still a twin), bases serve, per-family wire shapes
   (require_sso hint, canned provider).
5. Parity-POST (`w5_riders_parity_post_test.go`): R1 judgments
   byte-identical across lanes, bodies, and envelopes; bodies never
   change the judgment; `Allow` names the pinned POST. The upstream
   body half is vacuous for a judgment (the mux answers before any
   reader runs), which the byte-identity legs prove rather than
   assume.
6. Latency (`w5_riders_latency_test.go` +
   `scripts/measure-w5-riders-latency.sh`): pin-backed vs
   static-default judgment per spelling, bound 1.50x.
7. Spec (`w5_riders_spec_test.go`): served `openapi.json` 200 valid
   JSON, the three base keys with pinned operationIds
   (`searchCodeBundles`, `getPublicSignInPolicy`,
   `listAuthProviders`), no `patch` under bundles, no twin keys.

Lane rule and predicate: `go/internal/query/w5_riders_terminate.go`
(core judges each rider's spelling; invents nothing). Frozen table:
`go/internal/query/w5_riders_wave.go`.

## Known divergences (disclosed, not hidden)

- The served spec CARRIES the base keys (core owns the fragments) —
  the inverse of the satellite disclosed-absence posture, which holds
  dest-side only.
- The twin-slash spellings 404 core-side (Go mux has no slash-twin
  mounts); the satellite terminates its twins natively per
  `api/router.go:437-455`. Both postures agree on never-split: a twin
  never serves a different answer.
- The 405 judgment body is the mux's plain-text page, not an envelope
  error: the judgment happens before negotiation, so there is no
  envelope to honor.
- GET /api/v0/code/bundles is a second wrong-method spelling of the
  same mounted POST leg: it 405s with `Allow: POST` exactly like R1,
  pinned in the parity-POST leg, not the frozen table.

## Rollback

Single-commit revert: remove `w5_riders_wave.go`,
`w5_riders_terminate.go`, the `w5_riders_*_test.go` suite,
`scripts/measure-w5-riders-latency.sh`, and this record. The legs
return to their Wave-0 standing (pins plus contract proofs, no
seven-leg suite) with zero traffic or data movement — no handler,
route, or admission changes ride this lane. Verify by re-running the
lane suite against the reverted tree: `TestW5RidersFrozen` goes red
(table gone) while every other suite stays green.
