# multicloud

Provider-neutral cloud-runtime drift classifier and candidate builder shared by
AWS, GCP, and Azure (issues #1997, #1998).

## Why this package exists

The AWS drift path in `../cloudruntime` already classifies an observed cloud
resource against Terraform state and config, but it keys on a provider-specific
ARN string. GCP and Azure need the same four findings — orphaned, unmanaged,
ambiguous, unknown — without duplicating the join three times.

This package keeps **one** drift path by reusing `cloudruntime.Classify` (the
structural join is provider-independent) and re-keying every candidate on the
canonical `cloud_resource_uid` resolved by
`../../cloudinventory.ResolveProviderIdentity`. AWS keeps its own pack and
package unchanged; this package adds the provider-neutral path beside it.

## What it does

- `Classify(cloud, state, config)` delegates to the shared structural join,
  which also carries the shared value-drift comparison (#5453): once cloud,
  state, and config converge, `cloudruntime.Classify` compares the same
  allowlisted attributes (`cloudruntime.ValueAttributeAllowlistFor`) this
  package's `Row.Cloud`/`Row.State` carry, so an AWS resource routed through
  the provider-neutral path gets `image_version_drift` findings identical to
  the AWS-specific path. GCP/Azure resources carry no allowlist entries
  today (their resource type strings never match the AWS-only allowlist
  keys), so this is currently AWS-only in practice, not by a provider check.
- `Row.EffectiveFindingKind()` lets the reducer override the structural join
  with a stronger deterministic signal: `ambiguous` (conflicting ownership) or
  `unknown` (coverage gap). An override of ambiguous or unknown wins even when
  the bare layers converge, so conflicting or unproven ownership is never
  presented as managed.
- `Row.ResolveUID()` resolves the canonical join key. A blank, malformed, or
  unsupported identity returns `false`; the reducer counts it as unresolved and
  never fabricates a finding.
- `BuildCandidates(rows, scopeID)` emits one uid-keyed `model.Candidate` per
  finding, carrying provider, raw identity, observed/state/config evidence, raw
  tags, management-status atoms, and -- for an `image_version_drift` finding --
  the same bounded `declared_<attr>`/`observed_<attr>` value-pair evidence
  `cloudruntime.appendValueDriftEvidence` emits, via this package's own
  `appendValueDriftEvidence` (candidate.go), for
  `rules.MultiCloudRuntimeDriftRulePack()`.

## Boundaries

- No Postgres or graph reads/writes. Loaders and publication live in the
  reducer.
- No environment or service ownership inference from tag names. Tags are raw
  evidence for a later normalization rule.
- No provider-specific identity parsing here beyond what `cloudinventory`
  already owns.

## Taxonomy mapping

Findings map to the provider-neutral source-state taxonomy through the AWS
management-status mapping in
`../../../query/replatforming_source_state.go`: `cloud_only` and
`terraform_state_only` are `derived`, `ambiguous_management` is `ambiguous`, and
`unknown_management` is `unknown`. See
`docs/public/reference/replatforming-source-state-taxonomy.md`.
