package capabilitycatalog

import (
	"path/filepath"
	"slices"
	"testing"
)

func testAuthorizationCatalog() AuthorizationCatalog {
	return AuthorizationCatalog{
		Version: "v1",
		Roles: []BuiltInRole{
			{
				Role:        "developer",
				DisplayName: "Developer",
				Grants: []RoleGrant{
					{Action: "code.content.read", DataClasses: []string{"source_content"}, ScopeLevels: []string{"tenant", "workspace", "repository"}},
				},
			},
			{
				Role:             "owner",
				DisplayName:      "Owner",
				BootstrapDefault: true,
				Grants: []RoleGrant{
					{Action: "admin.manage", DataClasses: []string{"admin_metadata"}},
					{Action: "code.content.read", DataClasses: []string{"source_content"}, ScopeLevels: []string{"tenant", "workspace", "repository"}},
					{Action: "sensitive_data.read", DataClasses: []string{"secret_risk", "audit_sensitive"}},
				},
			},
		},
		DataClasses: []DataClass{
			{DataClass: "source_content", Sensitivity: "sensitive", Description: "Repository source text and snippets."},
			{DataClass: "admin_metadata", Sensitivity: "restricted", Description: "Tenant and workspace administration metadata."},
			{DataClass: "secret_risk", Sensitivity: "sensitive", Description: "Secret-risk and IAM evidence."},
			{DataClass: "audit_sensitive", Sensitivity: "sensitive", Description: "Detailed audit evidence."},
		},
		PermissionFamilies: []PermissionFamily{
			{
				Family:             "code_content",
				CapabilityPrefixes: []string{"code_search."},
				Action:             "code.content.read",
				DataClasses:        []string{"source_content"},
				ScopeLevels:        []string{"tenant", "workspace", "repository"},
				DefaultRoles:       []string{"developer", "owner"},
			},
		},
		BootstrapOwner: BootstrapOwnerPolicy{
			Role:                          "owner",
			StartsWithAdmin:               true,
			StartsWithSensitiveDataGrants: true,
			DelegableRoles:                []string{"developer"},
		},
		CustomPolicy: CustomPolicyPosture{
			Status: "deferred",
			Note:   "Custom policy language is intentionally deferred for v1.",
		},
	}
}

func TestBuildAttachesAuthorizationCatalog(t *testing.T) {
	t.Parallel()

	matrix := Matrix{Capabilities: []MatrixCapability{
		{
			Capability: "code_search.content_search",
			Tools:      []string{"find_code"},
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", MaxTruthLevel: "exact"},
			},
		},
	}}
	catalog, findings := BuildWithAuthorization(matrix, Overlay{Version: "v1"}, testAuthorizationCatalog(), Signals{
		MCPTools: map[string]bool{"find_code": true},
	})
	if len(findings) != 0 {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	if got, want := catalog.Authorization.BootstrapOwner.Role, "owner"; got != want {
		t.Fatalf("bootstrap owner role = %q, want %q", got, want)
	}
	if !catalog.Authorization.BootstrapOwner.StartsWithSensitiveDataGrants {
		t.Fatal("bootstrap owner must start with sensitive-data grants")
	}
	if len(catalog.Authorization.Roles) != 2 {
		t.Fatalf("roles = %d, want 2", len(catalog.Authorization.Roles))
	}
	entry := catalog.Entries[0]
	if got, want := entry.Authorization.Family, "code_content"; got != want {
		t.Fatalf("entry authz family = %q, want %q", got, want)
	}
	if got, want := entry.Authorization.Action, "code.content.read"; got != want {
		t.Fatalf("entry authz action = %q, want %q", got, want)
	}
	if got, want := entry.Authorization.DataClasses, []string{"source_content"}; !slices.Equal(got, want) {
		t.Fatalf("entry data classes = %v, want %v", got, want)
	}
	if got, want := entry.Authorization.DefaultRoles, []string{"developer", "owner"}; !slices.Equal(got, want) {
		t.Fatalf("entry default roles = %v, want %v", got, want)
	}
	if !entry.Authorization.SensitiveData {
		t.Fatal("source_content must mark the capability as sensitive-data-bearing")
	}
}

func TestBuildFlagsDefaultRoleMissingAuthorizationGrant(t *testing.T) {
	t.Parallel()

	matrix := Matrix{Capabilities: []MatrixCapability{
		{
			Capability: "code_search.content_search",
			Tools:      []string{"find_code"},
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", MaxTruthLevel: "exact"},
			},
		},
	}}
	authz := testAuthorizationCatalog()
	authz.Roles[0].Grants = nil

	_, findings := BuildWithAuthorization(matrix, Overlay{Version: "v1"}, authz, Signals{
		MCPTools: map[string]bool{"find_code": true},
	})
	if !hasAuthzFinding(findings, FindingInvalidAuthorizationReference, "code_content") {
		t.Fatalf("findings = %+v, want invalid default-role grant for code_content", findings)
	}
}

func TestBuildFlagsDefaultRoleMissingAuthorizationScope(t *testing.T) {
	t.Parallel()

	matrix := Matrix{Capabilities: []MatrixCapability{
		{
			Capability: "code_search.content_search",
			Tools:      []string{"find_code"},
			Profiles: map[string]MatrixProfile{
				"production": {Status: "supported", MaxTruthLevel: "exact"},
			},
		},
	}}
	authz := testAuthorizationCatalog()
	authz.Roles[0].Grants[0].ScopeLevels = []string{"tenant"}

	_, findings := BuildWithAuthorization(matrix, Overlay{Version: "v1"}, authz, Signals{
		MCPTools: map[string]bool{"find_code": true},
	})
	if !hasAuthzFinding(findings, FindingInvalidAuthorizationReference, "code_content") {
		t.Fatalf("findings = %+v, want invalid default-role scope for code_content", findings)
	}
}

func TestBuildFlagsMissingAuthorizationGrant(t *testing.T) {
	t.Parallel()

	matrix := testMatrix()
	authz := testAuthorizationCatalog()
	_, findings := BuildWithAuthorization(matrix, Overlay{Version: "v1"}, authz, Signals{
		MCPTools: map[string]bool{"find_code": true},
	})
	if !hasAuthzFinding(findings, FindingMissingAuthorizationGrant, "platform_impact.cloud_resource_list") {
		t.Fatalf("findings = %+v, want missing authorization for cloud resource capability", findings)
	}
}

func TestRealSpecsAuthorizationCatalogCoversEveryCapability(t *testing.T) {
	t.Parallel()

	specsDir := repoSpecsDir(t)
	matrix, err := LoadMatrix(specsDir)
	if err != nil {
		t.Fatalf("LoadMatrix: %v", err)
	}
	overlay, err := LoadOverlay(filepath.Join(specsDir, OverlayFileName))
	if err != nil {
		t.Fatalf("LoadOverlay: %v", err)
	}
	authz, err := LoadAuthorizationCatalog(filepath.Join(specsDir, AuthorizationFileName))
	if err != nil {
		t.Fatalf("LoadAuthorizationCatalog: %v", err)
	}
	catalog, findings := BuildWithAuthorization(matrix, overlay, authz, Signals{})
	for _, finding := range findings {
		if isAuthorizationFinding(finding.Kind) {
			t.Fatalf("real specs authorization finding: %+v", finding)
		}
	}
	for _, family := range requiredPermissionFamilies {
		if !authorizationCatalogHasFamily(catalog.Authorization, family) {
			t.Fatalf("required permission family %q missing from authorization catalog", family)
		}
	}
	if !catalog.Authorization.BootstrapOwner.StartsWithAdmin ||
		!catalog.Authorization.BootstrapOwner.StartsWithSensitiveDataGrants {
		t.Fatalf("bootstrap owner policy = %+v, want admin plus sensitive grants", catalog.Authorization.BootstrapOwner)
	}
	if !roleHasAction(catalog.Authorization, "tenant_admin", "admin.manage") {
		t.Fatal("tenant_admin must have admin.manage")
	}
	if roleHasSensitiveDataGrant(catalog.Authorization, "tenant_admin") {
		t.Fatal("tenant_admin must not include sensitive-data grants by default")
	}
	if !roleHasSensitiveDataGrant(catalog.Authorization, "sensitive_data_reader") {
		t.Fatal("sensitive_data_reader must carry sensitive-data grants separately")
	}
}

func hasAuthzFinding(findings []Finding, kind FindingKind, subject string) bool {
	for _, finding := range findings {
		if finding.Kind == kind && finding.Subject == subject {
			return true
		}
	}
	return false
}

func isAuthorizationFinding(kind FindingKind) bool {
	return kind == FindingMissingAuthorizationGrant ||
		kind == FindingInvalidAuthorizationReference ||
		kind == FindingStaleAuthorizationFamily
}
