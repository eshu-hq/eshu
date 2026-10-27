// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalert

import (
	"reflect"
	"testing"
)

// An alert that names a Maven package only through its package ID must yield
// the group:artifact coordinate, not the registry path segment glued onto it
// (issue #9286). The alert manifest filter sends these names to an exact
// entity_name predicate, so "maven2/<coordinate>" matches no manifest row.
func TestSecurityAlertPackageNameCandidatesMavenPackageID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		alert ProviderSecurityAlert
		want  []string
	}{
		{
			name: "package id only",
			alert: ProviderSecurityAlert{SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{
				PackageID: "maven://repo.maven.apache.org/maven2/org.example:lib",
			}},
			want: []string{"org.example:lib"},
		},
		{
			name: "provider name already a coordinate",
			alert: ProviderSecurityAlert{SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{
				PackageID:   "maven://repo.maven.apache.org/maven2/org.example:lib",
				PackageName: "org.example:lib",
			}},
			want: []string{"org.example:lib"},
		},
		{
			name: "npm scoped name keeps its path",
			alert: ProviderSecurityAlert{SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{
				PackageID: "npm://registry.npmjs.org/@scope/pkg",
			}},
			want: []string{"@scope/pkg"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SecurityAlertPackageNameCandidates(test.alert); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("SecurityAlertPackageNameCandidates() = %#v, want %#v", got, test.want)
			}
		})
	}
}
