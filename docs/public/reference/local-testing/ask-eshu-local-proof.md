# Ask Eshu Local Proof

The Ask Eshu local proof is the repeatable, secret-safe gate for the Ask Eshu
answer path. It proves the real runtime path — the API router mux, the
scoped-auth middleware, the ask wiring, the ask engine, the provider adapter,
the runtime answer guardrail, and both the JSON and SSE handlers — enforces
citation coverage and publish-safety on a configured provider, in both the
zero-provider deterministic mode and provider-backed mode.

The default run is fully offline and needs no DeepSeek credentials and no graph
or Postgres backend. It uses a local stub provider through the
`endpoint_profile_id` base-URL override, so the provider-backed path is exercised
against a local server instead of a hosted endpoint. The hosted, real-DeepSeek
end-to-end rerun is operator-local only; it is described at the end of this page
and is never run in CI or committed.

The upstream blockers for this proof — #3322, #3323, and #3324 — are RESOLVED
(merged), and PR #3336 landed the runtime answer guardrail this proof asserts, so
this checklist is unblocked.

## What It Proves

The proof drives POST /api/v0/ask through the real assembled handler and asserts:

| State | Expectation |
| --- | --- |
| Ask disabled | 503 `unavailable` (no `ESHU_ASK_ENABLED=true`). |
| Missing provider | 503 `unavailable` (ask enabled, no agent_reasoning profile). |
| Bad provider | 503 `unavailable` (profile present, credential env var unset, adapter build fails). |
| Scoped token | The scoped-token allowlist admits POST /api/v0/ask. |
| Status surface | GET /api/v0/status/answer-narration reports `provider_configured` consistent with adapter readiness. |
| Clean cited answer | 200 with narrated prose plus evidence handles, on both JSON and SSE (validated token deltas). |
| Publish-safety failure | An answer whose narration carries an AKIA-style key, a Bearer token, or a raw address is suppressed on both JSON and SSE — no leak in the token deltas. |
| Uncited claim | A factual narration sentence with no provenance is rejected by the governed validator and suppressed. |

It also scores a committed redacted answer-quality scorecard fixture that covers
all seven prompt families, proving the answer-quality and publish-safety
scorecard path.

## Run The Proof

```bash
# Offline, CI-runnable, secret-safe. Drives the real ask runtime path with a
# local stub provider, then scores the committed redacted scorecard fixture.
scripts/verify-ask-eshu-local-proof.sh

# Print the proof commands without running them.
scripts/verify-ask-eshu-local-proof.sh --list

# Offline structural and redaction self-test for the harness itself.
scripts/test-verify-ask-eshu-local-proof.sh
```

The harness runs these gates:

```bash
cd go && go test ./cmd/api -run 'TestAskLocalProof' -count=1
cd go && go test ./internal/query -run 'TestScopedHTTPRoute_Ask' -count=1
cd go && go test ./cmd/eshu -run 'TestAskEshuLocalProofScorecardFixturePasses' -count=1
cd go && go run ./cmd/eshu answer-quality-scorecard \
  --from cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json
```

## Redacted Sample Output

```text
==> ask runtime path proof (disabled/missing/bad provider, status, JSON+SSE cited success, leak suppression)
ok  	github.com/eshu-hq/eshu/go/cmd/api	0.7s
==> scoped-token allowlist admits POST /api/v0/ask
ok  	github.com/eshu-hq/eshu/go/internal/query	0.8s
==> committed scorecard fixture CLI regression
ok  	github.com/eshu-hq/eshu/go/cmd/eshu	0.7s
==> answer-quality + publish-safety scorecard over the committed redacted fixture
Answer-quality scorecard PASSED
  run   : ask-eshu-local-proof-redacted
  score : 100
  [ok] family_coverage: all major answer families captured
  [ok] citation_coverage: all captured prompts passed
  [ok] publish_safety: evidence contains only redacted publishable strings
ask eshu local proof verification passed
```

## How The Stub Provider Stands In For DeepSeek

The proof configures `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON` with an
`agent_reasoning` profile whose `provider_kind` is `deepseek` and whose
`endpoint_profile_id` is a local stub URL. `endpoint_profile_id` is a base-URL
override, so the OpenAI-compatible adapter posts to the local stub's
`/v1/chat/completions` instead of `https://api.deepseek.com`. The credential is
read from an environment variable and is a non-secret placeholder. This exercises
the real provider adapter, the real tool-call loop, the real governed narration
call, and the runtime guardrail without any real provider credentials.

The single backend-dependent leaf — the in-process tool dispatch that would
normally read the graph — is served by a controllable handler so the
cited-versus-suppressed behavior is deterministic and offline. Auth, scoped
routing, status, the engine, the provider adapter, and the guardrail all run as
they do in production.

## Operator-Local Real DeepSeek Rerun

The hosted, real-DeepSeek end-to-end rerun is operator-local only. Run it from a
private operator environment that already has a live Eshu API stack running with
the DeepSeek `agent_reasoning` profile configured. Export only operator-local
values; do not commit transcripts, credentials, private endpoints, or captured
responses:

```bash
export ESHU_ASK_DEEPSEEK_API_KEY='<operator-local value>'
export ESHU_SEMANTIC_PROVIDER_PROFILES_JSON='<operator-local provider profile JSON>'
export ESHU_ASK_LOCAL_PROOF_BASE_URL='<operator-local Eshu API base URL>'
export ESHU_ASK_LOCAL_PROOF_API_TOKEN='<operator-local API token>'

scripts/verify-ask-eshu-local-proof.sh --deepseek
```

This branch refuses to run unless the operator credential environment variables
are present, and it never echoes their values. CI never sets these, so the branch
never runs in CI. When enabled, the script runs the offline proof first, then
executes the live API proof:

```bash
GET  /api/v0/status/answer-narration
POST /api/v0/ask                 # JSON
POST /api/v0/ask                 # SSE with Accept: text/event-stream
go run ./cmd/eshu answer-quality-scorecard \
  --from cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json
```

The live response captures stay in a temporary directory, are never printed, and
are scanned before the final pass marker. Any credential-like value, private
host, raw address, missing JSON evidence/truth field, missing SSE answer/done
frame, failed curl request, or scorecard failure makes the proof exit non-zero.
The scorecard command still consumes the committed redacted
`answer-quality-scorecard/v1` artifact; raw Ask API transcripts are not treated
as scorecard artifacts.

## Answer-Quality Scorecard Fixture

The committed fixture lives at
`go/cmd/eshu/testdata/ask-eshu-local-proof-scorecard.json`. It is share-safe:
placeholders and redacted handles only, no private paths, hostnames, credentials,
or raw addresses. For the scorecard contract and the per-family proof, see
[Answer Quality Scorecard](answer-quality-scorecard.md).

## Verification

```bash
cd go && go test ./cmd/api ./internal/query ./cmd/eshu -count=1
scripts/test-verify-ask-eshu-local-proof.sh
scripts/verify-ask-eshu-local-proof.sh
```

Docs-only updates to this page still require the strict docs build and
`git diff --check`.
