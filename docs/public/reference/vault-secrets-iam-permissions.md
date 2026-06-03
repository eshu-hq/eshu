# Vault Secrets/IAM Collector — Read-Only Permissions

The Vault source lane of the
[Secrets and IAM Posture Collector](secrets-iam-posture-collector-contract.md)
observes Vault identity, trust, and **secret-metadata** posture and emits
redacted facts. It is **metadata-only by construction**: it never reads a secret
value, token, AppRole `secret_id`, or any credential material, and it never
touches a Vault KV `/data` endpoint.

This page gives operators the minimal read-only Vault policy the lane needs and
the metadata-sensitivity warnings that come with running it.

## What the lane reads

| Fact family | Vault endpoint(s) | Read shape |
| --- | --- | --- |
| `vault_auth_mount` | `sys/auth` | List/describe auth method mounts |
| `vault_auth_role` | `auth/<mount>/role`, `auth/<mount>/role/<name>` | Auth role metadata, incl. Kubernetes-auth `bound_service_account_*` (the IAM↔Vault join anchor) |
| `vault_acl_policy` | `sys/policies/acl`, `sys/policies/acl/<name>` | Policy name, content hash, per-rule capability summary — **never the raw policy body** |
| `vault_identity_entity` | `identity/entity/id` | Entity metadata, alias/group counts |
| `vault_identity_alias` | `identity/entity-alias/id` | Alias→entity/mount join anchors |
| `vault_kv_metadata` | `LIST <mount>/metadata`, KV v2 metadata/config | Path fingerprint, version counters, custom-metadata **key names** — **never `/data`** |
| `vault_secret_engine_mount` | `sys/mounts` | Secret-engine mount metadata |

The collector's client interface exposes **no** value-reading operation; a
structural test enforces that no method can read KV `/data`, a token, or an
AppRole `secret_id`.

## Minimal read-only policy

The following policy grants only what the lane reads. It deliberately excludes
every `/data/` path and any `create`/`update`/`delete` capability.

```hcl
# secrets-iam-readonly.hcl — Eshu Vault secrets/IAM posture collector

# Auth method mounts and roles (metadata only)
path "sys/auth" {
  capabilities = ["read", "list"]
}
path "auth/+/role" {
  capabilities = ["list"]
}
path "auth/+/role/+" {
  capabilities = ["read"]
}

# ACL policies (name + body hash + capability summary; body is hashed in-fact)
path "sys/policies/acl" {
  capabilities = ["list"]
}
path "sys/policies/acl/+" {
  capabilities = ["read"]
}

# Identity entities and aliases
path "identity/entity/id" {
  capabilities = ["list"]
}
path "identity/entity/id/+" {
  capabilities = ["read"]
}
path "identity/entity-alias/id" {
  capabilities = ["list"]
}
path "identity/entity-alias/id/+" {
  capabilities = ["read"]
}

# Secret-engine mounts
path "sys/mounts" {
  capabilities = ["read", "list"]
}

# KV v2 METADATA ONLY — never grant read on <kv_mount>/data/*
# Repeat these stanzas for each KV v2 mount the collector may inspect.
# Example mount paths:
#   - secret
#   - kv/team
# The trailing `*` glob covers nested metadata keys under that mount.
path "<kv_mount>/metadata" {
  capabilities = ["list"]
}
path "<kv_mount>/metadata/*" {
  capabilities = ["read", "list"]
}
path "<kv_mount>/config" {
  capabilities = ["read"]
}
```

Apply and bind it to the collector's auth role, for example:

```bash
vault policy write eshu-secrets-iam-readonly secrets-iam-readonly.hcl
```

Bind the policy to whatever auth method the collector authenticates with
(Kubernetes auth, AppRole, etc.). The collector receives an already
short-lived, read-only token; Eshu never stores it.

!!! danger "Never grant data read"
    Do **not** add `path "<kv_mount>/data/*" { capabilities = ["read"] }`. The lane
    has no code path that reads secret values, and granting data read would
    expand the blast radius of the collector token for no benefit.

## Metadata sensitivity

Secret **metadata** is sensitive even though secret values are never read.
Names, paths, and policy resources can reveal trust topology and naming schemes.
The lane therefore **hashes/fingerprints by default**:

- Mount paths, accessors, ACL rule paths, KV paths, KV custom-metadata key
  names, identity entity/alias IDs and names, auth role names, and bound
  ServiceAccount names/namespaces are emitted as keyed fingerprints, not
  cleartext. A redaction canary test asserts no raw value of these reaches a
  fact payload, log, or metric.
- Vault KV v2 `LIST .../metadata` returns key names and does no policy filtering
  on those names, which is exactly why key names are hashed by default. See the
  [Vault KV v2 API](https://developer.hashicorp.com/vault/api-docs/secret/kv/kv-v2).
- The configured Vault address is sanitized to `scheme://host/path` before it is
  recorded as fact provenance, so a credential-bearing URL (basic-auth userinfo
  or a token query parameter) is never persisted.

Only low-cardinality, non-sensitive enums are emitted in cleartext: auth method
(`kubernetes`, `approle`), mount type (`kv-v2`), KV version, and ACL
capabilities (`read`, `list`).

## Status and deployment

The Vault source mapping (all seven fact families) is implemented. The live
`hashicorp/vault/api` client adapter, the `secrets_iam_posture` collector kind,
claim-driven scheduling, Helm chart values, and the `eshu_dp_secrets_iam_*`
source metrics are tracked in issue #1356; this page documents the read-only
permission contract those components will require so operators can provision the
policy ahead of rollout.

## Related

- [Secrets and IAM Posture Collector Contract](secrets-iam-posture-collector-contract.md)
- [Vault auth API](https://developer.hashicorp.com/vault/api-docs/system/auth)
- [Vault policies API](https://developer.hashicorp.com/vault/api-docs/system/policies)
