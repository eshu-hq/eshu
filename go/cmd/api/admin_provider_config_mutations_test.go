// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// newTestProviderConfigMutationAdapter builds an adapter with a real store
// (backed by the empty fake DB) and the given env-registered id set, so
// rejectIfEnvManaged tests exercise the real Update/Revert/Enable/Disable
// methods rather than the helper in isolation.
func newTestProviderConfigMutationAdapter(envIDs map[string]struct{}) *providerConfigMutationAdapter {
	return &providerConfigMutationAdapter{
		envProviderIDs: envIDs,
	}
}

// TestProviderConfigMutationAdapterRejectsEnvManagedProvider proves Update,
// Revert, Enable, and Disable all reject a provider_config_id that is
// env-registered (pure env-only, or a shadowed DB row — both land in the
// same envProviderIDs set) with ErrAdminProviderConfigManagedByEnvironment,
// WITHOUT ever calling into the store (#4966 acceptance criteria: "edit
// attempts rejected with a clear error").
func TestProviderConfigMutationAdapterRejectsEnvManagedProvider(t *testing.T) {
	t.Parallel()
	adapter := newTestProviderConfigMutationAdapter(map[string]struct{}{"env_managed_1": {}})
	ctx := context.Background()

	t.Run("update", func(t *testing.T) {
		_, err := adapter.UpdateProviderConfig(ctx, query.AdminProviderConfigUpdateRequest{ProviderConfigID: "env_managed_1"})
		if !errors.Is(err, query.ErrAdminProviderConfigManagedByEnvironment) {
			t.Fatalf("UpdateProviderConfig() error = %v, want ErrAdminProviderConfigManagedByEnvironment", err)
		}
	})
	t.Run("revert", func(t *testing.T) {
		_, err := adapter.RevertProviderConfig(ctx, query.AdminProviderConfigRevertRequest{ProviderConfigID: "env_managed_1"})
		if !errors.Is(err, query.ErrAdminProviderConfigManagedByEnvironment) {
			t.Fatalf("RevertProviderConfig() error = %v, want ErrAdminProviderConfigManagedByEnvironment", err)
		}
	})
	t.Run("enable", func(t *testing.T) {
		_, err := adapter.EnableProviderConfig(ctx, "env_managed_1", "tenant_a", "rev_1")
		if !errors.Is(err, query.ErrAdminProviderConfigManagedByEnvironment) {
			t.Fatalf("EnableProviderConfig() error = %v, want ErrAdminProviderConfigManagedByEnvironment", err)
		}
	})
	t.Run("disable", func(t *testing.T) {
		_, err := adapter.DisableProviderConfig(ctx, "env_managed_1", "tenant_a")
		if !errors.Is(err, query.ErrAdminProviderConfigManagedByEnvironment) {
			t.Fatalf("DisableProviderConfig() error = %v, want ErrAdminProviderConfigManagedByEnvironment", err)
		}
	})
}

// TestProviderConfigMutationAdapterAllowsNonEnvManagedProvider proves a
// normal DB-only provider_config_id (not in the env-registered set) is NOT
// rejected by rejectIfEnvManaged — it fails later for the unrelated reason
// that the fake store has no matching row (Found=false), never with
// ErrAdminProviderConfigManagedByEnvironment.
func TestProviderConfigMutationAdapterAllowsNonEnvManagedProvider(t *testing.T) {
	t.Parallel()
	adapter := newTestProviderConfigMutationAdapter(map[string]struct{}{"env_managed_1": {}})
	if err := adapter.rejectIfEnvManaged("pc_db_only"); err != nil {
		t.Fatalf("rejectIfEnvManaged(pc_db_only) = %v, want nil", err)
	}
	if err := adapter.rejectIfEnvManaged("env_managed_1"); !errors.Is(err, query.ErrAdminProviderConfigManagedByEnvironment) {
		t.Fatalf("rejectIfEnvManaged(env_managed_1) = %v, want ErrAdminProviderConfigManagedByEnvironment", err)
	}
}
