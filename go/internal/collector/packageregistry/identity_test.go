package packageregistry

import "testing"

func TestNormalizePackageIdentityUsesEcosystemRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   PackageIdentity
		want NormalizedPackageIdentity
	}{
		{
			name: "npm scoped package lowercases scope and name",
			in: PackageIdentity{
				Ecosystem: EcosystemNPM,
				Registry:  "https://registry.npmjs.org/",
				RawName:   "@NPMCorp/Package-Name",
			},
			want: NormalizedPackageIdentity{
				Ecosystem:      EcosystemNPM,
				Registry:       "registry.npmjs.org",
				RawName:        "@NPMCorp/Package-Name",
				NormalizedName: "@npmcorp/package-name",
				Namespace:      "npmcorp",
				PackageID:      "npm://registry.npmjs.org/@npmcorp/package-name",
			},
		},
		{
			name: "pypi applies pep 503 normalization",
			in: PackageIdentity{
				Ecosystem: EcosystemPyPI,
				Registry:  "https://pypi.org/simple",
				RawName:   "Friendly_Bard...Plugin",
			},
			want: NormalizedPackageIdentity{
				Ecosystem:      EcosystemPyPI,
				Registry:       "pypi.org/simple",
				RawName:        "Friendly_Bard...Plugin",
				NormalizedName: "friendly-bard-plugin",
				PackageID:      "pypi://pypi.org/simple/friendly-bard-plugin",
			},
		},
		{
			name: "bare registry input lowercases only host portion",
			in: PackageIdentity{
				Ecosystem: EcosystemPyPI,
				Registry:  "Registry.PyPI.ORG/Simple/Private",
				RawName:   "Friendly_Bard",
			},
			want: NormalizedPackageIdentity{
				Ecosystem:      EcosystemPyPI,
				Registry:       "registry.pypi.org/Simple/Private",
				RawName:        "Friendly_Bard",
				NormalizedName: "friendly-bard",
				PackageID:      "pypi://registry.pypi.org/Simple/Private/friendly-bard",
			},
		},
		{
			name: "go module preserves module path case",
			in: PackageIdentity{
				Ecosystem: EcosystemGoModule,
				Registry:  "https://proxy.golang.org",
				RawName:   "Example.com/Org/Module/v2",
			},
			want: NormalizedPackageIdentity{
				Ecosystem:      EcosystemGoModule,
				Registry:       "proxy.golang.org",
				RawName:        "Example.com/Org/Module/v2",
				NormalizedName: "Example.com/Org/Module/v2",
				Namespace:      "Example.com/Org/Module",
				PackageID:      "gomod://proxy.golang.org/Example.com/Org/Module/v2",
			},
		},
		{
			name: "maven preserves gav case and namespace is group id",
			in: PackageIdentity{
				Ecosystem:  EcosystemMaven,
				Registry:   "https://repo.maven.apache.org/maven2/",
				RawName:    "Maven-Core",
				Namespace:  "Org.Apache.Maven",
				Classifier: "sources",
			},
			want: NormalizedPackageIdentity{
				Ecosystem:      EcosystemMaven,
				Registry:       "repo.maven.apache.org/maven2",
				RawName:        "Maven-Core",
				NormalizedName: "Maven-Core",
				Namespace:      "Org.Apache.Maven",
				Classifier:     "sources",
				PackageID:      "maven://repo.maven.apache.org/maven2/Org.Apache.Maven:Maven-Core",
			},
		},
		{
			name: "nuget lowercases package id",
			in: PackageIdentity{
				Ecosystem: EcosystemNuGet,
				Registry:  "https://api.nuget.org/v3/index.json",
				RawName:   "Newtonsoft.Json",
			},
			want: NormalizedPackageIdentity{
				Ecosystem:      EcosystemNuGet,
				Registry:       "api.nuget.org/v3/index.json",
				RawName:        "Newtonsoft.Json",
				NormalizedName: "newtonsoft.json",
				PackageID:      "nuget://api.nuget.org/v3/index.json/newtonsoft.json",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := NormalizePackageIdentity(tt.in)
			if err != nil {
				t.Fatalf("NormalizePackageIdentity() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("NormalizePackageIdentity() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizePackageIdentityRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   PackageIdentity
	}{
		{name: "missing ecosystem", in: PackageIdentity{Registry: "registry.npmjs.org", RawName: "react"}},
		{name: "missing registry", in: PackageIdentity{Ecosystem: EcosystemNPM, RawName: "react"}},
		{name: "missing package name", in: PackageIdentity{Ecosystem: EcosystemNPM, Registry: "registry.npmjs.org"}},
		{name: "maven missing group", in: PackageIdentity{Ecosystem: EcosystemMaven, Registry: "repo.maven.apache.org", RawName: "maven-core"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := NormalizePackageIdentity(tt.in); err == nil {
				t.Fatal("NormalizePackageIdentity() error = nil, want error")
			}
		})
	}
}
