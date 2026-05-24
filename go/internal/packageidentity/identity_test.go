package packageidentity

import "testing"

func TestNormalizePackageIdentityUsesCanonicalEcosystemRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RawIdentity
		want Identity
	}{
		{
			name: "npm scoped package lowercases scope and encodes purl",
			in: RawIdentity{
				Ecosystem:        EcosystemNPM,
				Registry:         "https://registry.npmjs.org/",
				RawName:          "@NPMCorp/Package-Name",
				Version:          "1.2.3",
				SourcePath:       "package-lock.json",
				SourceSpecificID: "npm-lock:@NPMCorp/Package-Name",
			},
			want: Identity{
				Ecosystem:        EcosystemNPM,
				Registry:         "registry.npmjs.org",
				RawName:          "@NPMCorp/Package-Name",
				NormalizedName:   "@npmcorp/package-name",
				Namespace:        "npmcorp",
				Version:          "1.2.3",
				PURL:             "pkg:npm/%40npmcorp/package-name@1.2.3",
				BOMRef:           "pkg:npm/%40npmcorp/package-name@1.2.3",
				PackageManager:   "npm",
				SourcePath:       "package-lock.json",
				SourceSpecificID: "npm-lock:@NPMCorp/Package-Name",
				PackageID:        "npm://registry.npmjs.org/@npmcorp/package-name",
			},
		},
		{
			name: "pypi applies pep 503 normalization",
			in: RawIdentity{
				Ecosystem: EcosystemPyPI,
				Registry:  "https://pypi.org/simple",
				RawName:   "Friendly_Bard...Plugin",
				Version:   "2.0.0",
			},
			want: Identity{
				Ecosystem:      EcosystemPyPI,
				Registry:       "pypi.org/simple",
				RawName:        "Friendly_Bard...Plugin",
				NormalizedName: "friendly-bard-plugin",
				Version:        "2.0.0",
				PURL:           "pkg:pypi/friendly-bard-plugin@2.0.0",
				BOMRef:         "pkg:pypi/friendly-bard-plugin@2.0.0",
				PackageManager: "pypi",
				PackageID:      "pypi://pypi.org/simple/friendly-bard-plugin",
			},
		},
		{
			name: "maven normalizes group artifact purl",
			in: RawIdentity{
				Ecosystem:  EcosystemMaven,
				Registry:   "https://repo.maven.apache.org/maven2/",
				RawName:    "Maven-Core",
				Namespace:  "Org.Apache.Maven",
				Version:    "3.9.9",
				Classifier: "sources",
			},
			want: Identity{
				Ecosystem:      EcosystemMaven,
				Registry:       "repo.maven.apache.org/maven2",
				RawName:        "Maven-Core",
				NormalizedName: "Maven-Core",
				Namespace:      "Org.Apache.Maven",
				Version:        "3.9.9",
				Classifier:     "sources",
				PURL:           "pkg:maven/Org.Apache.Maven/Maven-Core@3.9.9",
				BOMRef:         "pkg:maven/Org.Apache.Maven/Maven-Core@3.9.9",
				PackageManager: "maven",
				PackageID:      "maven://repo.maven.apache.org/maven2/Org.Apache.Maven:Maven-Core",
			},
		},
		{
			name: "go module preserves module path and aliases go ecosystem",
			in: RawIdentity{
				Ecosystem: "go",
				Registry:  "https://proxy.golang.org",
				RawName:   "Example.com/Org/Module/v2",
				Version:   "v2.1.0",
			},
			want: Identity{
				Ecosystem:      EcosystemGoModule,
				Registry:       "proxy.golang.org",
				RawName:        "Example.com/Org/Module/v2",
				NormalizedName: "Example.com/Org/Module/v2",
				Namespace:      "Example.com/Org/Module",
				Version:        "v2.1.0",
				PURL:           "pkg:golang/Example.com/Org/Module/v2@v2.1.0",
				BOMRef:         "pkg:golang/Example.com/Org/Module/v2@v2.1.0",
				PackageManager: "gomod",
				PackageID:      "gomod://proxy.golang.org/Example.com/Org/Module/v2",
			},
		},
		{
			name: "composer lowercases vendor package",
			in: RawIdentity{
				Ecosystem: EcosystemComposer,
				Registry:  "https://repo.packagist.org",
				RawName:   "Symfony/Console",
				Version:   "7.0.0",
			},
			want: Identity{
				Ecosystem:      EcosystemComposer,
				Registry:       "repo.packagist.org",
				RawName:        "Symfony/Console",
				NormalizedName: "symfony/console",
				Namespace:      "symfony",
				Version:        "7.0.0",
				PURL:           "pkg:composer/symfony/console@7.0.0",
				BOMRef:         "pkg:composer/symfony/console@7.0.0",
				PackageManager: "composer",
				PackageID:      "composer://repo.packagist.org/symfony/console",
			},
		},
		{
			name: "rubygems lowercases gem name",
			in: RawIdentity{
				Ecosystem: "ruby",
				Registry:  "https://rubygems.org",
				RawName:   "Rails",
				Version:   "7.1.0",
			},
			want: Identity{
				Ecosystem:      EcosystemRubyGems,
				Registry:       "rubygems.org",
				RawName:        "Rails",
				NormalizedName: "rails",
				Version:        "7.1.0",
				PURL:           "pkg:gem/rails@7.1.0",
				BOMRef:         "pkg:gem/rails@7.1.0",
				PackageManager: "rubygems",
				PackageID:      "rubygems://rubygems.org/rails",
			},
		},
		{
			name: "cargo lowercases crate name",
			in: RawIdentity{
				Ecosystem: "crates.io",
				Registry:  "https://crates.io",
				RawName:   "Serde_JSON",
				Version:   "1.0.116",
			},
			want: Identity{
				Ecosystem:      EcosystemCargo,
				Registry:       "crates.io",
				RawName:        "Serde_JSON",
				NormalizedName: "serde_json",
				Version:        "1.0.116",
				PURL:           "pkg:cargo/serde_json@1.0.116",
				BOMRef:         "pkg:cargo/serde_json@1.0.116",
				PackageManager: "cargo",
				PackageID:      "cargo://crates.io/serde_json",
			},
		},
		{
			name: "nuget lowercases package id",
			in: RawIdentity{
				Ecosystem: EcosystemNuGet,
				Registry:  "https://api.nuget.org/v3/index.json",
				RawName:   "Newtonsoft.Json",
				Version:   "13.0.3",
			},
			want: Identity{
				Ecosystem:      EcosystemNuGet,
				Registry:       "api.nuget.org/v3/index.json",
				RawName:        "Newtonsoft.Json",
				NormalizedName: "newtonsoft.json",
				Version:        "13.0.3",
				PURL:           "pkg:nuget/newtonsoft.json@13.0.3",
				BOMRef:         "pkg:nuget/newtonsoft.json@13.0.3",
				PackageManager: "nuget",
				PackageID:      "nuget://api.nuget.org/v3/index.json/newtonsoft.json",
			},
		},
		{
			name: "os package keeps distro registry and package manager",
			in: RawIdentity{
				Ecosystem:      "debian",
				Registry:       "debian:12",
				RawName:        "OpenSSL",
				Version:        "3.0.11-1~deb12u2",
				PackageManager: "deb",
			},
			want: Identity{
				Ecosystem:      EcosystemOS,
				Registry:       "debian:12",
				RawName:        "OpenSSL",
				NormalizedName: "openssl",
				Version:        "3.0.11-1~deb12u2",
				PURL:           "pkg:deb/debian:12/openssl@3.0.11-1~deb12u2",
				BOMRef:         "pkg:deb/debian:12/openssl@3.0.11-1~deb12u2",
				PackageManager: "deb",
				PackageID:      "os://debian:12/openssl",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Normalize(tt.in)
			if err != nil {
				t.Fatalf("Normalize() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("Normalize() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestNormalizePackageIdentityPreservesExplicitBOMRef(t *testing.T) {
	t.Parallel()

	got, err := Normalize(RawIdentity{
		Ecosystem: EcosystemNPM,
		Registry:  "registry.npmjs.org",
		RawName:   "react",
		Version:   "18.2.0",
		BOMRef:    "pkg:custom/react-component",
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.BOMRef != "pkg:custom/react-component" {
		t.Fatalf("BOMRef = %q, want explicit bom-ref", got.BOMRef)
	}
}

func TestNormalizePackageIdentityDoesNotDuplicateTwoSegmentNamespace(t *testing.T) {
	t.Parallel()

	got, err := Normalize(RawIdentity{
		Ecosystem: EcosystemComposer,
		Registry:  "repo.packagist.org",
		Namespace: "symfony",
		RawName:   "symfony/console",
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if got.NormalizedName != "symfony/console" {
		t.Fatalf("NormalizedName = %q, want symfony/console", got.NormalizedName)
	}
	if got.PackageID != "composer://repo.packagist.org/symfony/console" {
		t.Fatalf("PackageID = %q, want canonical composer package ID", got.PackageID)
	}
}

func TestNormalizePackageIdentityRejectsMissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   RawIdentity
	}{
		{name: "missing ecosystem", in: RawIdentity{Registry: "registry.npmjs.org", RawName: "react"}},
		{name: "missing registry", in: RawIdentity{Ecosystem: EcosystemNPM, RawName: "react"}},
		{name: "missing package name", in: RawIdentity{Ecosystem: EcosystemNPM, Registry: "registry.npmjs.org"}},
		{name: "maven missing group", in: RawIdentity{Ecosystem: EcosystemMaven, Registry: "repo.maven.apache.org", RawName: "maven-core"}},
		{name: "composer missing vendor", in: RawIdentity{Ecosystem: EcosystemComposer, Registry: "repo.packagist.org", RawName: "console"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Normalize(tt.in); err == nil {
				t.Fatal("Normalize() error = nil, want error")
			}
		})
	}
}
