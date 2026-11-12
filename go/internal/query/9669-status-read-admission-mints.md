# Evidence: #9669 Status read-admission mints carry no runtime change

Refs #9669, Refs #10491. The two v0 pins minted in this change —
`StatusGovernanceReadAdmission` (the `Summary` read behind
`GET /api/v0/status/governance`) and `StatusHostedReadinessReadAdmission`
(the `Run`/`RunSingle` reads behind `GET /api/v0/status/hosted-readiness`)
— are declaration-only interfaces plus frozen method-set, seal, and
fail-closed wire-shape tests. The seams stay unwired: no handler, route, or
caller depends on either pin, so no request path, query plan, worker claim,
lease, batch size, or Cypher statement changed. The `MATCH
(r:Repository) RETURN count(r) as count` text named in the pin doc is the
pre-existing leg the pin covers, quoted to pin the scope — not a new or
altered statement.

No-Regression Evidence: full `go/internal/query/...` tree green plus the
`cmd/api` cutover ownership tests green at the pinned HEAD, with zero
changes to any handler, wiring, or store file. Baseline is origin/main
`290076840d`; after measurement is `go test ./internal/query/... -count=1`
(all packages ok) and `go test ./cmd/api/ -run 'TestCutoverOwnership'
-count=1` (both tests pass) at the pinned HEAD. No graph backend, worker,
queue, or batching knob is touched, so there is no before/after latency
delta to measure: the change adds no executable path. Input shape is not
applicable (no new query input); row counts are unchanged (no statement
touched).

No-Observability-Change: no span, metric, log, or status surface is added
or altered. The pins declare no telemetry; the wire-shape probes assert
fail-closed errors through the existing handler error paths, which are
unchanged.
