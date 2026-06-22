package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeIdentitySubjects(t *testing.T) {
	t.Parallel()

	var identity Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "identity_subjects" {
			identity = def
			break
		}
	}
	if identity.Name == "" {
		t.Fatal("identity_subjects definition missing")
	}
	if identity.Path != "schema/data-plane/postgres/006e_identity_subjects.sql" {
		t.Fatalf("identity schema path = %q, want schema/data-plane/postgres/006e_identity_subjects.sql", identity.Path)
	}

	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS identity_users",
		"CREATE TABLE IF NOT EXISTS identity_user_emails",
		"CREATE TABLE IF NOT EXISTS identity_provider_configs",
		"CREATE TABLE IF NOT EXISTS identity_provider_config_revisions",
		"CREATE TABLE IF NOT EXISTS identity_external_subjects",
		"CREATE TABLE IF NOT EXISTS identity_local_credentials",
		"CREATE TABLE IF NOT EXISTS identity_mfa_factors",
		"CREATE TABLE IF NOT EXISTS identity_mfa_recovery_codes",
		"CREATE TABLE IF NOT EXISTS identity_tenant_memberships",
		"CREATE TABLE IF NOT EXISTS identity_roles",
		"CREATE TABLE IF NOT EXISTS identity_role_grants",
		"CREATE TABLE IF NOT EXISTS identity_membership_roles",
		"CREATE TABLE IF NOT EXISTS identity_sessions",
		"CREATE TABLE IF NOT EXISTS identity_service_principals",
		"CREATE TABLE IF NOT EXISTS identity_service_principal_roles",
		"CREATE TABLE IF NOT EXISTS identity_token_metadata",
		"disabled_at TIMESTAMPTZ NULL",
		"email_hash TEXT NOT NULL",
		"external_subject_id_hash TEXT NOT NULL",
		"credential_handle TEXT",
		"password_hash TEXT NOT NULL",
		"recovery_code_hash TEXT NOT NULL",
		"session_hash TEXT PRIMARY KEY",
		"token_hash TEXT NOT NULL",
		"REFERENCES tenants(tenant_id) ON DELETE CASCADE",
		"REFERENCES workspaces(tenant_id, workspace_id) ON DELETE CASCADE",
		"REFERENCES identity_users(user_id) ON DELETE CASCADE",
		"identity_external_subjects_provider_subject_idx",
		"identity_provider_configs_tenant_kind_key_idx",
		"identity_tenant_memberships_active_idx",
		"identity_sessions_active_idx",
		"identity_service_principal_roles_active_idx",
		"identity_token_metadata_active_idx",
	} {
		if !strings.Contains(identity.SQL, want) {
			t.Fatalf("identity schema SQL missing %q", want)
		}
	}

	for _, forbidden := range []string{
		"raw_token",
		"token_value",
		"bearer_token",
		"session_id text",
		"cookie_value",
		"raw_password",
		"password text",
		"client_secret text",
		"provider_secret",
		"saml_assertion",
		"private_key",
		"email text",
		"external_subject_id text",
	} {
		if strings.Contains(strings.ToLower(identity.SQL), forbidden) {
			t.Fatalf("identity schema SQL contains forbidden marker %q", forbidden)
		}
	}
}

func TestIdentitySubjectStoreEnsureSchemaUsesDefinitionSQL(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	store := NewIdentitySubjectStore(db)
	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("exec count = %d, want 1", len(db.execs))
	}
	if db.execs[0].query != IdentitySubjectSchemaSQL() {
		t.Fatal("EnsureSchema() did not execute IdentitySubjectSchemaSQL()")
	}
	if strings.Contains(strings.ToLower(db.execs[0].query), "token_value") {
		t.Fatal("EnsureSchema() SQL stores token values")
	}
}
