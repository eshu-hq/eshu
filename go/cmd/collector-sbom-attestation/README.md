# collector-sbom-attestation

`collector-sbom-attestation` runs the hosted SBOM and attestation collector as
a claim-driven service. It reads its desired collector instance from
`ESHU_COLLECTOR_INSTANCES_JSON`, claims `sbom_attestation` work items, fetches
the configured document target, and commits typed SBOM or attestation facts.

## Required Configuration

- `collector_kind`: `sbom_attestation`
- `claims_enabled`: `true`
- `configuration.targets[]`: one bounded source target per claim scope

Configured document targets require an HTTP(S) `document_url`. OCI referrer
targets require `provider`, `registry`, `repository`, `subject_digest`, and
`referrer_digest`. Credential values are resolved only from `username_env`,
`password_env`, or `bearer_token_env`.

## Runtime Env

| Variable | Purpose |
| --- | --- |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_INSTANCE_ID` | Select one instance when multiple are configured. |
| `ESHU_SBOM_ATTESTATION_POLL_INTERVAL` | Claim polling interval, default `1s`. |
| `ESHU_SBOM_ATTESTATION_CLAIM_LEASE_TTL` | Claim lease TTL. |
| `ESHU_SBOM_ATTESTATION_HEARTBEAT_INTERVAL` | Claim heartbeat interval. |
| `ESHU_SBOM_ATTESTATION_COLLECTOR_OWNER_ID` | Stable claim owner ID. |

The service exposes the same hosted status, pprof, and Prometheus surfaces as
the other claim-driven collectors.
