// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsMutationAndKeyMaterial is the security acceptance
// gate for the Transfer Family scanner (#873): the SDK adapter must never be
// able to create, update, delete, start, stop, import, tag, or otherwise mutate
// a Transfer resource, and must never be able to import or read host keys or SSH
// public keys. We reflect over the adapter-local apiClient interface and fail
// the build if any forbidden operation becomes reachable.
func TestAdapterAPIClientForbidsMutationAndKeyMaterial(t *testing.T) {
	forbiddenExact := []string{
		"CreateServer", "UpdateServer", "DeleteServer", "StartServer", "StopServer",
		"CreateUser", "UpdateUser", "DeleteUser",
		"CreateAccess", "UpdateAccess", "DeleteAccess",
		"CreateAgreement", "UpdateAgreement", "DeleteAgreement",
		"CreateConnector", "UpdateConnector", "DeleteConnector",
		"CreateProfile", "UpdateProfile", "DeleteProfile",
		"CreateWorkflow", "DeleteWorkflow",
		"ImportSshPublicKey", "DeleteSshPublicKey",
		"ImportHostKey", "UpdateHostKey", "DeleteHostKey",
		"ImportCertificate",
		"TestIdentityProvider", "TestConnection",
		"SendWorkflowStepState", "StartFileTransfer", "StartDirectoryListing",
	}
	// Any method whose name begins with one of these verbs is a write,
	// lifecycle, or key-material operation and must not exist on the
	// metadata-only adapter.
	forbiddenPrefixes := []string{
		"Create", "Update", "Delete", "Put",
		"Start", "Stop", "Import", "Send",
		"Tag", "Untag",
		"Test",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden mutation/key-material method %q; the Transfer adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation/key-material method %q (prefix %q); the Transfer adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a List or Describe read so the read surface stays explicit and auditable.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the Transfer read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") && !strings.HasPrefix(name, "Describe") {
			t.Fatalf("apiClient method %q is neither a List nor Describe read", name)
		}
	}
}
