# AGENTS.md — internal/graph/capture guidance for LLM assistants

## Read first

1. `go/internal/graph/capture/README.md` — pipeline position and the
   exported surface.
2. `go/internal/graph/capture/doc.go` — the package contract anchor.
3. `go/internal/backendconformance/differential.go` — the fingerprint,
   digest, and recorder, plus `differential_kinds.go` (`CompareRecordings`
   with divergence kinds) and `differential_unwind.go` (batch explosion)
   that this package persists and diffs; do not reimplement any of them
   here.

## Invariants this package enforces

- **Default test path is hermetic** — unit tests use fake seams and
  `t.TempDir()` sinks. The only live test is `differential_live_test.go`,
  which skips without `ESHU_DIFFERENTIAL_LIVE=1` plus both backend URIs
  and cleans its `answer-truth-differential:` fixtures on both backends.
- **Both backends or failure** — `Compare` fails when either side has no
  recordings, so a half-finished run can never look green.
- **Unknown backends fail on both sides** — `OpenDir` rejects them at
  record time, `LoadDir` rejects them at diff time; never guess a side.
- **Capture is observable-neutral** — add no telemetry here. A signal
  that fires per statement would perturb the replay under test and show
  up in its timings.
- **Env vars stay gate tooling** — `ESHU_DIFFERENTIAL_CAPTURE`,
  `ESHU_DIFFERENTIAL_CAPTURE_DIR`, and `ESHU_DIFFERENTIAL_CAPTURE_PHASE`
  are registered in `go/internal/envregistry` under the
  `backend-conformance` subsystem because the public gate docs cite them;
  never set them in production Compose files or service defaults.

## Common changes and how to scope them

- **New divergence class to excuse** → extend `AllowlistEntry` matching
  in `allowlist.go` with a test that a named entry excuses it and an
  unnamed one still fails; update
  `specs/backend-divergence-allowlist.v1.yaml` docs in the same change.
  Scope the tier to the narrowest kind the excuse needs — a dialect
  excuse must never match `results` on a read that should agree. Never
  reintroduce an `executions` or `rowcount` tier: both kinds are advisory
  at the gate (`backendconformance.AdvisoryKind`), so an entry for either
  is dead weight and the parser rejects both. A timing-dependent read
  whose digest disagrees across legs belongs in `transient_reads`, not in
  `entries`: it takes no tier, skips stale checking, and must carry a
  transient-state marker (`uid IS NULL`,
  `eshu_orphan_observed_at_unix`) — the parser rejects anything else. An
  ORDER BY read with no `LIMIT` or `SKIP` whose keys can tie (order-only
  delivery nondeterminism, never a row-multiset difference) belongs in
  `tie_order_reads`, not in `entries`: it takes no tier, skips stale
  checking, and must carry `ORDER BY` with no truncation — the parser
  rejects anything else, because with truncation tied keys change which
  rows return.
- **Richer diff output** → keep `MaxReportedDiffs` bounded (the gate's quorum phase shares it for its per-pairing dump); the full
  recordings stay in the CI artifact for the unbounded case.
- **New capture tier** → open the session once at binary startup with
  the binary name, decorate the outermost seam, and close it on the
  existing shutdown path; never wrap per-request.

## What NOT to change without an ADR

- The JSONL record envelope: the diff job and any external artifact
  tooling read it; version the schema rather than mutating in place.
- The allowlist's empty-by-default policy: every divergence fails until
  named with a reason, a tier, and an upstream issue link.
