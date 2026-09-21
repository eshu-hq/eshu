// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestAdapterAPIClientForbidsMutationAndTokenReads is the security acceptance
// gate: the Amplify SDK adapter must never be able to create, delete, update,
// start a job, start a deployment, generate access logs, mutate a webhook, or
// otherwise change an Amplify resource. Read-only as it is, none of those calls
// can reach an app's environment variables, build-spec secrets, repository
// access tokens, or basic-auth credentials as a write. We reflect over the
// adapter-local apiClient interface and fail the build if any forbidden
// operation becomes reachable.
func TestAdapterAPIClientForbidsMutationAndTokenReads(t *testing.T) {
	forbiddenExact := []string{
		"CreateApp", "DeleteApp", "UpdateApp",
		"CreateBranch", "DeleteBranch", "UpdateBranch",
		"CreateDomainAssociation", "DeleteDomainAssociation", "UpdateDomainAssociation",
		"CreateDeployment", "StartDeployment", "StartJob", "StopJob",
		"CreateBackendEnvironment", "DeleteBackendEnvironment",
		"CreateWebhook", "DeleteWebhook", "UpdateWebhook",
		"GenerateAccessLogs",
	}
	// Any method whose name begins with one of these verbs is a write, lifecycle,
	// or payload-generating operation and must not exist on the metadata-only
	// adapter.
	forbiddenPrefixes := []string{
		"Create", "Delete", "Update", "Put",
		"Start", "Stop", "Generate",
		"Tag", "Untag",
	}
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		for _, banned := range forbiddenExact {
			if name == banned {
				t.Fatalf("apiClient exposes forbidden mutation method %q; the Amplify adapter is metadata-only", name)
			}
		}
		for _, prefix := range forbiddenPrefixes {
			if strings.HasPrefix(name, prefix) {
				t.Fatalf("apiClient exposes mutation method %q (prefix %q); the Amplify adapter is metadata-only", name, prefix)
			}
		}
	}
}

// TestAdapterMethodsAreReadOnly asserts every method on the apiClient interface
// is a List read so the read surface stays explicit and auditable. Amplify
// exposes the app/branch/domain metadata the scanner needs through List APIs
// only; GetApp/GetBranch would return the same secret-bearing structs and are
// not needed, so the read surface omits even those Get reads.
func TestAdapterMethodsAreReadOnly(t *testing.T) {
	iface := reflect.TypeOf((*apiClient)(nil)).Elem()
	if iface.NumMethod() == 0 {
		t.Fatalf("apiClient interface has no methods; expected the Amplify read surface")
	}
	for i := 0; i < iface.NumMethod(); i++ {
		name := iface.Method(i).Name
		if !strings.HasPrefix(name, "List") {
			t.Fatalf("apiClient method %q is not a List read", name)
		}
	}
}
