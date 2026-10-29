# CLI Support Endpoints

Pure core logic over client-supplied artifacts for the extracted thin CLI
(8874 S4). Each route wraps one local-only function the CLI cannot carry after
extraction; every handler is a pure function of its request body with no
graph, Postgres, collector, or queue reads. The thin CLI sends bytes it
already holds (a fetched packet, a captured dossier, a bundle file, an
evidence file) and gets back bytes or an envelope.

## `POST /api/v0/investigation/packet/render`

Renders a supplied investigation evidence packet in the requested format with
the same renderer the offline `investigation export` command runs. Returns raw
bytes with the format content type (`application/json`,
`text/markdown; charset=utf-8`, `text/html; charset=utf-8`), not an envelope:
the caller asked for a rendering, not a query answer. Unknown formats fail
with 400, as do unknown fields, trailing values, and a missing packet
(`packet.packet_id` is required). Cap: 1 MiB, 413 above.

## `POST /api/v0/service/report/compose`

Composes a service intelligence report from a captured service-story dossier,
its truth envelope, and an optional captured supply-chain inventory (plus its
truth), running the same `FromServiceStory` / `FromSupplyChainInventory` /
`Compose` chain the offline `service-report` command runs. Answers a report
envelope. Malformed input fails with 400, as do unknown fields, trailing
values, and a missing dossier. Cap: 1 MiB, 413 above.

## `POST /api/v0/report/bundle/capture`

Redacts, digests, and validates client-supplied capture inputs into a
`wrong_answer_report.v1` bundle with the same fail-closed gate the offline
`report capture` command runs. The query envelope is supplied in full; the
server issues no query. Credential-bearing targets are refused with 422 and
never echoed (the contract tests reuse the CLI credential canary). Every
other capture failure is also 422: every failure reachable over the wire is
an input refusal, since the envelope already survived a JSON round trip on
the way in. Malformed JSON fails with 400, as do unknown fields, trailing
values, and missing surface, target, or envelope. Cap: 8 MiB
(payload-bearing private-triage mode), 413 above.

## `POST /api/v0/report/bundle/validate`

Runs the share-safe validation gate over supplied bundle bytes, with the
`require_public` posture mirroring `--require-public`. A 200 always carries
the `{valid, message}` verdict — valid or not — because validation is a
check, not a fetch; the CLI maps it to its exit code. Malformed JSON fails
with 400, as do unknown fields and trailing values. Cap: 1 MiB, 413
above.

## `POST /api/v0/answer-quality/score`

Scores redacted answer evidence against the dogfood criteria with the same
scorer the offline `answer-quality-scorecard` command runs. Answers a
scorecard verdict envelope. Malformed evidence fails with 400, as do
unknown fields, trailing values, and a missing evidence version. Cap:
1 MiB, 413 above.

## Shared rules

Errors use the standard error envelope. Telemetry comes from the existing
per-endpoint request metrics middleware; auth flows through the standard
API auth layer. These endpoints have no MCP tools and no capability-matrix
entries: they are CLI support, not query capabilities. The per-family
endpoint-vs-retain rationale lives in
`docs/internal/evidence/8874-s4-endpoint-decisions.md`.
