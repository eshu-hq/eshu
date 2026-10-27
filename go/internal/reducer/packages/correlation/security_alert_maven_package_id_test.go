// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package correlation

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// A Maven alert that names its package only through the package ID must match
// the manifest dependency's group:artifact name, and must not match a different
// group's artifact or a registry-path-prefixed name (issue #9286). Before the
// package-ID parser dropped the registry path, the parsed name was
// "maven2/<coordinate>", which matched no manifest row.
func TestSecurityAlertPackageNameMatchesMavenPackageID(t *testing.T) {
	t.Parallel()

	alert := securityalert.ProviderSecurityAlert{SecurityAlertReconciliationDecision: securityalert.SecurityAlertReconciliationDecision{
		Ecosystem: "maven",
		PackageID: "maven://repo.maven.apache.org/maven2/com.google.code.gson:gson",
	}}
	tests := []struct {
		name       string
		dependency string
		want       bool
	}{
		{"stored coordinate matches", "com.google.code.gson:gson", true},
		{"stored coordinate matches regardless of case", "Com.Google.Code.Gson:Gson", true},
		{"different group with the same artifact", "org.other:gson", false},
		{"bare artifact", "gson", false},
		{"registry-prefixed name", "maven2/com.google.code.gson:gson", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := SecurityAlertPackageNameMatches(alert, test.dependency); got != test.want {
				t.Fatalf("SecurityAlertPackageNameMatches(%q) = %v, want %v", test.dependency, got, test.want)
			}
		})
	}
}
