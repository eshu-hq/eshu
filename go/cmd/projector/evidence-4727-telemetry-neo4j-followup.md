# Evidence: projector in telemetry overlay + neo4j variant (#4727 / #4739 follow-up)

Follow-up to #4739. That PR added the always-on `projector` service to the base
`docker-compose.yaml` but did not add it to the telemetry overlay or the neo4j
variant. This change closes both gaps. It is a compose-topology / backend-config
selection change only — no runtime Go code path is altered.

## Proof

No-Regression Evidence: no runtime behavior changes; only compose service
membership + backend/OTEL env selection. Verified locally on the branch:
- `go test ./internal/runtime/` PASS — `TestDefaultComposeUsesNornicDBBackend`,
  `TestNeo4jComposeUsesNeo4jBackend`, `TestDefaultComposeFilesDoNotStartTelemetry`,
  and `TestTelemetryComposeOverlayDefinesTelemetryStack` all green with `projector`
  now in `graphRuntimeServices()` / `telemetryOverlayServices()`. Red before the
  compose edits (`compose service "projector" missing`), green after.
- `docker compose -f docker-compose.yaml config`, `-f docker-compose.neo4j.yml
  config`, and `-f docker-compose.yaml -f docker-compose.telemetry.yml config` all
  validate with no duplicate host ports.
- Merged-overlay check: the projector inherits `OTEL_EXPORTER_OTLP_ENDPOINT`
  (http://otel-collector:4317) + otel-collector/jaeger deps in the telemetry
  overlay, and carries `ESHU_GRAPH_BACKEND=neo4j` in the neo4j variant (was
  defaulting to the NornicDB backend — a split-brain canonical writer).

Observability Evidence: this change ADDS operator observability. Before it, the
projector — the runtime whose job is recovering stranded source-local retries —
was the one service absent from Jaeger / the OTLP collector under the telemetry
overlay, so its recovery traces/metrics could not be seen during local diagnostic
runs. After it, the projector emits the same OTLP traces/metrics as every other
runtime (via the shared `*eshu-otel-env` anchor) and depends on `otel-collector`,
so stuck-retry recovery is diagnosable end to end. No instruments were added or
removed; only the export wiring for an existing runtime.

Refs #4727, #4739, #3624.
