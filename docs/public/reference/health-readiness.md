# Health And Readiness Probes

Eshu's long-running services expose two distinct Kubernetes probe endpoints on
the admin surface. They answer different questions, and conflating them is the
classic cause of crash loops and traffic black-holing.

| Endpoint   | Probe kind | Question it answers              | Dependency-aware |
| ---------- | ---------- | -------------------------------- | ---------------- |
| `/healthz` | liveness   | Is the process alive?            | No               |
| `/readyz`  | readiness  | Can it serve dependent traffic?  | Yes              |

## Liveness — `/healthz`

`/healthz` is intentionally cheap and dependency-free. It returns `200 OK` as
long as the process is running and the HTTP server is accepting connections. It
never checks Postgres or the graph backend.

This is deliberate: if liveness checked dependencies, a transient Postgres or
graph outage would restart every replica simultaneously, turning a recoverable
dependency blip into a self-inflicted outage. Liveness must only fail when the
process itself is wedged and a restart is the correct remedy.

## Readiness — `/readyz`

`/readyz` reflects whether the service can actually serve dependent traffic. It
runs a set of bounded dependency probes and returns:

- `200 OK` when every probe passes.
- `503 Service Unavailable` with a cause body when any probe fails.

### Probes

The API and MCP server register these probes:

- **`status_snapshot`** — reads the storage-backed status snapshot, exercising
  Postgres connectivity *and* schema presence. A failure here typically means
  the database is reachable but the schema is not applied.
- **`postgres`** — a bounded `PingContext`. A failure here (especially a
  deadline) distinguishes an unreachable database or pool exhaustion from a
  schema fault.
- **`graph`** — a bounded Bolt `VerifyConnectivity` against the graph backend.
  The same Bolt driver fronts both Neo4j and NornicDB, so one probe covers both.
  When the graph is disabled (the local lightweight profile), the probe is
  omitted and readiness is not gated on an unused dependency.

Each probe runs concurrently under its own bounded timeout (default 2s), so a
single slow dependency cannot block the probe handler. The `503` body aggregates
every failing dependency in a deterministic order, for example:

```
service=eshu-api probe=readyz status=error error=graph: ...; postgres: ...
```

### Cause taxonomy

| Cause prefix      | Operator meaning                                    |
| ----------------- | --------------------------------------------------- |
| `postgres: ...`   | Database unreachable or connection pool exhausted   |
| `graph: ...`      | Graph backend (Bolt) unreachable                    |
| `status_snapshot: ...` | Schema not applied, or status store query failing |

## Anti-flap (debounce)

`/readyz` always reports the *true current* state of its dependencies. It does
not smooth or cache results, because an endpoint that lies about current state
is harder to reason about than one that is honest.

Debounce is the Kubernetes probe's job, configured via
`readinessProbe.failureThreshold`. The Eshu Helm chart sets:

```yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: http
  initialDelaySeconds: 10
  periodSeconds: 15
  timeoutSeconds: 5
  failureThreshold: 3
  successThreshold: 1
```

With `failureThreshold: 3` and `periodSeconds: 15`, a replica is only pulled
from the Service endpoints after ~45s of sustained failure, so a brief
dependency hiccup between two probes does not flap the pod out of rotation. A
single successful probe (`successThreshold: 1`) restores it.

Liveness uses a separate `failureThreshold: 3` against `/healthz` so a wedged
process is still restarted, independent of dependency state.

## Why this matters

Before dependency-aware readiness, `/readyz` and the API liveness/readiness
probes were effectively always-200: Kubernetes could not distinguish an
alive-but-broken pod (graph down, schema missing) from a ready one, and routed
traffic to replicas that could only return errors. The probes above let the
control plane make the correct routing and restart decisions.

No-Regression Evidence: probes execute only on `/readyz` hits at the Kubernetes
probe cadence, never on the query or graph-write hot paths. Each probe is a
single bounded connection check on the existing shared Postgres pool and Bolt
driver — no new pool, worker, queue, or goroutine pool is introduced. Verified
by `go test ./internal/runtime ./cmd/api ./cmd/mcp-server -count=1`.

Observability Evidence: `/readyz` `503` responses name the failing dependency
and its error, so an operator can triage graph-down vs. Postgres pool-exhaustion
vs. schema-not-applied from the probe body alone. Liveness stays dependency-free
so dependency outages never trigger restarts.
