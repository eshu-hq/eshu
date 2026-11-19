# Wave-6 D4.4 live proof evidence (Refs eshu-hq/eshu#9669)

Proven 2026-11-19 UTC on `drive/9669-wave6-server` against the freshly
built `eshu-core-api` from this lane and the satellite at pin `08ebde4`
(`Merge pull request #155`, also upstream `eshu-api` HEAD that day).
The whole proof reruns from a clean checkout with one command (below);
this file records the observed outputs, not a substitute for rerunning.

## Rerun

Prereqs: docker (images below), go (repo-pinned 1.26.11 first on `PATH`,
`GOTMPDIR` set), `curl`, `openssl`, `rg`, and an `eshu-api` checkout at
the pin (default `../eshu-api`; else `SATELLITE_SRC=`, else the script
clones `https://github.com/eshu-hq/eshu-api.git` and checks out the pin).

```bash
bash scripts/prove-core-api-live.sh
ESHU_D44_LIVE=1 go test ./cmd/core-api/ -run TestCoreAPILiveD44Proof -count=1 -v   # from go/
```

Backend images (pinned, `docker inspect` on the demo containers):

- `ghcr.io/eshu-hq/ci-postgres-bookworm@sha256:afc7e2d441324c0388fa80c3d24f733b4194a4eb7f47dd8ee2b08eb1a24a647c`
  (`wave6-pg`, `eshu:eshu-demo-pw`, db `eshu`, host port `15433`)
- `ghcr.io/eshu-hq/ci-neo4j-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (`wave6-neo4j`, `neo4j/wave6-demo-pw`, bolt host port `17687`)

Ports: core `127.0.0.1:18091`, satellite `127.0.0.1:18092` (both
overridable via `CORE_ADDR`/`SAT_ADDR`). Schema was intact on the demo
containers (PG tables present, Neo4j HTTP 200), so the script reused
them; on missing containers it recreates them and applies the schema
through `eshu-bootstrap-data-plane` (ApplyBootstrap + migrations).
Per-run throwaway credentials only (random shared keys, random DEK);
the proof exercises no provider secrets.

## Observed run (2026-11-19, `SATELLITE_SRC=<scratch clone @ 08ebde4>`)

```text
D44-PASS leg 0: backends up: wave6-pg + wave6-neo4j
D44-PASS leg 0: binaries built: core + satellite@08ebde4
D44-PASS leg 1: /readyz 200 with both backends up
D44-PASS leg 2: /readyz 503 (no Retry-After) with Postgres down
D44-PASS leg 2b: /readyz 200 with Postgres restored
D44-PASS leg 3: /readyz 503 (no Retry-After) with Neo4j down
D44-PASS leg 3b: /readyz 200 with Neo4j restored
D44-PASS leg 4: surface-inventory 200 byte-identical sat=200 core=200
D44-PASS leg 4: slash-twin 404 byte-identical sat=404 core=404
D44-PASS leg 4: freshness 400 byte-identical sat=400 core=400
D44-PASS leg 4: derive POST 200 byte-identical sat=200 core=200
D44-PASS leg 5: satellite /readyz 503 (no Retry-After) vs dead core
D44-PASS leg 5b: core + satellite /readyz 200 restored
D44-PASS leg 6: latency pins hold (core probe << 30 s relay bound; satellite dispatch == readclient)
D44-DONE all legs pass
```

503 bodies observed (permanent, no `Retry-After` header on any of them):

- PG down, core `/readyz` 503:
  `{"service":"eshu-core-api","probe":"readyz","status":"error",
  "error":"failed to connect to `user=eshu database=eshu`:
  127.0.0.1:15433 (127.0.0.1): dial error: dial tcp 127.0.0.1:15433:
  connect: connection refused"}`
- Neo4j down, core `/readyz` 503:
  `{"service":"eshu-core-api","probe":"readyz","status":"error",
  "error":"ConnectivityError: dial tcp 127.0.0.1:17687: connect:
  connection refused"}`
- Dead core, satellite `/readyz` 503:
  `{"service":"eshu-api","probe":"readyz","status":"unavailable",
  "details":{"reason":"connection_refused"}}`

Relay legs ran on the relay lane (`"via":"relay","outcome":"core_ok"`
in the satellite log for each of the four comparisons). The caller
presents the core shared key, which is foreign to the satellite gate
(the satellite runs under its own gate key), so the wave gates relay
explicitly and the core judges; the same request bytes hit the core
directly for the byte comparison (`cmp` on bodies plus status codes).

## Satellite suite: no live-core mode (relay substitution stands)

Attempted invocation: the satellite suite at the pin has no live-core
entrypoint. `api/*_test.go` and `internal/readclient/*_test.go` read no
environment (`rg -n 'os\.Getenv|os\.LookupEnv'` over both trees returns
nothing) and consume no `--core-base-url`: every dispatch test drives
an in-process `stubCore` over `httptest.NewServer` (see
`api/dispatch_test.go`). `make test` is `go test ./...` with those
stubs. So the D4 "satellite `make ci` green with the new binary"
criterion is covered here by the byte-identical relay substitution
(leg 4, real satellite binary vs real core binary over HTTP), which is
committed and rerunnable via the script above — not prose.

## Latency pins

- Core side, committed here:
  `go/internal/query/serve_latency_bound_test.go`
  (`TestCoreReadinessProbeStaysFarBelowRelayBound`: 2 s probe <
  30 s relay bound, with an order-of-magnitude margin).
- Satellite side, at the pin (run in a read-only scratch clone):
  `go test ./api/ -run TestDispatch_TimeoutMatchesReadContract -count=1`
  → `PASS` (`dispatchTimeout == readclient.DefaultTimeout == 30s`).

## F2 (ride-along, same commit)

`wrapCoreAPIMiddleware` in `go/cmd/core-api/main_wiring.go` now nests
the deprecation headers OUTSIDE the v1 rewrite, matching its comment:
`/api/v1/` responses carry no `Deprecation`/`Sunset`, direct `/api/v0/`
responses keep both. Pinned by
`TestCoreOuterMiddlewareV1CarriesNoDeprecation` (red before the
reorder: `/api/v1/ Sunset = "test-sunset"`; green after).
