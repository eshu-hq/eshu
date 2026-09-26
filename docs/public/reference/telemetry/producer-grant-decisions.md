# Producer-Grant Decision Telemetry

Core-issued producer grants (#6709) authorize a first-party producer to emit an
approved core-owned fact kind. Each allow or deny decision at the four grant
stages leaves one operator signal (#6726), so a grant allow, a grant deny, and
the reason are distinguishable from telemetry alone. This page is the reference
for that signal; the [Metrics](metrics.md) and [Logs](logs.md) pages list the
names, and [Component Producer Grants](../component-producer-grants.md) covers
issuing and revoking grants.

## Signals

- `eshu_dp_component_producer_grant_decisions_total` — counter, one increment
  per decision. Labels: `decision` (`allow`, `deny`), `stage` (`install`,
  `readback`, `activation`, `emission`), `reason` (`granted` for an allow;
  `no_matching_grant`, `revoked`, `expired`, `scope_mismatch`,
  `schema_not_covered`, or `grants_unreadable` for a deny), and `fact_kind`.
- Span event `component.producer_grant.decision` on the span active at the
  decision site (the `collector.claimed_run` span for the per-emission
  recheck), carrying the four labels plus `eshu.producer_grant.producer_id` and
  `eshu.producer_grant.version`.
- Structured log keys `producer_grant.producer_id`, `.version`, `.decision`,
  `.stage`, `.reason`, `.fact_kind`. Denies log at WARN. Allows log at INFO only
  at `install` and `activation`; per-emission and readback allows are counted
  and never logged.

## Stages

| Stage | Decision site | Emitted by a shipped binary today |
| --- | --- | --- |
| `install` | `eshu component install` (`Registry.Install`). | No. The CLI has no observer; the outcome is in command output and exit status. |
| `readback` | Registry readback, including the worker's activation selection and the coordinator's activation planning. | Yes, by `collector-component-extension` and by `workflow-coordinator`. The API and MCP server readbacks attach no observer. |
| `activation` | `eshu component enable` (`Registry.Enable`), extension-host construction (`NewSource`), and the coordinator's reload of a component it plans a claims-enabled instance for. | Extension-host construction and the coordinator: yes. `eshu component enable`: no (no observer in the CLI). |
| `emission` | The recheck against the live grants on every extension result. | Yes, by `collector-component-extension`. |

An `allow` means the grant covers the kind. It does not mean the install or
result was accepted: a later check (identity, fencing, payload schema, fact-kind
collision) can still fail it.

## Reasons

| Reason | Meaning |
| --- | --- |
| `granted` | An allow: a live grant covers the emission. |
| `no_matching_grant` | No grant names this producer, version, and kind. |
| `revoked` | Every matching grant is revoked. |
| `expired` | Every unrevoked matching grant is past its expiry. |
| `scope_mismatch` | A live grant exists but its scope is not a collector kind the manifest declares. |
| `schema_not_covered` | A live in-scope grant exists but does not cover the emitted schema version. |
| `grants_unreadable` | The grant set could not be read; the recheck denied fail-closed. |

## Cardinality and safety

`decision`, `stage`, and `reason` are closed sets, and `fact_kind` is bounded by
the core fact-kind registry (any non-registry value folds to `other`, an
out-of-set stage, decision, or reason to `unknown`). The producer id is
operator-configured and unbounded, so it is never a metric label; it appears on
the span event and log line only. No signal carries a credential, grant scope,
config value, or fact payload. Kinds that are not core-owned need no grant and
record nothing. Emission decisions are recorded once per distinct core kind per
result. A denial still fails the result closed exactly as before (terminal
`InvalidResult`, zero facts); the signal is additive.

## Triage

`sum by (reason) (rate(eshu_dp_component_producer_grant_decisions_total{decision="deny"}[5m]))`
names why emissions are being rejected. `revoked` and `expired` mean a grant
lifecycle action, `scope_mismatch` and `schema_not_covered` mean a grant that no
longer matches the manifest, and `grants_unreadable` means the registry could
not be read. The deny log line carries only the closed reason, never the read
error or a path; inspect the registry home directly to find the cause.

## Which processes emit it

| Process | Stages it records | Why |
| --- | --- | --- |
| `collector-component-extension` (worker) | `readback`, `activation`, `emission` | It selects the activation, builds the extension host, and rechecks every result. |
| `workflow-coordinator` | `readback`, `activation` | It reads the registry back at startup and reloads each component it plans a claims-enabled instance for. It has a full telemetry runtime and never emits results. |
| `eshu component ...` CLI | none | The CLI has no telemetry runtime. Its durable record is `eshu component grants` output plus registry state; the worker records the readback, activation, and emission stages once the component runs. |
| API and MCP server | none | They read the registry back on every request, so an observer would add one decision per request per family and no new information. |

The coordinator plans an activation only for a component that passes readback
and has a claims-enabled activation, so its `activation` decision is an allow
in practice. A revocation that lands between the coordinator's readback and its
reload records an `activation` deny and the coordinator then fails startup
closed, as it did before. A component the coordinator skips (any deny at
`readback`) records that deny and no `activation` decision.

The worker and the coordinator record the same `stage` values for the same
component activation, so the counter has no process label. Separate them with
the OTEL `service.name` resource attribute, exported as `service_name`:
`workflow-coordinator` for the coordinator and `collector-component-extension`
for the worker, for example
`sum by (service_name, stage) (increase(eshu_dp_component_producer_grant_decisions_total[1h]))`.
An unfiltered sum counts each activation once per process that observes it.
Decisions are made once per process start (readback and activation) or per
result (emission), never per poll.
