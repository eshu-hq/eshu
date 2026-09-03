# secretsiam

Resolves who can reach which secret, and how, by joining AWS IAM, Kubernetes,
GCP workload-identity and Vault source facts into reducer-owned read models,
then projecting the exact ones into the canonical graph.

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns two reducer domains and the pipeline behind
them, and nothing else in the reducer depends on its internals.

## What it owns

| piece | file | what it does |
|---|---|---|
| `SecretsIAMTrustChainHandler` | `secrets_iam_trust_chain.go` | the reducer handler for `secrets_iam_trust_chain` |
| `BuildSecretsIAMTrustChainReadModels` | `secrets_iam_trust_chain_build.go` | joins the evidence packet into the four read models |
| identity resolution | `secrets_iam_trust_chain_resolve.go` | matches service accounts to IAM roles, Vault roles and GCP identities |
| observation builders | `secrets_iam_trust_chain_observations.go` | turns resolved state into posture observations and gaps |
| IAM role identity | `secrets_iam_trust_chain_iam_role.go` | derives the AWS IAM role `CloudResource` uid the graph joins on |
| GCP workload identity | `secrets_iam_trust_chain_gcp.go` | the GCP service-account and binding lane |
| external trust posture | `secrets_iam_external_trust.go` | flags `sts:AssumeRole` trust without `sts:ExternalId` |
| `TrustChainDomainDefinition` | `secrets_iam_trust_chain_domain.go` | the additive domain definition the root registers |
| `PostgresSecretsIAMTrustChainWriter` | `secrets_iam_trust_chain_writer.go` | persists the four derived fact kinds |
| `SecretsIAMGraphProjectionHandler` | `secrets_iam_graph_projection.go` | the reducer handler for `secrets_iam_graph_projection` |
| `ExtractSecretsIAMGraphRows` | `secrets_iam_graph_projection_extract.go` | turns exact read models into node and edge rows |
| endpoint readiness gate | `secrets_iam_graph_projection_readiness.go` | defers the write until cross-scope endpoints commit |

## Exported surface

| symbol | what it is |
|---|---|
| `SecretsIAMTrustChainState` + 6 state constants | how completely one chain resolved |
| `SecretsIAMTrustChainReadModels` | the four read models one intent produces |
| `SecretsIAMIdentityTrustChain` | one resolved workload-to-identity chain |
| `SecretsIAMPrivilegePostureObservation` | one privilege-posture finding |
| `SecretsIAMSecretAccessPath` | one resolved identity-to-secret path |
| `SecretsIAMPostureGap` | why a chain or path could not be completed |
| `SecretsIAMTrustChainLoadStats` | what the bounded evidence load actually read |
| `SecretsIAMTrustChainEvidenceLoader` | loads the bounded source-fact packet |
| `SecretsIAMTrustChainWrite` / `WriteResult` / `Writer` | the durable publication contract |
| `PostgresSecretsIAMTrustChainWriter` | the Postgres implementation of that writer |
| `SecretsIAMTrustChainHandler` | the trust-chain reducer handler |
| `BuildSecretsIAMTrustChainReadModels` | the pure read-model builder |
| `TrustChainDomainDefinition` | the trust-chain additive domain definition |
| `SecretsIAMGraphWriter` | the four node and five edge write methods, plus scope retract |
| `SecretsIAMGraphProjectionHandler` | the graph projection reducer handler |
| `GraphProjectionDomainDefinition` | the projection additive domain definition |
| `ExtractSecretsIAMGraphRows` | the pure extractor from facts to graph rows |
| `SecretsIAMGraphRows` / `SecretsIAMGraphTally` | the extracted rows and what was skipped |
| `SecretsIAMGraphEvidenceSource` | the evidence source stamped on projected rows |
| `SecretsIAMEndpointNotReadyFailureClass` | the retryable failure class the readiness gate returns |

## Partial is a first-class answer

`SecretsIAMTrustChainState` has six values, not two. A chain that resolved only
halfway is `partial`, one whose evidence is a generation behind is `stale`, one
the collector could not read is `permission_hidden`, and one whose identity
layer this family does not model is `unsupported`. Collapsing any of them into
`unresolved` would report a missing answer where the honest answer is "the
evidence says something, but not enough."

Only `exact` rows are projected into the graph. Everything else stays a read
model with a `SecretsIAMPostureGap` naming what was missing, which is what lets
an operator tell "no such path exists" apart from "we could not see far enough
to tell."

## Two gates keep the graph honest

**Registration.** The reducer root registers the projection domain only when
both a fact loader and a non-nil `SecretsIAMGraphWriter` are wired, so live
graph writes stay off until a deployment opts in (ADR #1314 §14). The root's
`secrets_iam_graph_projection_wiring_test.go` proves both directions of that
gate; it lives at the root because it exercises root registration, not this
package.

**Cross-scope endpoint presence.** Before writing, the handler asks
`gpphase.EndpointPresenceLookup` whether every `KubernetesWorkload` and
`CloudResource` uid its edges reference is already committed. If any is missing
it returns a retryable error classified `SecretsIAMEndpointNotReadyFailureClass`
so the queue re-runs the intent once those endpoints commit, instead of
committing a projection whose edges were silently dropped (issue #1380). A nil
lookup disables the gate.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factdecode`, `reducer/factload`, `reducer/factwrite`,
`reducer/gpphase`, `reducer/payloadcore`, `reducer/schemadecode`,
`internal/facts`, `internal/graph/edgetype`, `internal/telemetry`,
`internal/truth` and the factschema SDK, and it never imports the parent
`internal/reducer` package. The dependency runs the other way: the root keeps
compatibility aliases in `secrets_iam_compat.go` so its own callers, plus
`cmd/reducer`, `internal/storage/postgres` and `internal/replay/costcounting`,
compile unchanged.

Three symbols the root used to own moved down to a shared tier so both sides can
name them:

- `payloadcore.CloudResourceUID` — the canonical `CloudResource` node identity.
  The AWS and Azure resource materializers and this family's IAM-role resolver
  must all produce the same uid, or the `SECRETS_IAM_ASSUMES_IAM_ROLE` edge
  points at a node nothing else writes.
- `factwrite.SingleInsertQuery` — the shared single-row fact upsert this
  family's writer issues.
- `gpphase.EndpointPresenceLookup` — the read half of the endpoint-presence
  primitive. The write half (`EndpointPresenceRow`, `EndpointPresenceWriter`)
  stays at the root with the materializers that publish presence.

## Telemetry

| handler | instrument | labels |
|---|---|---|
| trust chain | `eshu_dp_secrets_iam_reducer_trust_chains_total` | `result`, `confidence` |
| trust chain | `eshu_dp_secrets_iam_posture_observations_total` | `risk_type`, `severity` |
| graph projection | `eshu_dp_secrets_iam_graph_nodes_written_total` | `node_type` |
| graph projection | `eshu_dp_secrets_iam_graph_edges_written_total` | `edge_type` |
| graph projection | `eshu_dp_secrets_iam_graph_skipped_total` | `skip_reason` |

The graph projection handler also wraps its work in the
`reducer.secrets_iam_graph_projection` span. Facts rejected for a malformed
payload increment the shared `eshu_dp_reducer_input_invalid_facts_total`
counter, and the reducer executions running either handler stay covered by
`eshu_dp_reducer_executions_total` and `eshu_dp_reducer_run_duration_seconds`.

At 3 AM, read the skipped counter before the written ones. A projection that
writes nothing because everything was skipped and a projection that writes
nothing because there was nothing to do look identical in the written counters
alone; `skip_reason` is what separates them.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk inside the moved production files is package-clause and
import requalification: symbols the reducer root used to supply as one-line
forwarders are now imported from the leaf that already owned them
(`payloadcore` for the payload and string helpers, `contract` for the domain
and result vocabulary, `factdecode`/`factload`/`factwrite`/`schemadecode`/
`gpphase` for the rest). Three symbols were hoisted rather than requalified —
`payloadcore.CloudResourceUID`, `factwrite.SingleInsertQuery` and
`gpphase.EndpointPresenceLookup` — each moved whole, with a root forwarder or
alias left behind, so no root call site changed. The two domain-definition
constructors were exported (`TrustChainDomainDefinition`,
`GraphProjectionDomainDefinition`) and the root now calls them through
forwarders under their former unexported names. On the test side,
`secrets_iam_graph_projection_wiring_test.go` moved back to the root because it
exercises root registration, and gained its own stub loader and writer, and
`secrets_iam_trust_chain_writer_test.go` gained a local copy of the root's exec
double — Go test files cannot share unexported symbols across a package
boundary. A Go import change adds no indirection at runtime. Measured on this
branch after the final edit: `go build ./...` and `go vet ./...` both exit 0,
and `go test ./internal/reducer/... -count=1` passes, including this package.
Binary output was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. The five counters above, the span, and the executions that wrap them
are the same before and after the move; only the file paths the
telemetry-coverage rows point at changed.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: hoist it to a shared-core
  tier (`payloadcore` for generic helpers and identity, `contract` for
  vocabulary, `gpphase` for readiness shapes) and leave a root forwarder,
  rather than reaching upward.
- **`SecretsIAMEndpointNotReadyFailureClass` is a storage contract, not a Go
  identifier.** `internal/storage/postgres`' readiness claim gate matches the
  literal `secrets_iam_endpoint_not_ready` when it decides to re-enqueue rather
  than dead-letter. Changing the string without changing that query turns every
  deferral into a retry-budget burn.
- **The IAM-role uid must match the AWS materializer's.** It is
  `payloadcore.CloudResourceUID(account_id, region, "aws_iam_role", role_arn)`,
  a hash, not a readable ARN. Any drift between this family and the AWS
  resource materializer produces an edge to a node that never gets written, and
  nothing fails loudly.
- **A nil `PresenceLookup` is not "ready", it is "ungated".** The projection
  then writes whatever resolved, and the writer no-ops edges to uncommitted
  endpoints. That is a deliberate opt-out, not a default to rely on.
- **Blank join keys are rejected, not defaulted.** A whitespace-only service
  account join key, a missing Vault role join key or a missing GCP email digest
  quarantines that fact through `factdecode` rather than producing a chain
  keyed on the empty string, which would join everything to everything.

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Secrets/IAM posture collector contract](../../../../docs/public/reference/secrets-iam-posture-collector-contract.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
