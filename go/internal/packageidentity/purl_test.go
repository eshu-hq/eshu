package packageidentity

import "testing"

func TestPackageIDFromPURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		purl string
		want string
	}{
		{
			name: "npm version qualified strips to canonical package id",
			purl: "pkg:npm/lodash@4.17.11",
			want: "npm://registry.npmjs.org/lodash",
		},
		{
			name: "npm versionless matches the version qualified identity",
			purl: "pkg:npm/lodash",
			want: "npm://registry.npmjs.org/lodash",
		},
		{
			name: "npm scoped name folds the scope into the name",
			purl: "pkg:npm/%40babel/core@7.0.0",
			want: "npm://registry.npmjs.org/@babel/core",
		},
		{
			name: "pypi normalizes separators",
			purl: "pkg:pypi/Django@4.2",
			want: "pypi://pypi.org/simple/django",
		},
		{
			name: "maven namespace and artifact",
			purl: "pkg:maven/org.apache.logging.log4j/log4j-core@2.14.1",
			want: "maven://repo.maven.apache.org/maven2/org.apache.logging.log4j:log4j-core",
		},
		{
			name: "go module preserves full module path",
			purl: "pkg:golang/golang.org/x/text@v0.3.7",
			want: "gomod://proxy.golang.org/golang.org/x/text",
		},
		{
			name: "os package uses purl distro namespace",
			purl: "pkg:deb/debian/openssl@3.0.11-1~deb12u2",
			want: "os://debian/openssl",
		},
		{
			name: "blank input is not an error",
			purl: "",
			want: "",
		},
		{
			name: "non purl input is not an error",
			purl: "lodash@4.17.11",
			want: "",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := PackageIDFromPURL(tc.purl)
			if err != nil {
				t.Fatalf("PackageIDFromPURL(%q) error = %v", tc.purl, err)
			}
			if got != tc.want {
				t.Fatalf("PackageIDFromPURL(%q) = %q, want %q", tc.purl, got, tc.want)
			}
		})
	}
}

// TestPackageIDFromPURLMatchesNormalizeIdentity guarantees the purl shortcut
// produces the same PackageID as building a RawIdentity by hand, which is how
// the vulnerability-intelligence collector derives the affected_package
// package_id. If the two ever diverge, SBOM components stop correlating with
// vulnerability facts.
func TestPackageIDFromPURLMatchesNormalizeIdentity(t *testing.T) {
	t.Parallel()

	fromPURL, err := PackageIDFromPURL("pkg:npm/lodash@4.17.11")
	if err != nil {
		t.Fatalf("PackageIDFromPURL error = %v", err)
	}
	manual, err := Normalize(RawIdentity{
		Ecosystem: EcosystemNPM,
		Registry:  DefaultRegistry(EcosystemNPM),
		RawName:   "lodash",
	})
	if err != nil {
		t.Fatalf("Normalize error = %v", err)
	}
	if fromPURL != manual.PackageID {
		t.Fatalf("PackageIDFromPURL = %q, want %q (manual identity)", fromPURL, manual.PackageID)
	}
}
