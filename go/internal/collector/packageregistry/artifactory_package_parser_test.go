package packageregistry

import (
	"strings"
	"testing"
)

func TestParseArtifactoryPackageMetadataWrapsNativeNPMAndHosting(t *testing.T) {
	t.Parallel()

	metadata, err := ParseArtifactoryPackageMetadata(
		parserTestContext(EcosystemNPM, "https://artifactory.example.com/artifactory/api/npm/npm-local/"),
		[]byte(`{
			"provider": "jfrog",
			"repository": "npm-local",
			"repository_type": "local",
			"package_type": "npm",
			"upstream_id": "npmjs",
			"upstream_url": "https://user:secret@registry.npmjs.org/?token=secret&mirror=1",
			"metadata": {
				"name": "@Example/Web-App",
				"versions": {
					"1.0.0": {
						"dependencies": {"Left.Pad": "^1.3.0"},
						"dist": {
							"tarball": "https://user:secret@artifactory.example.com/artifactory/api/npm/npm-local/@example/web-app/-/web-app-1.0.0.tgz?token=secret&download=1",
							"integrity": "sha512-abcd"
						},
						"repository": {"type": "git", "url": "git+https://github.com/example/web-app.git"}
					}
				}
			}
		}`),
	)
	if err != nil {
		t.Fatalf("ParseArtifactoryPackageMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 1, 1, 1)
	if len(metadata.Hosting) != 1 {
		t.Fatalf("hosting observations = %d, want 1", len(metadata.Hosting))
	}
	if got := metadata.Packages[0].Identity.RawName; got != "@Example/Web-App" {
		t.Fatalf("package raw name = %q", got)
	}
	if got := metadata.Hosting[0].Provider; got != "jfrog" {
		t.Fatalf("hosting provider = %q", got)
	}
	if got := metadata.Hosting[0].Repository; got != "npm-local" {
		t.Fatalf("hosting repository = %q", got)
	}
	if got := metadata.Hosting[0].RepositoryType; got != "local" {
		t.Fatalf("hosting repository type = %q", got)
	}
	if got := metadata.Hosting[0].Ecosystem; got != EcosystemNPM {
		t.Fatalf("hosting ecosystem = %q", got)
	}
	if got := metadata.Hosting[0].UpstreamID; got != "npmjs" {
		t.Fatalf("hosting upstream id = %q", got)
	}
	if strings.Contains(metadata.Hosting[0].UpstreamURL, "secret") {
		t.Fatalf("hosting upstream URL was not sanitized: %q", metadata.Hosting[0].UpstreamURL)
	}
	if got := metadata.Artifacts[0].ArtifactURL; got != "https://artifactory.example.com/artifactory/api/npm/npm-local/@example/web-app/-/web-app-1.0.0.tgz?download=1" {
		t.Fatalf("artifact URL = %q", got)
	}
}

func TestParseArtifactoryPackageMetadataRejectsMismatchedPackageType(t *testing.T) {
	t.Parallel()

	_, err := ParseArtifactoryPackageMetadata(
		parserTestContext(EcosystemMaven, "https://artifactory.example.com/artifactory/libs-release-local/"),
		[]byte(`{
			"provider": "jfrog",
			"repository": "npm-local",
			"repository_type": "local",
			"package_type": "npm",
			"metadata": {"name": "left-pad", "versions": {}}
		}`),
	)
	if err == nil {
		t.Fatal("ParseArtifactoryPackageMetadata() error = nil, want package_type mismatch")
	}
	if got := err.Error(); !strings.Contains(got, `package_type "npm" does not match parser ecosystem "maven"`) {
		t.Fatalf("ParseArtifactoryPackageMetadata() error = %q, want package_type mismatch", got)
	}
}

func TestParseArtifactoryPackageMetadataSupportsStringNativeMetadata(t *testing.T) {
	t.Parallel()

	metadata, err := ParseArtifactoryPackageMetadata(
		parserTestContext(EcosystemMaven, "https://artifactory.example.com/artifactory/libs-release-local/"),
		[]byte(`{
			"provider": "jfrog",
			"repository": "libs-release-local",
			"repository_type": "remote",
			"package_type": "maven",
			"metadata": "<project><groupId>org.example</groupId><artifactId>core-api</artifactId><version>1.2.3</version></project>"
		}`),
	)
	if err != nil {
		t.Fatalf("ParseArtifactoryPackageMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 0, 1, 0)
	if got := metadata.Packages[0].Identity.Namespace; got != "org.example" {
		t.Fatalf("package namespace = %q", got)
	}
	if got := metadata.Hosting[0].Repository; got != "libs-release-local" {
		t.Fatalf("hosting repository = %q", got)
	}
}
