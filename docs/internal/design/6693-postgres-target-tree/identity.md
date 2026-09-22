# #6693 mapping: identity and governance audit

Part of the [storage/postgres target tree](../6693-postgres-target-tree.md). Destinations covered: `identity`, `governance`.

Paths are relative to `go/internal/storage/postgres/`. Each line reads `current -> new`.

### `governance/audit/` (2 non-test, 5 test)

```text
governance_audit_store.go -> governance/audit/store.go
governance_audit_store_helpers.go -> governance/audit/helpers.go
```

<details><summary>Tests</summary>

```text
governance_audit_append_bench_test.go -> governance/audit/append_bench_test.go
governance_audit_list_warn_test.go -> governance/audit/list_warn_test.go
governance_audit_scan_tolerance_test.go -> governance/audit/scan_tolerance_test.go
governance_audit_store_test.go -> governance/audit/store_test.go   # external test package: imports root
governance_audit_tenant_test.go -> governance/audit/tenant_test.go
```

</details>

### `identity/` (1 non-test, 1 test)

```text
identity_subjects.go -> identity/subjects.go   # type decomposition first, see D1
```

<details><summary>Tests</summary>

```text
identity_subjects_test.go -> identity/subjects_test.go   # external test package: imports root
```

</details>

### `identity/admin/` (4 non-test, 4 test)

```text
identity_admin_mutations.go -> identity/admin/mutations.go
identity_admin_mutations_sql.go -> identity/admin/mutations_sql.go
identity_admin_reads.go -> identity/admin/reads.go
identity_admin_reads_providers.go -> identity/admin/reads_providers.go
```

<details><summary>Tests</summary>

```text
identity_admin_mutations_fake_db_test.go -> identity/admin/mutations_fake_db_test.go
identity_admin_mutations_test.go -> identity/admin/mutations_test.go
identity_admin_reads_test.go -> identity/admin/reads_test.go
identity_mapping_pagination_live_test.go -> identity/admin/mapping_pagination_live_test.go
```

</details>

### `identity/api/` (7 non-test, 4 test)

```text
identity_api_token_lifecycle.go -> identity/api/token_lifecycle.go
identity_api_token_lifecycle_sql.go -> identity/api/token_lifecycle_sql.go
identity_api_tokens.go -> identity/api/scoped_resolution.go
identity_api_tokens_list.go -> identity/api/token_list.go
identity_api_tokens_sql.go -> identity/api/scoped_resolution_sql.go
scoped_api_tokens.go -> identity/api/scoped_token.go
scoped_api_tokens_schema.go -> identity/api/scoped_token_schema.go
```

<details><summary>Tests</summary>

```text
identity_api_token_owner_scope_test.go -> identity/api/token_owner_scope_test.go   # follows its private symbols, not its name
identity_api_tokens_list_test.go -> identity/api/token_list_test.go   # spans identity/api=50% identity=50%
identity_api_tokens_reserved_alias_test.go -> identity/api/scoped_resolution_reserved_alias_test.go
scoped_api_tokens_test.go -> identity/api/scoped_token_test.go   # external test package: imports root
```

</details>

### `identity/bootstrap/` (6 non-test, 8 test)

```text
identity_bootstrap_credential.go -> identity/bootstrap/credential.go
identity_bootstrap_credential_mfa.go -> identity/bootstrap/credential_mfa.go
identity_bootstrap_credential_owner.go -> identity/bootstrap/credential_owner.go
identity_bootstrap_credential_sql.go -> identity/bootstrap/credential_sql.go
identity_bootstrap_credential_validate.go -> identity/bootstrap/credential_validate.go
identity_setup_completion.go -> identity/bootstrap/setup_completion.go
```

<details><summary>Tests</summary>

```text
identity_bootstrap_credential_concurrency_test.go -> identity/bootstrap/credential_concurrency_test.go   # external test package: imports identity, root
identity_bootstrap_credential_mfa_test.go -> identity/bootstrap/credential_mfa_test.go   # external test package: imports identity
identity_bootstrap_credential_owner_test.go -> identity/bootstrap/credential_owner_test.go   # external test package: imports identity
identity_bootstrap_credential_recovery_scope_live_test.go -> identity/bootstrap/credential_recovery_scope_live_test.go   # external test package + export_test.go shim: imports root
identity_bootstrap_credential_test.go -> identity/bootstrap/credential_test.go   # external test package: imports identity, identity/local
identity_bootstrap_reenroll_mfa_lock_contention_test.go -> identity/bootstrap/reenroll_mfa_lock_contention_test.go   # external test package: imports identity, identity/local, root
identity_setup_completion_concurrency_test.go -> identity/bootstrap/setup_completion_concurrency_test.go   # external test package: imports identity, identity/local, root
identity_setup_completion_test.go -> identity/bootstrap/setup_completion_test.go   # external test package: imports identity, identity/local
```

</details>

### `identity/github/` (2 non-test, 1 test)

```text
github_login.go -> identity/github/login.go
github_login_schema.go -> identity/github/schema.go
```

<details><summary>Tests</summary>

```text
github_login_test.go -> identity/github/login_test.go   # external test package: imports root
```

</details>

### `identity/local/` (12 non-test, 14 test)

```text
identity_local.go -> identity/local/local.go
identity_local_helpers.go -> identity/local/helpers.go
identity_local_lifecycle.go -> identity/local/lifecycle.go
identity_local_mfa_reset_lock.go -> identity/local/mfa_reset_lock.go
identity_local_mfa_status.go -> identity/local/mfa_status.go
identity_local_rotate.go -> identity/local/rotate.go
identity_local_schema.go -> identity/local/schema.go
identity_local_sql.go -> identity/local/sql.go
identity_local_totp.go -> identity/local/totp.go
identity_local_totp_sql.go -> identity/local/totp_sql.go
identity_local_types.go -> identity/local/types.go
identity_local_validate.go -> identity/local/validate.go
```

<details><summary>Tests</summary>

```text
identity_local_bootstrap_consume_test.go -> identity/local/bootstrap_consume_test.go   # external test package: imports identity
identity_local_mfa_all_users_login_test.go -> identity/local/mfa_all_users_login_test.go   # external test package + export_test.go shim: imports identity
identity_local_mfa_reset_concurrency_test.go -> identity/local/mfa_reset_concurrency_test.go   # external test package: imports identity, root
identity_local_mfa_reset_lock_contention_test.go -> identity/local/mfa_reset_lock_contention_test.go   # external test package: imports identity, root
identity_local_mfa_status_test.go -> identity/local/mfa_status_test.go   # spans identity/local=50% identity=50%
identity_local_must_change_password_test.go -> identity/local/must_change_password_test.go   # external test package + export_test.go shim: imports identity
identity_local_rotate_concurrency_test.go -> identity/local/rotate_concurrency_test.go   # external test package: imports identity, root
identity_local_rotate_test.go -> identity/local/rotate_test.go   # external test package: imports identity
identity_local_totp_leakage_test.go -> identity/local/totp_leakage_test.go   # external test package + export_test.go shim: imports identity
identity_local_totp_login_test.go -> identity/local/totp_login_test.go   # external test package + export_test.go shim: imports identity
identity_local_totp_test.go -> identity/local/totp_test.go   # external test package + export_test.go shim: imports identity
local_identity_bootstrap_test.go -> identity/local/bootstrap_test.go   # external test package: imports identity
local_identity_lifecycle_test.go -> identity/local/lifecycle_test.go   # external test package: imports identity, identity/api
local_identity_test.go -> identity/local/local_test.go   # external test package + export_test.go shim: imports identity
```

</details>

### `identity/oidc/` (3 non-test, 2 test)

```text
oidc_login.go -> identity/oidc/login.go
oidc_login_schema.go -> identity/oidc/schema.go
oidc_session_refresh.go -> identity/oidc/refresh.go
```

<details><summary>Tests</summary>

```text
oidc_login_test.go -> identity/oidc/login_test.go   # external test package: imports root
oidc_session_refresh_test.go -> identity/oidc/refresh_test.go
```

</details>

### `identity/provider/` (8 non-test, 6 test)

```text
identity_provider_config_login_reads.go -> identity/provider/login_reads.go
identity_provider_config_oidc_bearer_reads.go -> identity/provider/oidc_bearer_reads.go
identity_provider_config_reads.go -> identity/provider/reads.go
identity_provider_config_status_writes.go -> identity/provider/status_writes.go
identity_provider_config_types.go -> identity/provider/types.go
identity_provider_config_writes.go -> identity/provider/writes.go
identity_provider_config_writes_helpers.go -> identity/provider/writes_helpers.go
identity_provider_config_writes_sql.go -> identity/provider/writes_sql.go
```

<details><summary>Tests</summary>

```text
identity_provider_config_enable_test.go -> identity/provider/config_enable_test.go   # external test package: imports identity
identity_provider_config_live_test.go -> identity/provider/config_live_test.go   # external test package: imports identity, root
identity_provider_config_negative_leakage_test.go -> identity/provider/config_negative_leakage_test.go   # external test package + export_test.go shim: imports identity
identity_provider_config_oidc_bearer_reads_test.go -> identity/provider/oidc_bearer_reads_test.go   # external test package: imports identity
identity_provider_config_writes_test.go -> identity/provider/writes_test.go   # external test package: imports identity
identity_saml_provider_secret_roundtrip_test.go -> identity/provider/saml_secret_roundtrip_test.go   # external test package + export_test.go shim: imports identity; follows its private symbols, not its name
```

</details>

### `identity/saml/` (5 non-test, 3 test)

```text
identity_saml.go -> identity/saml/saml.go
identity_saml_login_reads.go -> identity/saml/login_reads.go
identity_saml_sql.go -> identity/saml/sql.go
identity_saml_types.go -> identity/saml/types.go
saml_sso.go -> identity/saml/sso.go
```

<details><summary>Tests</summary>

```text
identity_saml_login_reads_test.go -> identity/saml/login_reads_test.go   # external test package: imports identity
identity_saml_test.go -> identity/saml/saml_test.go   # external test package: imports identity
saml_sso_test.go -> identity/saml/sso_test.go   # external test package: imports root
```

</details>

### `identity/session/` (4 non-test, 5 test)

```text
browser_sessions.go -> identity/session/session.go
browser_sessions_list.go -> identity/session/list.go
browser_sessions_refresh.go -> identity/session/refresh.go
browser_sessions_schema.go -> identity/session/schema.go
```

<details><summary>Tests</summary>

```text
browser_sessions_list_test.go -> identity/session/list_test.go
browser_sessions_oidc_test.go -> identity/session/oidc_test.go
browser_sessions_refresh_test.go -> identity/session/refresh_test.go
browser_sessions_static_grant_policy_hash_live_test.go -> identity/session/static_grant_policy_hash_live_test.go   # external test package: imports root
browser_sessions_test.go -> identity/session/session_test.go   # external test package: imports root
```

</details>

### `identity/sign/` (3 non-test, 4 test)

```text
identity_sign_in_policy.go -> identity/sign/policy.go
identity_sign_in_policy_sql.go -> identity/sign/sql.go
identity_sign_in_policy_types.go -> identity/sign/types.go
```

<details><summary>Tests</summary>

```text
identity_sign_in_policy_concurrency_helpers_test.go -> identity/sign/policy_concurrency_helpers_test.go   # external test package: imports root
identity_sign_in_policy_concurrency_test.go -> identity/sign/policy_concurrency_test.go   # external test package: imports identity, identity/local, root
identity_sign_in_policy_revoke_timeout_test.go -> identity/sign/policy_revoke_timeout_test.go   # external test package: imports identity
identity_sign_in_policy_test.go -> identity/sign/policy_test.go   # external test package + export_test.go shim: imports identity, identity/local
```

</details>
