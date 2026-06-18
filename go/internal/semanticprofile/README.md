# internal/semanticprofile

`internal/semanticprofile` parses the optional semantic extraction provider
profile configuration used by hosted runtimes. It is deliberately model-only:
the package validates profile metadata and credential handles, then returns
redacted status rows for API, MCP, and admin status surfaces.

The package does not read secret values, open provider clients, perform health
probes, or decide source allowlists. Credential rotation, source policy, prompt
safety, and provider traffic are owned by later surfaces; API and MCP runtimes
intersect these profile rows with `internal/semanticpolicy` before reporting
source policy as configured.

## Configuration Contract

Profiles are supplied as JSON in `ESHU_SEMANTIC_PROVIDER_PROFILES_JSON`. Each
profile must include a stable `profile_id`, a supported `provider_kind`, a
`credential_source`, and one or more allowed `source_classes`.
`search_documents` is the source class for curated `EshuSearchDocument` text
used by governed search-vector builds.

Credential sources carry handles only. For `environment_variable`, the handle
must be the name of an environment variable, not the provider key value itself.
Status projections expose the credential source kind and configured flag, but
never the handle. Profile-local `source_policy_configured` metadata is preserved
by this package, then runtime status gates source classes through
`ESHU_SEMANTIC_EXTRACTION_POLICY_JSON`.

## Verification

```bash
cd go && go test ./internal/semanticprofile -count=1
```
