# G2 Wave-1 seven-leg record (core side)

Refs `eshu-hq/eshu#9669`, satellite `eshu-hq/eshu-api#101` RE:G2V2.
Parent plan: `docs/internal/9669-removal/step2-wave-plan.md` (Wave 1).
Ruling: `docs/internal/arbiter/9669-step2-scope-ruling-2026-11-18.md`
(decisions 3, 7, 8). Frozen rows:
`contracts/step2-frozen-scope.md` FORWARDED-READS R4-R7 plus the G2
base rows.

This lane proves 9 rows core-side with the W8a seven-leg shape
(satellite `api/w8a_*_test.go` at `08ebde4` is the template,
reference only — every line of proof here is new core-side code per
the oracle discipline): the 5 G2 base legs (ExtractionReadiness 2,
FactSchemaVersions 2, SurfaceInventory 1, served through their v0
admission pins since Wave 0) plus the 4 forwarded-read rider
spellings (freshness keyed POSTs x3, surface-inventory keyed POST).

## Per-route record

| # | Method+Path | Family | Pin | Core fragment |
| --- | --- | --- | --- | --- |
| 1 | `GET /api/v0/collector-extraction-readiness` | ExtractionReadiness | `ExtractionReadinessReadAdmission v0` | `go/internal/query/openapi/paths/status/collector_extraction_readiness.go` |
| 2 | `GET /api/v0/collector-extraction-readiness/{family}` | ExtractionReadiness | `ExtractionReadinessReadAdmission v0` | same as row 1 |
| 3 | `GET /api/v0/fact-schema-versions` | FactSchemaVersions | `FactSchemaReadAdmission v0` | `go/internal/query/openapi/paths/status/fact_schema_version.go` |
| 4 | `GET /api/v0/fact-schema-versions/{fact_kind}` | FactSchemaVersions | `FactSchemaReadAdmission v0` | same as row 3 |
| 5 | `GET /api/v0/surface-inventory` | SurfaceInventory | `SurfaceInventoryReadAdmission v0` | `go/internal/query/openapi/paths/catalog/surface_inventory.go` |
| 6 | `POST /api/v0/freshness/changed-since` | Freshness (rider R4) | `FreshnessReadAdmission v0` (base leg) | `go/internal/query/openapi/paths/freshness/changed_since.go` |
| 7 | `POST /api/v0/freshness/generations` | Freshness (rider R5) | `FreshnessReadAdmission v0` (base leg) | `go/internal/query/openapi/paths/freshness/generations.go` |
| 8 | `POST /api/v0/freshness/services/changed-since` | Freshness (rider R6) | `FreshnessReadAdmission v0` (base leg) | `go/internal/query/openapi/paths/freshness/service_changed_since.go` |
| 9 | `POST /api/v0/surface-inventory` | SurfaceInventory (rider R7) | `SurfaceInventoryReadAdmission v0` (base leg) | same as row 5 |

The riders are spellings, not routes: the table pins each rider to
its GET base leg's pin and fragment. The freshness GET base legs
stay owned by their later waves; the visualization derive POST and
the manifest-dependencies GET belong to other lanes and never enter
this wave.

## What this lane proves (one leg per row family)

1. Conformance (`g2_wave1_conformance_test.go`): frozen 9-row table,
   native mount (GETs serve, POST riders judge 405 with
   `Allow: GET`), predicate-vs-mux agreement, slash twins 404 with
   zero admission contact, unknown routes 404, earlier-lane
   freshness paths out of the wave.
2. Auth (`g2_wave1_auth_test.go`): wave paths stay key-gated,
   scoped allowlist admits the 5 GET base legs only, shared-key
   terminates 200 with exactly one admission contact,
   credential-less/junk/empty-key stay 401 with zero contact, keyed
   riders judge 405 with zero contact.
3. Fail-closed (`g2_wave1_failclosed_test.go`): failing inventory
   admission surfaces the fixed 500 with no `Retry-After` in both
   envelopes, riders judge before admission, unknown
   families/kinds 404, unset admissions keep serving the real
   catalog (never fail-open-to-empty). No 503 leg: no Wave-1 row
   reads a live backend.
4. Parity-GET (`g2_wave1_parity_get_test.go`): repeat-read byte
   identity across the status matrix x 2 envelopes, filter queries
   intact, per-family wire shapes (schema_version marker plus truth
   capability).
5. Parity-POST (`g2_wave1_parity_post_test.go`): rider judgments
   byte-identical across lanes, bodies, and envelopes; bodies never
   change the judgment; `Allow` names the pinned GET.
6. Latency (`g2_wave1_latency_test.go` +
   `scripts/measure-g2-wave1-latency.sh`): pin-backed vs
   static-default per family, bound 1.50x.
7. Spec (`g2_wave1_spec_test.go`): served `openapi.json` 200 valid
   JSON, 8 path keys with pinned operationIds, no `post` under any
   wave key (riders are spellings, not routes).

Lane rule and predicate: `go/internal/query/g2_wave1_terminate.go`
(core judges each row's method; invents nothing). Frozen table:
`go/internal/query/g2_wave1_wave.go`.

## Known divergences (disclosed, not hidden)

- The served spec CARRIES the wave GET keys (core owns the
  fragments) — the inverse of the satellite disclosed-absence
  posture, which holds dest-side only.
- The twin-slash spellings 404 core-side (Go mux has no slash-twin
  mounts); satellite terminates its twins natively. Both postures
  agree on never-split: a twin never serves a different answer.
- The 405 judgment body is the mux's plain-text page, not an
  envelope error: the judgment happens before negotiation, so there
  is no envelope to honor.

## Rollback

Single-commit revert: remove `g2_wave1_wave.go`,
`g2_wave1_terminate.go`, the `g2_wave1_*_test.go` suite,
`scripts/measure-g2-wave1-latency.sh`, and this record. The legs
return to their Wave-0 standing (pins plus canned-pin proofs, no
seven-leg suite) with zero traffic or data movement — no handler,
route, or admission changes ride this lane. Verify by re-running
the lane suite against the reverted tree: `TestG2Wave1Frozen` goes
red (table gone) while every other suite stays green.
