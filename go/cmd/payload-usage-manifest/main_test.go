// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot resolves the repository root from this test file's own location
// (go/cmd/payload-usage-manifest -> repo root is three levels up), so the
// CLI-level tests below exercise the real checked-in reducer/factschema
// directories rather than synthetic fixtures.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	return filepath.Join(wd, "..", "..", "..")
}

// TestRunGenerateAgainstRealRepoProducesNonTrivialManifest proves the CLI's
// generate mode, run against this repository's real go/internal/reducer and
// sdk/go/factschema directories, produces valid, non-trivial JSON for at
// least one real fact kind — issue #4573's "runs against the real AWS/IAM/
// security-group handlers ... produces a non-trivial, correct manifest"
// acceptance criterion, exercised through the actual CLI entry point rather
// than only the library's own tests.
func TestRunGenerateAgainstRealRepoProducesNonTrivialManifest(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"-repo-root", repoRoot(t), "-mode", "generate"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, stderr = %s", err, stderr.String())
	}

	var manifest struct {
		Kinds []struct {
			FactKind       string `json:"fact_kind"`
			DeclaredFields []any  `json:"declared_fields"`
			UsedFields     []any  `json:"used_fields"`
		} `json:"kinds"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &manifest); err != nil {
		t.Fatalf("generate output is not valid JSON: %v\noutput:\n%s", err, stdout.String())
	}
	if len(manifest.Kinds) != 117 {
		t.Fatalf("len(Kinds) = %d, want 117 (1 wired codeowners kind (codeowners.ownership via the reducer decode seam, #5419) + 10 aws/iam (8 from #4568 + rds_instance_posture and s3_external_principal_grant wired via the reducer decode seam in #4632) + 3 cross-provider image_reference kinds (aws/azure/gcp, #4685) + 6 wired incident (4 reducer-decoded + incident.lifecycle_event and change.record decoded only by the query-layer incident-context read model, #4794 W2a) + 3 wired gcp (gcp_cloud_resource, gcp_cloud_relationship via the reducer; gcp_tag_observation via the shared cloud-tag-evidence storage loader, #4686) + 3 wired azure (azure_cloud_resource, azure_cloud_relationship via the reducer; azure_tag_observation via the shared cloud-tag-evidence storage loader, #4686) + 3 wired kubernetes_live + 8 wired vulnerability + 8 wired sbom_attestation (the original 5 plus sbom.dependency_relationship and sbom.external_reference wired to the reducer's typed decode seam #5370, plus attestation.slsa_provenance now emitted by the collector and wired to the reducer decode seam #5371) + 6 wired ci_cd_run + 8 wired secrets_iam vault/k8s reducer kinds + 1 wired security_alert reducer kind + 17 wired observability reducer kinds (source_instance is typed but has no reducer decode wrapper, so it is intentionally excluded) + 2 wired code kinds (file, repository outer envelopes) + 6 wired codedataflow kinds (Wave 4f S2) + 4 wired service_catalog kinds (entity, ownership, repository_link via the reducer; operational_link decoded only by the query-layer incident-context read model, #4794 W2a) + 6 projector oci_registry kinds + 5 projector terraform_state kinds + 3 projector package_registry kinds + 9 wired work_item query kinds (issue_type_metadata added #4731) + 2 wired documentation kinds via the reducer, projector, and query factschema_decode*.go globs + 3 reducer_derived package correlation kinds)", len(manifest.Kinds))
	}

	var foundNonTrivial bool
	for _, k := range manifest.Kinds {
		if len(k.UsedFields) > 3 {
			foundNonTrivial = true
		}
	}
	if !foundNonTrivial {
		t.Error("no fact kind had more than 3 used fields; expected at least one real, non-trivial usage set")
	}
}

// TestRunGateAgainstRealRepoPasses proves the CLI's gate mode is currently
// green against the real repository state and prints a summary line, not
// just an empty success.
func TestRunGateAgainstRealRepoPasses(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"-repo-root", repoRoot(t), "-mode", "gate"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run() error = %v, stdout = %s, stderr = %s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "no undeclared field usage found") {
		t.Errorf("gate stdout = %q, want a summary line reporting clean status", stdout.String())
	}
}

// TestRunGateFailsOnUndeclaredField proves the CLI-level gate mode fails and
// prints the specific handler/kind/field when a checked-in schema is missing
// a field a real handler reads. This exercises the exact scenario issue
// #4573 requires as the failing-first fixture, but through the CLI's -mode
// gate flag rather than the library API directly (see
// go/internal/reducer/payload_usage_manifest_test.go and
// go/internal/payloadusage/load_test.go for the library-level equivalents).
func TestRunGateFailsOnUndeclaredField(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	schemaSrc := filepath.Join(root, "sdk", "go", "factschema", "schema")
	tmpSchemaDir := t.TempDir()
	copySchemaDir(t, schemaSrc, tmpSchemaDir)

	// Break the aws_resource schema: drop the "resource_type" property that
	// every migrated aws_resource consumer reads via resource.ResourceType.
	brokenPath := filepath.Join(tmpSchemaDir, "aws_resource.v1.schema.json")
	breakSchemaField(t, brokenPath, "resource_type")

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"-repo-root", root,
		"-schema-dir", tmpSchemaDir,
		"-mode", "gate",
	}, &stdout, &stderr)
	if err == nil {
		t.Fatalf("run() error = nil, want a gate failure; stdout = %s", stdout.String())
	}
	out := stdout.String()
	for _, want := range []string{"FactKindAWSResource", "ResourceType", "aws_resource_materialization.go"} {
		if !strings.Contains(out, want) {
			t.Errorf("gate failure output = %q, want it to contain %q", out, want)
		}
	}
}

// TestRunHelpDocumentsModes proves -help documents both modes, per the
// factschema-diff --help precedent this command mirrors.
func TestRunHelpDocumentsModes(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	err := run([]string{"-help"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run([-help]) error = nil, want flag.ErrHelp")
	}
	helpOut := stderr.String()
	for _, want := range []string{"generate", "gate", "Contract System v1", "loader-dir", "relationships-dir", "replay-dir"} {
		if !strings.Contains(helpOut, want) {
			t.Errorf("-help output = %q, want it to mention %q", helpOut, want)
		}
	}
}

func copySchemaDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("read schema src dir: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, entry.Name())) //nolint:gosec // test fixture, path from os.ReadDir of a fixed src dir.
		if err != nil {
			t.Fatalf("read schema file %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, entry.Name()), data, 0o600); err != nil {
			t.Fatalf("write schema file %s: %v", entry.Name(), err)
		}
	}
}

// breakSchemaField removes fieldName from a JSON Schema file's "properties"
// and "required" arrays in place, simulating a schema that regressed behind
// a handler that still reads the field.
func breakSchemaField(t *testing.T, path, fieldName string) {
	t.Helper()
	raw, err := os.ReadFile(path) //nolint:gosec // test fixture, path built from a t.TempDir() copy.
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema %s: %v", path, err)
	}
	if props, ok := doc["properties"].(map[string]any); ok {
		delete(props, fieldName)
	}
	if required, ok := doc["required"].([]any); ok {
		var kept []any
		for _, r := range required {
			if s, ok := r.(string); ok && s == fieldName {
				continue
			}
			kept = append(kept, r)
		}
		doc["required"] = kept
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("encode broken schema: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write broken schema %s: %v", path, err)
	}
}
