# Fleet Validation Activation — Exact Commands (DRAFT)

Companion to the
[fleet validation activation record](1314-secrets-iam-graph-fleet-validation-activation-record.md)
for the fleet validation deployment on the EKS cluster. Gate: ADR #1314.
Issues [#6228](https://github.com/eshu-hq/eshu/issues/6228) /
[#2430](https://github.com/eshu-hq/eshu/issues/2430).

**Enactment package only. Every command below is validated by render or by
offline gates — nothing here was applied to any cluster.** `FILL` tokens mark
values only the owner can supply at enactment. Do not paste secret values into
tickets, records, or shell history: pass credentials by environment reference
(the target secret store), never by literal.

Legend: `FILL:<what>` is supplied and signed at enactment. Everything else is
literal and copy-paste-ready.

## 0. Preconditions (no cluster contact)

- Tools: `helm` (>= 3.19 proven at package time), `kubectl` (target context),
  `go` (proof gates), `cypher-shell` or the Neo4j browser (readback).
- Work from the exact deployed commit; record its SHA in the §4 table.
- `kubectl` was not present on the packaging host, so live-cluster commands
  below are render/shape-validated, not executed. The first `kubectl` run at
  enactment is `kubectl config current-context` — abort on any other context.

## 1. Enable (target overlay only — defaults stay OFF)

Append exactly this to the target overlay values file (`FILL:<target overlay
values path>` — the overlay for the fleet validation deployment; not the chart
default, not the shared base values):

```yaml
resolutionEngine:
  enabled: true
  env:
    ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED: "true"
```

Render-proof before sync (proves the override shape lands on the reducer
Deployment; the chart path below is the deploy checkout, used read-only):

```bash
CH=<path to the deploy checkout>/helm/eshu
BASE=<path to the deploy checkout>/gitops/argocd/base/values.yaml
helm template eshu "$CH" -f "$BASE" \
  -f FILL:<target overlay values path> \
  --show-only templates/deployment-resolution-engine.yaml \
| grep -n -A1 'ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED'
```

Expected: one `- name:` line plus `value: "true"`. Any other diff on the
rendered Deployment is out of scope — stop and reconcile.

Sync and roll (ArgoCD is the normal upgrade path for this target):

```bash
argocd app sync FILL:<target ArgoCD app name>
kubectl -n FILL:<target namespace> rollout status deploy \
  -l app.kubernetes.io/component=resolution-engine --timeout=600s
```

Confirm the flag-on line (the operator's flag-on confirmation):

```bash
kubectl -n FILL:<target namespace> logs deploy \
  -l app.kubernetes.io/component=resolution-engine --tail=200 \
| grep 'secrets/IAM graph projection ENABLED'
```

## 2. §4 proof commands

Run the live writer conformance against the **target Neo4j backend** from the
exact deployed commit. `PASS` is required; a skip is not evidence.

```bash
cd go
ESHU_SECRETS_IAM_GRAPH_LIVE=1 \
ESHU_GRAPH_BACKEND=neo4j \
ESHU_NEO4J_URI='FILL:<target Neo4j Bolt endpoint>' \
ESHU_NEO4J_USERNAME='FILL:<target Neo4j user>' \
ESHU_NEO4J_PASSWORD='FILL:<from the target secret store, never pasted>' \
ESHU_NEO4J_DATABASE='FILL:<target database; omit for server default>' \
go test ./internal/storage/cypher/cloud/aws/iam \
  -run '^TestSecretsIAMGraphWriterLiveConformance$' -count=1 -v
```

The package must be the one that contains the test (`iam` package,
`secrets_graph_live_test.go`). A `-run` pattern matching nothing exits 0 with
`[no tests to run]` — that output is not evidence either.

Re-run the §7 redaction-allowlist gate on the exact deployed commit:

```bash
cd go
go test ./internal/reducer/... ./internal/storage/cypher ./cmd/reducer \
  -run 'SecretsIAMGraph|SecretsIAM|TestExtractRowsCarryNoForbiddenProperties' \
  -count=1
```

Live readback after a generation has projected (scope is the enactment
`scope_id` from record §2; evidence source is fixed):

```cypher
MATCH (n)
WHERE n.scope_id = 'FILL:<enactment scope_id>'
  AND n.evidence_source = 'reducer/secrets-iam-graph'
  AND (n:SecretsIAMServiceAccount OR n:SecretsIAMVaultAuthRole
    OR n:SecretsIAMVaultPolicy OR n:SecretsIAMSecretMetadataPath)
RETURN labels(n)[0] AS family, count(n) AS nodes
ORDER BY family;
```

```cypher
MATCH ()-[r]->()
WHERE r.scope_id = 'FILL:<enactment scope_id>'
  AND r.evidence_source = 'reducer/secrets-iam-graph'
  AND type(r) STARTS WITH 'SECRETS_IAM_'
RETURN type(r) AS edge, count(r) AS edges
ORDER BY edge;
```

Sensitive-value spot check (report `suspicious_values=0`; never print values):

```cypher
MATCH (n)
WHERE n.scope_id = 'FILL:<enactment scope_id>'
  AND n.evidence_source = 'reducer/secrets-iam-graph'
  AND (n:SecretsIAMServiceAccount OR n:SecretsIAMVaultAuthRole
    OR n:SecretsIAMVaultPolicy OR n:SecretsIAMSecretMetadataPath)
WITH n, [k IN keys(n) WHERE
  toString(n[k]) STARTS WITH 'arn:aws'
  OR toString(n[k]) CONTAINS '/secret/'
  OR toString(n[k]) STARTS WITH 'vault:'] AS suspicious
WHERE size(suspicious) > 0
RETURN count(n) AS suspicious_values;
```

Expect `suspicious_values = 0`. Then run the same shape over `SECRETS_IAM_*`
relationships (report `suspicious_values=0`; never print values):

```cypher
MATCH ()-[r]->()
WHERE r.scope_id = 'FILL:<enactment scope_id>'
  AND r.evidence_source = 'reducer/secrets-iam-graph'
  AND type(r) STARTS WITH 'SECRETS_IAM_'
WITH r, [k IN keys(r) WHERE
  toString(r[k]) STARTS WITH 'arn:aws'
  OR toString(r[k]) CONTAINS '/secret/'
  OR toString(r[k]) STARTS WITH 'vault:'] AS suspicious
WHERE size(suspicious) > 0
RETURN count(r) AS suspicious_values;
```

## 3. Rollback

To disable (runbook §6):

```bash
# Remove the §1 snippet from FILL:<target overlay values path>
# (or set the value to "false"), then:
argocd app sync FILL:<target ArgoCD app name>
kubectl -n FILL:<target namespace> rollout status deploy \
  -l app.kubernetes.io/component=resolution-engine --timeout=600s
kubectl -n FILL:<target namespace> logs deploy \
  -l app.kubernetes.io/component=resolution-engine --tail=200 \
  > /tmp/secretsiam-rollback-logs.txt; echo "kubectl-exit=$?"
grep -c 'secrets/IAM graph projection ENABLED' /tmp/secretsiam-rollback-logs.txt; echo "warn-grep-exit=$?"
```

Expected: `kubectl-exit=0` (the observation command itself succeeded), then
`0` with `warn-grep-exit=1` (line absent — no further live `SecretsIAM*`
writes or retracts occur). The split exists so a kubectl failure cannot mimic
success: if `kubectl-exit` is nonzero, stop — a `0`/`1` grep signature on an
empty capture proves nothing. Failure signature: `kubectl-exit≠0`.

What rollback leaves behind: already-written `SecretsIAM*` nodes/edges
**remain** — disabling stops new writes, it does not retract. If the §2
decision requires removal, run a scoped retract through the writer over the
enactment `scope_id` + `evidence_source=reducer/secrets-iam-graph`, then
re-verify reducer-owned label counts are 0 while retained endpoints survive.
The four `SecretsIAM*` uid constraints and scope indexes also remain; leaving
them is harmless and lets a later re-enable skip re-bootstrap.

## 4. Render/dry-run evidence captured at package time

Host: packaging worktree at base `3e07d26b1f`; helm `v3.19.0+g3d8990f`;
chart + values read from the deploy checkout (unmodified). `kubectl` absent
on this host — live-cluster commands above are shape-validated only.

- E1 (override shape renders, exit 0): base values plus a temp file holding
  exactly the §1 snippet rendered the reducer Deployment with
  `ESHU_REDUCER_SECRETS_IAM_GRAPH_PROJECTION_ENABLED` / `value: "true"`
  (lines 94-95 of the flagged render; flagged-render-exit=0,
  flagged-grep-exit=0).
- E2 (defaults OFF, exits captured directly): the same render without the
  override contains zero occurrences (base-grep-exit=1); the deploy-checkout EKS
  overlay (`gitops/argocd/overlays/aws/values.yaml` over base) also renders
  zero occurrences (aws-render-exit=0, aws-grep-exit=1); a repo-wide search
  for the flag across the deploy chart and gitops tree finds nothing
  (deploy-flag-grep-exit=1).
- E3 (redaction gate current on this base, exit captured directly): the
  runbook §5b command passes with gate-exit=0 and 95 `ok` packages
  (remainder: packages with no test files).
- E4 (repo/chart/operator defaults untouched): this package's diff adds only
  these two record documents — verified by `git status` at commit time; no
  values, template, wiring, or operator file is modified.

## 5. FILL fields needing owner signature (handoff checklist)

Target deployment identifier + overlay values path; target ArgoCD app name;
target namespace + kubectl context; Neo4j Bolt endpoint, user, password
reference, database; enactment `scope_id`; rollback owner; observation window;
§1 approver name / role / date; all §4 evidence pastes.
