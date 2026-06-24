package collector

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/discovery"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestBuildDiscoveryAdvisoryReportBySourceFileKind verifies that
// buildDiscoveryAdvisoryReport populates EntityCounts.BySourceFileKind using
// the bounded telemetry.ContentEntitySourceFileKind classifier. This is the
// path that lets drainCollector emit eshu_dp_content_entity_emitted_total
// per file kind without scanning individual facts.
//
// The fixture intentionally includes a large number of lockfile entities to
// exercise the #3676 explosion pattern that the counter is designed to surface.
func TestBuildDiscoveryAdvisoryReportBySourceFileKind(t *testing.T) {
	t.Parallel()

	depMeta := map[string]any{"config_kind": "dependency", "package_manager": "npm", "lockfile": true}
	entities := []ContentEntitySnapshot{
		// Code entities (empty ArtifactType, no manifest metadata).
		{EntityType: "Function", RelativePath: "pkg/main.go", ArtifactType: ""},
		{EntityType: "Function", RelativePath: "pkg/util.go", ArtifactType: ""},
		{EntityType: "Class", RelativePath: "pkg/types.go", ArtifactType: ""},

		// Package manifest dependency entities — simulates the #3676 explosion.
		// Real parser/reducer shape: entity_type "Variable", empty artifact_type,
		// config_kind "dependency" in metadata.
		{EntityType: "Variable", RelativePath: "package-lock.json", ArtifactType: "", Metadata: depMeta},
		{EntityType: "Variable", RelativePath: "package-lock.json", ArtifactType: "", Metadata: depMeta},
		{EntityType: "Variable", RelativePath: "go.mod", ArtifactType: "", Metadata: map[string]any{"config_kind": "dependency", "package_manager": "gomod"}},

		// Config entities use the artifact_type tokens the parser really emits.
		{EntityType: "Resource", RelativePath: "deploy/main.tf", ArtifactType: "terraform_hcl"},
		{EntityType: "Service", RelativePath: "docker-compose.yml", ArtifactType: "docker_compose"},
	}

	contentFiles := []ContentFileMeta{
		{RelativePath: "pkg/main.go"},
		{RelativePath: "pkg/util.go"},
		{RelativePath: "pkg/types.go"},
		{RelativePath: "package-lock.json"},
		{RelativePath: "go.mod"},
		{RelativePath: "deploy/main.tf"},
		{RelativePath: "docker-compose.yml"},
	}

	report := buildDiscoveryAdvisoryReport(
		"/repo",
		time.Now(),
		discovery.DiscoveryStats{},
		[]string{},
		contentFiles,
		entities,
		"abc123",
	)

	if report == nil {
		t.Fatal("buildDiscoveryAdvisoryReport() returned nil")
	}

	bsk := report.EntityCounts.BySourceFileKind
	if bsk == nil {
		t.Fatal("EntityCounts.BySourceFileKind is nil")
	}

	wantCounts := map[string]int{
		telemetry.SourceFileKindCode:            3,
		telemetry.SourceFileKindPackageManifest: 3,
		telemetry.SourceFileKindConfig:          2,
	}
	for kind, want := range wantCounts {
		if got := bsk[kind]; got != want {
			t.Errorf("BySourceFileKind[%q] = %d, want %d", kind, got, want)
		}
	}

	// "other" should be absent (no unknown artifact types in fixture)
	if got := bsk[telemetry.SourceFileKindOther]; got != 0 {
		t.Errorf("BySourceFileKind[%q] = %d, want 0", telemetry.SourceFileKindOther, got)
	}
}

// TestBuildDiscoveryAdvisoryReportBySourceFileKindAllCode verifies that a repo
// with only plain code files (no artifact_type set) maps entirely to "code".
func TestBuildDiscoveryAdvisoryReportBySourceFileKindAllCode(t *testing.T) {
	t.Parallel()

	entities := []ContentEntitySnapshot{
		{EntityType: "function", RelativePath: "main.go", ArtifactType: ""},
		{EntityType: "function", RelativePath: "util.go", ArtifactType: ""},
	}
	contentFiles := []ContentFileMeta{
		{RelativePath: "main.go"},
		{RelativePath: "util.go"},
	}

	report := buildDiscoveryAdvisoryReport(
		"/repo",
		time.Now(),
		discovery.DiscoveryStats{},
		[]string{},
		contentFiles,
		entities,
		"",
	)

	bsk := report.EntityCounts.BySourceFileKind
	if got := bsk[telemetry.SourceFileKindCode]; got != 2 {
		t.Errorf("BySourceFileKind[code] = %d, want 2", got)
	}
	if len(bsk) != 1 {
		t.Errorf("BySourceFileKind has %d keys, want 1 (only code): %v", len(bsk), bsk)
	}
}
