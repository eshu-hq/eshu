# Demo-Corpus Loki Lane No-Regression Note (#9202)

Scope: the `drive/9202-loki-removal` demo-corpus compose rework only
(`docker-compose.demo.corpus.yaml` loki lane,
`scripts/demo-corpus-loki-reconcile.sh`, and two doc rows). It changes no
Go code, no Cypher, no schema DDL, no queue/lease/concurrency knobs, and
no Helm values. The in-tree loki deletion it builds on is earlier lane
work; its extraction evidence belongs to the lane PR, not here.

No-Regression Evidence (#9202): the previous stanza could never run
(satellite image holds only `/usr/local/bin/collector`, so the old
binary path, `-mode=cassette` flags, and curl healthcheck all failed).
Baseline behavior was a crash-looping service, so there is no passing
shape to regress. After measurement, against postgres:18-alpine plus the
pinned nornicdb backend with satellite release v0.1.2 pinned to
`sha256:12eac89e7abd2988529c6f2854ba63293b5507502630764c3f6b8ffc7e3bf84d`:
the shipped seed command writes collector
(`sha256:5e2bf9cda8d6bba44fe70998f8177972ced8b0fdc08d75257f843e0c29e779d0`)
plus manifest
(`sha256:154fe294f5ba9b8881b1d75277936f120a6ad9708cbbcbaa8b9f3211db1022bd`);
the shipped reconcile script runs to PASS (four producer grants,
allowlist install, claims-capable enable); component list reports
`installed,enabled,claim_capable`; and the worker serves its curl
`/healthz` as a non-root user with zero restarts over the probe window.
Terminal row counts: `ingestion_scopes` holds only the bootstrap's own
`eshu` scope — the worker idles without claims and commits nothing,
because the credential-free demo ships no live Loki target. The
orchestrator's expected-collector count stays at 9, so a manual demo run
fails closed at the commit wait instead of advancing on a partial
corpus. Nine-source convergence needs live QA targets through the same
activation shape and is NOT_CHECKED here.

No-Observability-Change: no new metrics, spans, logs, or status fields.
Operator signals stay the existing component-extension worker
instruments, the seed/reconcile container logs, and the compose
healthcheck; the satellite emits no telemetry of its own.
