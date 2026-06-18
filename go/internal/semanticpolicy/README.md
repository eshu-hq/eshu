# internal/semanticpolicy

## Purpose

`internal/semanticpolicy` evaluates hosted semantic extraction source and
egress policy. It turns a configured provider profile plus source, scope, ACL,
allowlist, egress posture, budget, redaction, and retention settings into a
single reason-coded decision before any prompt or queue work exists.

## Ownership boundary

This package owns the pure policy contract for semantic extraction. It validates
operator policy JSON, semantic provider egress rules, source classes, source
selectors, scope selectors, limits, redaction posture, and retention posture.

It does not own provider profile parsing (`internal/semanticprofile`), prompt
safety/redaction execution, provider clients, queue persistence, observation
facts, or API/MCP readback routes. Those callers must treat a denied decision as
terminal for provider egress.

## Exported surface

See `doc.go` for the godoc-rendered contract.

- `EnvPolicyJSON` names `ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`.
- `Policy`, `Rule`, `Scope`, and `SourceSelector` model explicit source allowlists.
- `EgressPolicy` and `EgressProviderRule` model restricted or broad semantic
  provider egress posture.
- `Settings`, `Limits`, `Redaction`, and `Retention` carry the bounded runtime
  settings inherited by allowed semantic work.
- `Request` names the exact source decision input.
- `Decision` returns `Allowed`, status state, stable reason, matched policy and
  rule ids, and normalized settings.
- `LoadFromEnv`, `ParsePolicyJSON`, and `Normalize` parse and validate policy.
- `Evaluate` applies policy, provider status, source allowlists, and ACL state.
- `EvaluateEgress` and `EgressDecision` are the focused claim-path egress
  re-check used by the semantic-provider execution worker immediately before any
  provider dispatch. It re-confirms only provider-profile and source-class egress
  posture and fails closed on a missing policy, missing allow rule, or explicit
  deny. It carries no provider host, endpoint, URL, or credential, so its result
  is safe for redacted telemetry, logs, and audit labels.
- `ApplyToProviderStatuses` projects policy allowlists into redacted status rows.

## Dependencies

- `internal/semanticprofile` supplies shared provider/source-class constants.
- `internal/status` supplies the stable semantic extraction state vocabulary and
  redacted provider profile status rows.

The package imports no storage, telemetry, runtime, or provider SDK packages.

## Telemetry

This package emits no metrics or spans. It returns low-cardinality reason codes
that future queue or provider workers can attach to bounded counters, logs, and
spans without including raw paths, prompts, source identifiers, or credential
handles.

## Gotchas / invariants

- Policy denies by default. Empty policy, disabled policy, unsupported source
  class, missing profile, stale ACL, missing egress policy, denied egress, and
  unallowlisted source all return a denied `Decision`.
- Provider profiles alone are not enough. API and MCP status only report source
  policy as configured after this package intersects provider profile source
  classes with explicit policy rules and semantic provider egress rules.
- ACL state must be `allowed`; `denied`, `partial`, `missing`, and `stale` fail
  closed.
- `egress.mode=broad` is an explicit operator opt-in and cannot include
  provider-specific rules. Restricted mode requires a provider-profile and
  source-class allow rule before provider work can be planned.
- Retention is intentionally narrow: metadata-only or hash-only posture with no
  prompt body retention and hash-only prompt metadata; response retention may be
  hash-only or bounded redacted excerpts.

## Related docs

- `docs/internal/design/1758-documentation-semantic-observations.md`
- `docs/public/reference/environment-runtime-storage.md`
- `docs/public/reference/telemetry/index.md`
