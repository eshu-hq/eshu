# scopedtoken

Resolves hosted per-team bearer tokens into bounded authorization contexts for
the Eshu API and MCP read surface (issue #1852, epic #1899).

## Why

The hosted surface authenticates with one shared bearer token via
`query.AuthMiddleware*`; every holder can read every indexed repository. This
package closes that gap: generated identity-backed tokens and the
operator-managed registry both map a token to a tenant, workspace, and the
repository / ingestion-scope ids it may read. The `query.AuthContext` it
returns flows through the existing scoped-route gate and the per-family bounded
query filters (repositories, code search, documentation, service/package/CI-CD
correlations, supply-chain impact, container-image identities, ...), so a
per-team token reads only its onboarded scope.

## Contract

- `LoadRegistryFromFile(path)` reads and validates a JSON registry document and
  returns a `*Registry`. It **fails closed**: malformed JSON, a bad token hash,
  unsafe audit metadata, a duplicate hash, a missing tenant/workspace, or an
  unsupported version is a hard error. Error messages never include token-hash
  material.
- `(*Registry).ResolveScopedToken(ctx, credential)` implements
  `query.ScopedTokenResolver`. It hashes the presented credential with SHA-256
  and returns the matching `AuthContext` (`Mode = scoped`). An empty or
  unrecognized credential returns `(zero, false, nil)` so the caller falls
  through to shared-token or unauthenticated handling. It never logs or returns
  the credential.
- `NewPostgresIdentityResolver(store)` resolves generated personal and
  service-principal API tokens from `identity_token_metadata`, active identity
  subjects, active role assignments, and active repository/scope targets. It
  records `last_used_at` after a successful lookup.
- `ChainResolvers(...)` composes generated identity tokens, the optional file
  registry, and any future scoped resolver without changing shared-token
  compatibility fallback.
- The registry is read-only after construction and safe for concurrent use.

## Security model

- The registry file stores only `token_sha256` (lowercase hex SHA-256), never
  the token. A leaked file cannot be replayed because SHA-256 is
  preimage-resistant.
- Lookup is by hash of attacker-controlled input; forging a match requires a
  preimage, so a hash-map lookup is sufficient (the standard hashed-API-key
  pattern).
- No token, credential, raw subject, raw policy body, path, or grant id is ever
  placed in an error, log line, or metric label by this package.

## Registry file format

```json
{
  "version": 1,
  "tokens": [
    {
      "token_sha256": "<lowercase hex sha256 of the bearer token>",
      "tenant_id": "team-payments",
      "workspace_id": "team-payments",
      "subject_class": "team_token",
      "subject_id_hash": "sha256:9f86d081884c7d65",
      "policy_revision_hash": "sha256:e3b0c44298fc1c14",
      "all_scopes": false,
      "allowed_scope_ids": ["git-repository-scope:acme/payments"],
      "allowed_repository_ids": ["repo://acme/payments"]
    }
  ]
}
```

Optional audit attribution fields must be `sha256:` hashes when set. Omit them
when the registry only needs to carry scope grants.

`all_scopes: true` marks an admin-equivalent scoped token. With `all_scopes`
false and empty grants the token authorizes no repositories and reads return
bounded empty/zero shapes.

## Wiring

The API and MCP servers always add the Postgres identity-token resolver after
Postgres connects, then append the optional registry from
`ESHU_SCOPED_TOKENS_FILE` (`internalruntime.ScopedTokenResolverFromEnv`).
Generated identity tokens are tried first; unknown credentials fall through to
the file registry and then to the shared-token / local dev-mode behavior.

## Operator issuance & rotation

The registry file is the issuance and rotation surface:

- **Issue**: generate a random token, compute `sha256` of it, add an entry with
  the hash and the team's grants, deliver the token to the team over a secure
  channel, and reload (restart) the API/MCP pods.
- **Rotate**: replace the entry's `token_sha256` with the new token's hash.
- **Revoke**: remove the entry.

See `docs/public/operate/hosted-governance.md` for the operator runbook.
