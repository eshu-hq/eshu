# AGENTS — multicloud

## Read first

1. `doc.go` — package contract.
2. `classify.go` — `Row`, `Classify`, `EffectiveFindingKind`, `ResolveUID`.
3. `candidate.go` — uid-keyed candidate and evidence construction.
4. `../cloudruntime/` — the AWS path this package mirrors but must not change.
5. `../../cloudinventory/identity.go` — the canonical `cloud_resource_uid`.
6. `../../rules/multi_cloud_runtime_drift_rules.go` — rule-pack declaration.
7. `docs/public/reference/multi-cloud-collector-contract.md` — reducer contract.

## Invariants

- Every candidate is keyed on canonical `cloud_resource_uid`, never the raw
  provider identity. `CorrelationKey` is the uid.
- `BuildCandidates` skips rows whose identity does not resolve to a canonical
  uid; the reducer counts those as unresolved. Never fabricate a uid here.
- An explicit `ambiguous` or `unknown` `FindingKind` override wins over the
  structural join so conflicting or unproven ownership is never managed.
- The structural join (`Classify`) is delegated to `cloudruntime.Classify` —
  do not fork it. AWS and multi-cloud must agree on the join semantics.
- Config evidence is emitted only when a config layer is actually present.
  Never synthesize config to promote a resource to managed.
- Raw tags, raw identities, and uids stay in `EvidenceAtom`s only. Do not add
  them as metric labels.

## Common changes

- Add a finding kind: update `cloudruntime.FindingKind` (shared), this package's
  `managementStatusForFinding`, evidence builders, the reducer summary, tests,
  telemetry docs, and the taxonomy doc.
- Change candidate evidence shape: keep `EvidenceTypeCloudResourceUID` aligned
  with `rules.MultiCloudRuntimeDriftRulePack`'s required evidence.

## Anti-patterns

- Do not query Postgres or graph backends from this package.
- Do not duplicate the cloudruntime structural join; reuse it.
- Do not infer environment or service ownership from tag names here.

## What NOT to change without an ADR

- The canonical-uid-primary join contract.
- The ambiguous/unknown override precedence.
- The reuse of `cloudruntime.Classify` for the structural decision.
