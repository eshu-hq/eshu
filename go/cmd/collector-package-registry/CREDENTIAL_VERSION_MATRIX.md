# Credential handling, version matrix, and rollback draft (#8862 PREP)

## Credential handling (current in-tree behavior, must be preserved)

- Resolution: `username_env` / `password_env` / `bearer_token_env` target
  fields name environment variables; `mapTarget` (`config.go`) resolves them
  with `getenv` into `runtime.TargetConfig{Username, Password, BearerToken}`.
  Raw values never enter desired collector config, workflow rows, facts,
  logs, metric labels, or docs.
- Transport: bearer token becomes an `Authorization: Bearer` header, else
  username/password becomes Basic auth (`http_provider.go`). Credential-free
  cassette mode needs no secrets at all.
- Missing-credential path: a private-visibility target with a metadata URL
  but no resolved credential completes as `credentials_missing` warning
  evidence with package, source URI, and registry detail suppressed
  (`suppressPrivateTargetDetail`).
- Redaction: `sanitizeURL` drops userinfo and strips sensitive query keys
  (`access_token`, `api_key`, `apikey`, `auth`, `authorization`, `jwt`,
  `key`, `password`, `passwd`, `secret`, `sig`, `signature`, `token`,
  `x-amz-credential`, `x-amz-security-token`, `x-amz-signature`); unparseable
  URLs fail closed to empty. `sanitizeText` applies the same pass to
  free-text messages. Pinned by `credential_canary_test.go` in both the
  `package` and `runtime` trees plus the parity leak probe.

## Supported version matrix

| Component | Oldest supported | Newest verified | Evidence |
|---|---|---|---|
| Core (eshu-hq/eshu) | TBD (owner-designated QA EKS config) | `c697b07618` (this base) | worktree HEAD |
| Producer (in-tree) | same as core | same as core | monorepo build |
| Producer (extracted) | NOT_CHECKED | NOT_CHECKED | destination repo absent |
| Fact schemas (all 9 kinds) | `1.0.0` | `1.0.0` | `chain` constants + parity tests |
| Metadata byte limit | 20MiB | 20MiB | `TestExtractionPrepMetadataByteLimit` |
| Go toolchain | `go 1.26.10` (go.mod) | Homebrew Go used locally | `go test` run |

Oldest/newest core-vs-component pairs for production (AC7) stay NOT_CHECKED
until the owner-designated QA EKS deployment configuration is wired and the
destination exists.

## Rollback rehearsal draft (AC9, NOT_CHECKED)

1. Stop claims: disable the extracted producer's desired collector
   instances (or stop the deployment); confirm no new `ClaimID` issues.
2. Drain or expire leases: wait out `ClaimLeaseTTL` or expire outstanding
   claims in the workflow control plane; confirm zero held claims.
3. Revoke replaced authority: revoke the extracted producer's grant; confirm
   the commit path rejects its fencing tokens.
4. Restore the supported producer: re-enable the in-tree (or prior)
   producer for the same scopes; replay the last committed generation per
   scope.
5. Verify exact truth: rerun the parity suite, then compare admitted facts,
   reducer projection, canonical graph, HTTP API, and MCP truth against the
   pre-cutover baseline; confirm one authority only (no dual writes).
6. Record: promotion-gate evidence, head SHAs on both sides, and the
   rollback decision in the goal file or PR before closing AC9.
