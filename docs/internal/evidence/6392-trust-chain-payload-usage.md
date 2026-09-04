# #6392 — Trust-chain anchor decodes under the payload-usage gate

Eight qualified SDK decode calls in
`go/internal/storage/postgres/secrets_iam_trust_chain_anchor_decode.go` read
payload fields the payload-usage manifest could not see: those decoders have no
seam declaration in any `factschema_decode*.go`, so `decodeFuncs` never
contained their names and every field read off their results silently vanished.
Deleting a field one of those handlers reads from its fact kind's schema stayed
green — a false-green gate.

The fix (issue option 2) adds the eight missing seam wrappers in
`go/internal/reducer/schemadecode/factschema_decode_secretsiam.go` (same bare
names as the SDK functions, so the qualified-call binding attributes the
loader's live-site reads to them) plus their `factKindSchemaFile` mappings in
`go/internal/payloadusage/schema.go`. The wrappers are attribution identity
only — the loader keeps calling the SDK directly, and each wrapper calls the
same SDK function and returns the same SDK struct type, so the manifest's
struct-field join is identical and wrapper/live drift is nearly impossible by
construction (an SDK rename breaks compilation of both; a wrapper deletion
trips the 133-kind count test). It also scopes decode bindings per
switch/select branch (`go/internal/payloadusage/usage_scoped.go`), so sibling
case clauses reusing one identifier no longer collapse onto the last binding;
reads resolve against the nearest enclosing region's bindings, each recorded
exactly once. Manifest grows 125 → 133 kinds.

Matched pair: with `role_arn` removed from the `aws_iam_trust_policy` schema,
`TestPayloadUsageManifest` fails naming handler
`secrets_iam_trust_chain_anchor_decode.go` reading `RoleARN` on
`FactKindAWSIAMTrustPolicy` via `DecodeAWSIAMTrustPolicy`; with the schema
intact the gate is green. `TestScanDecodeUsageSeesTrustChainAnchorDecodes`
pins attribution for every field all eight live sites read (including a nested
binding-less branch case and a nested-binding case reading mid-chain state
through the chained overlay), using the real seams glob.

- No-Regression Evidence: no new runtime call sites — the eight wrappers are
  never called outside the manifest's seam scan, and the scanner itself runs
  only at gate/test time. No Cypher, SQL, queue, worker, or batching surface
  changes; nothing here executes per-fact hot. `go test
  ./internal/payloadusage/ -count=1` green, `go vet` clean on both touched
  packages, and the role_arn removal probe above trips exactly the violation
  the gate exists to catch. No benchmark applies.
- No-Observability-Change: no metric, span, log, status, or telemetry surface
  moves. The manifest grows by eight kinds (125 → 133, asserted by count
  tests), which is the fix itself, not an operator-visible change.
