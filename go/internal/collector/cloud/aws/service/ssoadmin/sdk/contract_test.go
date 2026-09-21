// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"reflect"
	"strings"
	"testing"
)

// TestSSOAdminAPIClientExcludesMutationAndInlinePolicyReads is the load-bearing
// proof for issue #760: the SDK adapter reaches AWS IAM Identity Center only
// through the metadata-only reads listed below. The Identity Center permission
// set inline policy body encodes the org least-privilege model, so the adapter
// must never call GetInlinePolicyForPermissionSet or
// GetPermissionsBoundaryForPermissionSet. Application access-scope attributes
// can carry sensitive group filters, so the adapter must never call
// GetApplicationAccessScope or ListApplicationAccessScopes. The two AWS API
// interfaces (ssoAdminAPI and identityStoreAPI) are the only ways the adapter
// reaches AWS, so asserting their method shape proves the forbidden APIs are
// unreachable from this code path.
func TestSSOAdminAPIClientExcludesMutationAndInlinePolicyReads(t *testing.T) {
	wantSSOAdmin := map[string]bool{
		"ListInstances":                                      true,
		"ListPermissionSets":                                 true,
		"DescribePermissionSet":                              true,
		"ListManagedPoliciesInPermissionSet":                 true,
		"ListCustomerManagedPolicyReferencesInPermissionSet": true,
		"ListAccountsForProvisionedPermissionSet":            true,
		"ListAccountAssignments":                             true,
		"ListApplications":                                   true,
		"ListTrustedTokenIssuers":                            true,
		"ListTagsForResource":                                true,
	}
	assertInterfaceMethods(t, reflect.TypeOf((*ssoAdminAPI)(nil)).Elem(), wantSSOAdmin)

	wantIdentityStore := map[string]bool{
		"DescribeGroup": true,
		"DescribeUser":  true,
	}
	assertInterfaceMethods(t, reflect.TypeOf((*identityStoreAPI)(nil)).Elem(), wantIdentityStore)

	// Defensive substring check across both interfaces. Any mutation verb,
	// inline-policy read, permissions-boundary read, or access-scope read is a
	// contract violation regardless of the want-list above.
	forbidden := []string{
		"Create",
		"Update",
		"Delete",
		"Put",
		"Attach",
		"Detach",
		"ProvisionPermissionSet",
		"InlinePolicy",
		"PermissionsBoundary",
		"AccessScope",
		"ApplicationGrant",
		"ApplicationAuthenticationMethod",
		"GroupMembership",
	}
	// Allowed exceptions: methods whose names contain a forbidden substring but
	// are still safe metadata reads. ListAccountsForProvisionedPermissionSet
	// lists the accounts a permission set is provisioned to; it is not the
	// ProvisionPermissionSet mutation.
	allowed := map[string]bool{
		"ListAccountsForProvisionedPermissionSet": true,
	}
	for _, iface := range []reflect.Type{
		reflect.TypeOf((*ssoAdminAPI)(nil)).Elem(),
		reflect.TypeOf((*identityStoreAPI)(nil)).Elem(),
	} {
		for i := 0; i < iface.NumMethod(); i++ {
			name := iface.Method(i).Name
			if allowed[name] {
				continue
			}
			for _, bad := range forbidden {
				if strings.Contains(name, bad) {
					t.Errorf("%s method %q contains forbidden substring %q", iface.Name(), name, bad)
				}
			}
		}
	}
}

func assertInterfaceMethods(t *testing.T, iface reflect.Type, want map[string]bool) {
	t.Helper()
	have := map[string]bool{}
	for i := 0; i < iface.NumMethod(); i++ {
		have[iface.Method(i).Name] = true
	}
	for name := range want {
		if !have[name] {
			t.Errorf("%s missing required method %q", iface.Name(), name)
		}
	}
	for name := range have {
		if !want[name] {
			t.Errorf("%s exposes unexpected method %q; metadata-only contract violated", iface.Name(), name)
		}
	}
}
