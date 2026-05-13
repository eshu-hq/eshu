package packageregistry

import (
	"strings"
	"testing"
	"time"
)

func TestParseNPMPackumentMetadataBuildsObservations(t *testing.T) {
	t.Parallel()

	metadata, err := ParseNPMPackumentMetadata(parserTestContext(EcosystemNPM, "https://registry.npmjs.org/"), []byte(`{
		"name": "@Example/Web-App",
		"versions": {
			"1.0.0": {
				"deprecated": "use 2.x",
				"dependencies": {"Left.Pad": "^1.3.0"},
				"devDependencies": {"Typescript": "~5.5.0"},
				"optionalDependencies": {"FSEvents": "^2.3.3"},
				"peerDependencies": {"React": ">=18"},
				"dist": {
					"tarball": "https://token:secret@registry.npmjs.org/@example/web-app/-/web-app-1.0.0.tgz?access_token=secret&cache=ok",
					"integrity": "sha512-abcd",
					"shasum": "deadbeef"
				},
				"repository": {"type": "git", "url": "git+https://github.com/example/web-app.git"},
				"homepage": "https://example.com/web-app"
			}
		}
	}`))
	if err != nil {
		t.Fatalf("ParseNPMPackumentMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 4, 1, 2)
	if got := metadata.Packages[0].Identity.RawName; got != "@Example/Web-App" {
		t.Fatalf("package raw name = %q", got)
	}
	version := metadata.Versions[0]
	if !version.Deprecated {
		t.Fatalf("version Deprecated = false, want true")
	}
	if got := version.ArtifactURLs[0]; got != "https://registry.npmjs.org/@example/web-app/-/web-app-1.0.0.tgz?cache=ok" {
		t.Fatalf("artifact URL = %q", got)
	}
	dependencies := dependencyTypes(metadata.Dependencies)
	for _, want := range []string{"runtime", "development", "optional", "peer"} {
		if !dependencies[want] {
			t.Fatalf("missing dependency type %q in %#v", want, metadata.Dependencies)
		}
	}
	if !metadata.Dependencies[2].Optional {
		t.Fatalf("optional dependency Optional = false, want true")
	}
}

func TestParsePyPIProjectMetadataBuildsObservations(t *testing.T) {
	t.Parallel()

	metadata, err := ParsePyPIProjectMetadata(parserTestContext(EcosystemPyPI, "https://pypi.org/pypi/"), []byte(`{
		"info": {
			"name": "Friendly_Bard",
			"version": "0.9.0",
			"requires_dist": [
				"Requests-OAuthlib >= 2 ; python_version >= '3.11'",
				"colorama ; extra == 'windows'"
			],
			"project_urls": {
				"Source": "https://user:secret@github.com/example/friendly-bard?token=secret",
				"Homepage": "https://example.com/friendly"
			}
		},
		"releases": {
			"0.9.0": [{
				"packagetype": "bdist_wheel",
				"url": "https://files.pythonhosted.org/packages/friendly-0.9.0.whl?sig=secret&download=1",
				"filename": "friendly-0.9.0-py3-none-any.whl",
				"size": 1234,
				"upload_time_iso_8601": "2026-05-11T09:00:00Z",
				"yanked": true,
				"digests": {"sha256": "abc123"}
			}]
		}
	}`))
	if err != nil {
		t.Fatalf("ParsePyPIProjectMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 2, 1, 2)
	if got := metadata.Packages[0].Identity.RawName; got != "Friendly_Bard" {
		t.Fatalf("package raw name = %q", got)
	}
	if !metadata.Versions[0].Yanked {
		t.Fatalf("version Yanked = false, want true")
	}
	if got := metadata.Artifacts[0].ArtifactURL; got != "https://files.pythonhosted.org/packages/friendly-0.9.0.whl?download=1" {
		t.Fatalf("artifact URL = %q", got)
	}
	if got := metadata.Dependencies[0].Dependency.RawName; got != "Requests-OAuthlib" {
		t.Fatalf("dependency raw name = %q", got)
	}
	if got := metadata.Dependencies[0].Marker; got != "python_version >= '3.11'" {
		t.Fatalf("dependency marker = %q", got)
	}
}

func TestParseGenericPackageMetadataBuildsHostingAndDeduplicatesArtifacts(t *testing.T) {
	t.Parallel()

	metadata, err := ParseGenericPackageMetadata(parserTestContext(EcosystemGeneric, "https://jfrog.example/artifactory/"), []byte(`{
		"provider": "artifactory",
		"repository": "generic-local",
		"repository_type": "local",
		"name": "team/tool",
		"namespace": "team",
		"version": "2026.05.12",
		"visibility": "private",
		"source_url": "https://user:secret@git.example/team/tool?token=secret",
		"artifacts": [
			{"key": "team/tool/2026.05.12/tool-linux-amd64.tar.gz", "url": "https://jfrog.example/artifactory/generic-local/team/tool/2026.05.12/tool-linux-amd64.tar.gz", "size": 99, "sha256": "abc"},
			{"key": "team/tool/2026.05.12/tool-linux-amd64.tar.gz", "url": "https://jfrog.example/artifactory/generic-local/team/tool/2026.05.12/tool-linux-amd64.tar.gz", "size": 99, "sha256": "abc"}
		]
	}`))
	if err != nil {
		t.Fatalf("ParseGenericPackageMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 0, 1, 1)
	if len(metadata.Hosting) != 1 {
		t.Fatalf("hosting observations = %d, want 1", len(metadata.Hosting))
	}
	if got := metadata.Packages[0].Visibility; got != VisibilityPrivate {
		t.Fatalf("visibility = %q, want %q", got, VisibilityPrivate)
	}
	if got := metadata.SourceHints[0].RawURL; got != "https://git.example/team/tool" {
		t.Fatalf("source URL = %q", got)
	}
}

func TestParseMavenPackageMetadataBuildsObservations(t *testing.T) {
	t.Parallel()

	metadata, err := ParseMavenPackageMetadata(parserTestContext(EcosystemMaven, "https://repo.maven.apache.org/maven2"), []byte(`
		<project>
			<modelVersion>4.0.0</modelVersion>
			<groupId>org.Example</groupId>
			<artifactId>Core-API</artifactId>
			<version>1.2.3</version>
			<packaging>jar</packaging>
			<url>https://example.com/core-api</url>
			<scm>
				<url>scm:git:https://user:secret@github.com/example/core-api.git?token=secret</url>
			</scm>
			<dependencies>
				<dependency>
					<groupId>org.slf4j</groupId>
					<artifactId>slf4j-api</artifactId>
					<version>[2.0,3.0)</version>
					<scope>compile</scope>
				</dependency>
				<dependency>
					<groupId>junit</groupId>
					<artifactId>junit</artifactId>
					<version>4.13.2</version>
					<scope>test</scope>
					<optional>true</optional>
				</dependency>
			</dependencies>
		</project>`))
	if err != nil {
		t.Fatalf("ParseMavenPackageMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 2, 1, 2)
	if got := metadata.Packages[0].Identity.Namespace; got != "org.Example" {
		t.Fatalf("package namespace = %q", got)
	}
	if got := metadata.Packages[0].Identity.RawName; got != "Core-API" {
		t.Fatalf("package raw name = %q", got)
	}
	if got := metadata.Dependencies[0].Dependency.Namespace; got != "org.slf4j" {
		t.Fatalf("dependency namespace = %q", got)
	}
	if got := metadata.Dependencies[0].DependencyType; got != "compile" {
		t.Fatalf("dependency type = %q", got)
	}
	if !metadata.Dependencies[1].Optional {
		t.Fatalf("test dependency Optional = false, want true")
	}
	if got := metadata.Artifacts[0].ArtifactKey; got != "org/Example/Core-API/1.2.3/Core-API-1.2.3.jar" {
		t.Fatalf("artifact key = %q", got)
	}
}

func TestParseNuGetPackageMetadataBuildsObservations(t *testing.T) {
	t.Parallel()

	metadata, err := ParseNuGetPackageMetadata(parserTestContext(EcosystemNuGet, "https://api.nuget.org/v3/index.json"), []byte(`
		<package>
			<metadata>
				<id>Friendly.Json</id>
				<version>13.0.3</version>
				<projectUrl>https://example.com/friendly</projectUrl>
				<repository type="git" url="https://user:secret@github.com/example/friendly-json.git?token=secret" />
				<dependencies>
					<group targetFramework="net8.0">
						<dependency id="System.Text.Json" version="[8.0.0,)" />
					</group>
					<dependency id="Newtonsoft.Json" version="[13.0.1,)" />
				</dependencies>
			</metadata>
		</package>`))
	if err != nil {
		t.Fatalf("ParseNuGetPackageMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 2, 1, 2)
	if got := metadata.Packages[0].Identity.RawName; got != "Friendly.Json" {
		t.Fatalf("package raw name = %q", got)
	}
	if got := metadata.Dependencies[0].TargetFramework; got != "net8.0" {
		t.Fatalf("dependency target framework = %q", got)
	}
	if got := metadata.Artifacts[0].ArtifactKey; got != "friendly.json.13.0.3.nupkg" {
		t.Fatalf("artifact key = %q", got)
	}
}

func TestParseGoModuleProxyMetadataBuildsObservations(t *testing.T) {
	t.Parallel()

	metadata, err := ParseGoModuleProxyMetadata(parserTestContext(EcosystemGoModule, "https://proxy.golang.org"), []byte(`{
		"module": "golang.org/x/mod",
		"info": {"Version": "v0.20.0", "Time": "2026-05-10T08:30:00Z"},
		"mod": "module golang.org/x/mod\n\nrequire golang.org/x/tools v0.24.0\nrequire (\n\tgithub.com/google/go-cmp v0.6.0\n\tgolang.org/x/sync v0.8.0 // indirect\n)\n",
		"zip_url": "https://proxy.golang.org/golang.org/x/mod/@v/v0.20.0.zip?token=secret",
		"sum": "h1:abc"
	}`))
	if err != nil {
		t.Fatalf("ParseGoModuleProxyMetadata() error = %v", err)
	}

	requireObservationCounts(t, metadata, 1, 1, 3, 1, 0)
	if got := metadata.Packages[0].Identity.RawName; got != "golang.org/x/mod" {
		t.Fatalf("package raw name = %q", got)
	}
	if got := metadata.Dependencies[2].Dependency.RawName; got != "golang.org/x/tools" {
		t.Fatalf("dependency raw name = %q", got)
	}
	if got := dependencyTypeByName(metadata.Dependencies)["golang.org/x/sync"]; got != "indirect" {
		t.Fatalf("golang.org/x/sync dependency type = %q", got)
	}
	if got := metadata.Artifacts[0].Hashes["sum"]; got != "h1:abc" {
		t.Fatalf("artifact sum = %q", got)
	}
}

func TestMetadataParsersRejectMalformedDocuments(t *testing.T) {
	t.Parallel()

	ctx := parserTestContext(EcosystemNPM, "registry.npmjs.org")
	if _, err := ParseNPMPackumentMetadata(ctx, []byte(`{"name":`)); err == nil {
		t.Fatal("ParseNPMPackumentMetadata() error = nil, want malformed JSON error")
	}
	if _, err := ParseMavenPackageMetadata(parserTestContext(EcosystemMaven, "repo.maven.apache.org"), []byte(`<project>`)); err == nil {
		t.Fatal("ParseMavenPackageMetadata() error = nil, want malformed XML error")
	}
	if _, err := ParseNuGetPackageMetadata(parserTestContext(EcosystemNuGet, "api.nuget.org"), []byte(`<package>`)); err == nil {
		t.Fatal("ParseNuGetPackageMetadata() error = nil, want malformed XML error")
	}
	if _, err := ParseGoModuleProxyMetadata(parserTestContext(EcosystemGoModule, "proxy.golang.org"), []byte(`{"module":`)); err == nil {
		t.Fatal("ParseGoModuleProxyMetadata() error = nil, want malformed JSON error")
	}
	if _, err := ParseGenericPackageMetadata(parserTestContext(EcosystemGeneric, "registry.example"), []byte(`{"name":`)); err == nil {
		t.Fatal("ParseGenericPackageMetadata() error = nil, want malformed JSON error")
	}
}

func parserTestContext(ecosystem Ecosystem, registry string) MetadataParserContext {
	return MetadataParserContext{
		Ecosystem:           ecosystem,
		Registry:            registry,
		ScopeID:             string(ecosystem) + "://scope",
		GenerationID:        "fixture-generation",
		CollectorInstanceID: "fixture-collector",
		FencingToken:        21,
		ObservedAt:          time.Date(2026, 5, 12, 12, 0, 0, 0, time.UTC),
		SourceURI:           registry + "fixture",
	}
}

func requireObservationCounts(
	t *testing.T,
	metadata ParsedMetadata,
	packages int,
	versions int,
	dependencies int,
	artifacts int,
	sourceHints int,
) {
	t.Helper()

	if len(metadata.Packages) != packages {
		t.Fatalf("packages = %d, want %d", len(metadata.Packages), packages)
	}
	if len(metadata.Versions) != versions {
		t.Fatalf("versions = %d, want %d", len(metadata.Versions), versions)
	}
	if len(metadata.Dependencies) != dependencies {
		t.Fatalf("dependencies = %d, want %d", len(metadata.Dependencies), dependencies)
	}
	if len(metadata.Artifacts) != artifacts {
		t.Fatalf("artifacts = %d, want %d", len(metadata.Artifacts), artifacts)
	}
	if len(metadata.SourceHints) != sourceHints {
		t.Fatalf("sourceHints = %d, want %d", len(metadata.SourceHints), sourceHints)
	}
	for _, artifact := range metadata.Artifacts {
		if strings.Contains(artifact.ArtifactURL, "secret") {
			t.Fatalf("artifact URL was not sanitized: %q", artifact.ArtifactURL)
		}
	}
}

func dependencyTypes(dependencies []PackageDependencyObservation) map[string]bool {
	types := make(map[string]bool)
	for _, dependency := range dependencies {
		types[dependency.DependencyType] = true
	}
	return types
}

func dependencyTypeByName(dependencies []PackageDependencyObservation) map[string]string {
	types := make(map[string]string)
	for _, dependency := range dependencies {
		types[dependency.Dependency.RawName] = dependency.DependencyType
	}
	return types
}
