package collector

// TestGitEmittedFactEnvelopeSchemaVersionGuard is the regression guard that
// prevents a repeat of the workflow-image-evidence schema regression (#3353).
//
// Any fact kind emitted by the git collector that belongs to a registered
// schema-version family MUST carry the registry version in SchemaVersion.
// This test exercises every git emitter path that produces such a fact and
// asserts the version is present and matches the central registry. A new
// emitter that calls factEnvelope without stamping SchemaVersion will cause
// this test to fail immediately.
//
// Covered families (git collector only):
//   - CICD (ci.workflow_image_evidence — fixed in #3353)
//   - Documentation (documentation_source, documentation_document,
//     documentation_section, documentation_link)
//   - Observability (observability_source_instance,
//     observability_declared_folders, observability_coverage_warnings)

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestGitEmittedFactEnvelopeSchemaVersionGuard(t *testing.T) {
	t.Parallel()

	// registry is the authoritative map of fact kind -> required SchemaVersion.
	// Computed once so every sub-test uses the same snapshot of the registry.
	registry := facts.SupportedSchemaVersions()

	t.Run("CICD", func(t *testing.T) {
		t.Parallel()
		envelopes := emitCICDWorkflowImageFacts(t)
		assertSchemaVersionStamped(t, envelopes, registry, facts.CICDWorkflowImageEvidenceFactKind)
	})

	t.Run("Documentation", func(t *testing.T) {
		t.Parallel()
		envelopes := emitDocumentationFacts(t)
		for _, kind := range []string{
			facts.DocumentationSourceFactKind,
			facts.DocumentationDocumentFactKind,
			facts.DocumentationSectionFactKind,
		} {
			assertSchemaVersionStamped(t, envelopes, registry, kind)
		}
	})

	t.Run("Observability", func(t *testing.T) {
		t.Parallel()
		envelopes := emitObservabilityFacts(t)
		for _, kind := range []string{
			facts.ObservabilitySourceInstanceFactKind,
			facts.ObservabilityDeclaredFolderFactKind,
			facts.ObservabilityCoverageWarningFactKind,
		} {
			assertSchemaVersionStamped(t, envelopes, registry, kind)
		}
	})
}

// TestGitEmittedFactEnvelopeSchemaVersionGuardAllRegistered is the sweep that
// checks every emitted envelope against the registry without targeting a
// specific kind. It catches a new registered kind that a future emitter
// forgets to stamp, even if it is not yet in the explicit list above.
func TestGitEmittedFactEnvelopeSchemaVersionGuardAllRegistered(t *testing.T) {
	t.Parallel()

	registry := facts.SupportedSchemaVersions()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "README.md"), markdownDocFixture)
	writeCollectorTestFile(
		t,
		filepath.Join(repoPath, ".github", "workflows", "deploy.yml"),
		workflowImageFixture,
	)

	repo := testCollectorRepositoryMetadata(repoPath)
	observedAt := time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 2,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "README.md",
			Digest:       "sha256:readme",
			Language:     "markdown",
		}},
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: ".github/workflows/deploy.yml",
			Body:         workflowImageFixture,
			ArtifactType: "github_actions_workflow",
			Language:     "yaml",
		}},
		FileData: []map[string]any{{
			"path": filepath.Join(repoPath, "observability.yaml"),
			"lang": "yaml",
			"observability_declared_folders": []map[string]any{{
				"name":                     "folder.checkout",
				"line_number":              1,
				"source_class":             "declared",
				"source_kind":              "kubernetes",
				"declaration_kind":         "grafana_folder_resource",
				"folder_uid":               "checkout",
				"folder_title_fingerprint": "folder:abc",
				"outcome":                  "exact",
			}},
			"observability_coverage_warnings": []map[string]any{{
				"name":         "warning.unsupported.plugin",
				"line_number":  2,
				"source_class": "declared",
				"source_kind":  "kubernetes",
				"warning_kind": "unsupported_datasource_type",
				"outcome":      "unsupported",
			}},
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-schema-guard", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)

	var violations []string
	for _, env := range envelopes {
		want, registered := registry[env.FactKind]
		if !registered {
			continue
		}
		if env.SchemaVersion != want {
			violations = append(violations, env.FactKind+": SchemaVersion = "+env.SchemaVersion+", want "+want)
		}
	}
	if len(violations) > 0 {
		t.Errorf("git-emitted facts with registered fact kinds carry wrong SchemaVersion (factEnvelope must stamp the version after building the envelope):")
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
	}
}

// emitCICDWorkflowImageFacts builds a streaming generation that triggers
// ci.workflow_image_evidence emission and returns the collected envelopes.
func emitCICDWorkflowImageFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, ".github", "workflows", "deploy.yml"), workflowImageFixture)
	repo := testCollectorRepositoryMetadata(repoPath)
	observedAt := time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFiles: []ContentFileSnapshot{{
			RelativePath: ".github/workflows/deploy.yml",
			Body:         workflowImageFixture,
			ArtifactType: "github_actions_workflow",
			Language:     "yaml",
		}},
	}
	collected := buildStreamingGeneration(repoPath, repo, "run-cicd", observedAt, snapshot, false)
	return drainFactChannel(collected.Facts)
}

// emitDocumentationFacts builds a streaming generation that triggers
// documentation_source, documentation_document, documentation_section, and
// documentation_link emission.
func emitDocumentationFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "README.md"), markdownDocFixture)
	repo := testCollectorRepositoryMetadata(repoPath)
	observedAt := time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "README.md",
			Digest:       "sha256:readme",
			Language:     "markdown",
		}},
	}
	collected := buildStreamingGeneration(repoPath, repo, "run-doc", observedAt, snapshot, false)
	return drainFactChannel(collected.Facts)
}

// emitObservabilityFacts builds a streaming generation that triggers
// observability_source_instance, observability_declared_folders, and
// observability_coverage_warnings emission.
func emitObservabilityFacts(t *testing.T) []facts.Envelope {
	t.Helper()
	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	observedAt := time.Date(2026, time.June, 20, 0, 0, 0, 0, time.UTC)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		FileData: []map[string]any{{
			"path": filepath.Join(repoPath, "observability.yaml"),
			"lang": "yaml",
			"observability_declared_folders": []map[string]any{{
				"name":                     "folder.checkout",
				"line_number":              1,
				"source_class":             "declared",
				"source_kind":              "kubernetes",
				"declaration_kind":         "grafana_folder_resource",
				"folder_uid":               "checkout",
				"folder_title_fingerprint": "folder:abc",
				"outcome":                  "exact",
			}},
			"observability_coverage_warnings": []map[string]any{{
				"name":         "warning.unsupported.plugin",
				"line_number":  2,
				"source_class": "declared",
				"source_kind":  "kubernetes",
				"warning_kind": "unsupported_datasource_type",
				"outcome":      "unsupported",
			}},
		}},
	}
	collected := buildStreamingGeneration(repoPath, repo, "run-obs", observedAt, snapshot, false)
	return drainFactChannel(collected.Facts)
}

// assertSchemaVersionStamped asserts that at least one envelope of the given
// kind was emitted and that every one of them carries the expected schema
// version from the registry. The "at least one" check ensures the test fails
// if the emitter is accidentally removed, not just silenced.
func assertSchemaVersionStamped(t *testing.T, envelopes []facts.Envelope, registry map[string]string, kind string) {
	t.Helper()
	want, ok := registry[kind]
	if !ok {
		t.Fatalf("fact kind %q is not in the schema version registry; update the test if the kind was renamed", kind)
	}
	var found []facts.Envelope
	for _, env := range envelopes {
		if env.FactKind == kind {
			found = append(found, env)
		}
	}
	if len(found) == 0 {
		t.Fatalf("no envelopes of kind %q were emitted; the emitter may have been removed or the test fixture needs updating", kind)
	}
	for i, env := range found {
		if env.SchemaVersion != want {
			t.Errorf("envelope[%d] kind=%q: SchemaVersion = %q, want %q "+
				"(factEnvelope does not stamp SchemaVersion — the emitter must set it after the call)",
				i, kind, env.SchemaVersion, want)
		}
	}
}

// workflowImageFixture is a minimal GitHub Actions workflow that triggers
// ci.workflow_image_evidence emission (docker build/push commands).
const workflowImageFixture = `name: deploy
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: Build image
        run: docker build -t registry.example.com/team/api:latest .
      - name: Push image
        run: docker push registry.example.com/team/api:latest
`

// markdownDocFixture is a minimal README that triggers documentation_source,
// documentation_document, documentation_section, and documentation_link
// emission.
const markdownDocFixture = `# API Service

The API service handles requests. See [deployment docs](docs/deploy.md).

## Architecture

Uses a microservice pattern.
`
