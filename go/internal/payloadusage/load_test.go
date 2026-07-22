// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package payloadusage

import (
	"path/filepath"
	"testing"
)

// repoRoot finds the repository root from this test file's own location
// (go/internal/payloadusage -> repo root is four levels up), so the
// real-repo integration tests below run against the actual checked-in
// go/internal/reducer, sdk/go/factschema/{aws,iam,gcp}/v1, and
// sdk/go/factschema/schema directories without depending on the working
// directory the test runner happens to invoke `go test` from.
func repoRoot(t *testing.T) string {
	t.Helper()
	// This file lives at <repoRoot>/go/internal/payloadusage/load_test.go.
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	return filepath.Join(wd, "..", "..", "..")
}

// TestLoadAgainstRealReducer proves issue #4573's acceptance criterion "the
// manifest generator runs against the real AWS/IAM/security-group handlers
// migrated in issue 2 and produces a non-trivial, correct manifest for at
// least one real fact kind (not just synthetic fixtures)": it runs Load
// against this repository's actual go/internal/reducer and
// sdk/go/factschema directories (no fixtures) and asserts concrete,
// hand-verified facts about the aws_resource kind established by reading
// go/internal/reducer/aws_resource_materialization.go and
// aws_relationship_join.go directly (see PR description / commit message for
// the file:line citations).
func TestLoadAgainstRealReducer(t *testing.T) {
	t.Parallel()

	manifest, err := Load(Paths{RepoRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(manifest.Kinds) != 120 {
		t.Fatalf("len(manifest.Kinds) = %d, want 120 (10 aws/iam — the original 8 plus rds_instance_posture and s3_external_principal_grant wired to the reducer's typed decode seam, #4632 — + 3 cross-provider image_reference kinds (aws/azure/gcp, #4685) + 6 wired incident (4 reducer-decoded + incident.lifecycle_event and change.record decoded only by the query-layer incident-context read model, #4794 W2a) + 3 wired gcp (gcp_cloud_resource, gcp_cloud_relationship via the reducer; gcp_tag_observation via the shared cloud-tag-evidence storage loader, #4686) + 3 wired azure (azure_cloud_resource, azure_cloud_relationship via the reducer; azure_tag_observation via the shared cloud-tag-evidence storage loader, #4686) + 4 wired kubernetes_live (the original 3 plus kubernetes_live.namespace, #5434) + 8 wired vulnerability + 8 wired sbom_attestation (the original 5 plus sbom.dependency_relationship and sbom.external_reference wired to the reducer's typed decode seam, #5370, plus attestation.slsa_provenance wired to the reducer's typed decode seam, #5371) + 6 wired ci_cd_run + 8 wired secrets_iam vault/k8s reducer kinds + 1 wired security_alert reducer kind + 3 reducer_derived package correlation kinds + 17 wired observability reducer kinds (source_instance is typed but has no reducer decode wrapper, so it is intentionally excluded) + 2 wired code kinds (file, repository outer envelopes) + 6 wired codedataflow kinds (Wave 4f S2) + 4 wired service_catalog kinds (entity, ownership, repository_link via the reducer; operational_link decoded only by the query-layer incident-context read model, #4794 W2a) + 6 projector oci_registry kinds + 6 projector terraform_state kinds (the original 5 plus terraform_state_provider_binding wired to the projector's typed decode seam, #5446) + 3 projector package_registry kinds + 9 wired work_item query kinds (issue_type_metadata added #4731) + 2 wired documentation kinds + 1 wired codeowners kind (codeowners.ownership via the reducer, issue #5419 Phase 3) + 1 wired submodule kind (submodule.pin via the reducer, issue #5420 Phase 3) via the reducer, projector, and query factschema_decode*.go globs); got %+v", len(manifest.Kinds), manifest.Kinds)
	}

	var awsResource *KindManifest
	for i := range manifest.Kinds {
		if manifest.Kinds[i].FactKind == "FactKindAWSResource" {
			awsResource = &manifest.Kinds[i]
		}
	}
	if awsResource == nil {
		t.Fatal("FactKindAWSResource not found in manifest")
	}

	if awsResource.DecodeFunc != "decodeAWSResource" {
		t.Errorf("DecodeFunc = %q, want decodeAWSResource", awsResource.DecodeFunc)
	}
	if awsResource.StructType != "awsv1.Resource" {
		t.Errorf("StructType = %q, want awsv1.Resource", awsResource.StructType)
	}

	// awsv1.Resource declares exactly 10 named JSON fields (account_id,
	// resource_id, region, resource_type, arn, name, state, service_kind,
	// correlation_anchors, tags) — Attributes is excluded (json:"-").
	if len(awsResource.DeclaredFields) != 10 {
		t.Errorf("len(DeclaredFields) = %d, want 10; got %+v", len(awsResource.DeclaredFields), awsResource.DeclaredFields)
	}

	usedByJSON := map[string][]string{}
	for _, u := range awsResource.UsedFields {
		usedByJSON[u.JSONName] = u.Files
	}

	// cloudResourceNodeRow (aws_resource_materialization.go) reads
	// resource.ARN, resource.ResourceID, resource.ResourceType, resource.Name,
	// resource.State, resource.AccountID, resource.Region,
	// resource.ServiceKind, resource.CorrelationAnchors, and
	// resource.Attributes (untyped, excluded). aws_relationship_join.go's
	// resourceUIDFromEnvelope reads a subset of the same fields.
	wantUsedInMaterialization := []string{"arn", "resource_id", "resource_type", "name", "state", "account_id", "region", "service_kind", "correlation_anchors"}
	for _, jsonName := range wantUsedInMaterialization {
		files, ok := usedByJSON[jsonName]
		if !ok {
			t.Errorf("expected field %q to be recorded as used somewhere in the manifest; got used fields %+v", jsonName, usedByJSON)
			continue
		}
		found := false
		for _, f := range files {
			if f == "aws_resource_materialization.go" {
				found = true
			}
		}
		if !found {
			t.Errorf("field %q used files = %v, want aws_resource_materialization.go among them", jsonName, files)
		}
	}

	// "tags" is a declared field on awsv1.Resource that no migrated handler
	// reads yet (per resource.go's own doc comment on Tags, only
	// materialization/join/posture consumers are wired so far) — this proves
	// the manifest is a real subset derived from usage, not a copy of every
	// declared field.
	if _, used := usedByJSON["tags"]; used {
		t.Log("note: \"tags\" is now read by a reducer handler; this is fine (proves the manifest tracks real usage), update this test's assumption if it changed intentionally")
	}

	// #4668: aws_iam_permission and aws_iam_principal read their fields ONLY
	// through wrapper structs — iamPermissionStatement.permission
	// (iam_can_perform_grant.go / iam_escalation_grant.go /
	// iam_escalation_target.go) and secretsIAMPrincipal.decoded
	// (secrets_iam_trust_chain_iam_role.go). Before wrapper-mediated
	// attribution those reads were invisible to the scanner, so
	// aws_iam_permission undercounted and aws_iam_principal's UsedFields was
	// empty. Assert the two-level wrapper reads are now attributed on the real
	// reducer, not just synthetic fixtures.
	wantWrapperMediated := map[string][]string{
		"FactKindAWSIAMPermission": {"actions", "not_actions", "resources"},
		"FactKindAWSIAMPrincipal":  {"account_id", "region"},
	}
	for factKind, wantFields := range wantWrapperMediated {
		var kind *KindManifest
		for i := range manifest.Kinds {
			if manifest.Kinds[i].FactKind == factKind {
				kind = &manifest.Kinds[i]
			}
		}
		if kind == nil {
			t.Errorf("%s not found in manifest; the IAM decode seam is not being scanned", factKind)
			continue
		}
		used := map[string]struct{}{}
		for _, u := range kind.UsedFields {
			used[u.JSONName] = struct{}{}
		}
		for _, f := range wantFields {
			if _, ok := used[f]; !ok {
				t.Errorf("%s UsedFields is missing %q — a wrapper-mediated read (#4668) was not attributed; got %+v", factKind, f, kind.UsedFields)
			}
		}
	}

	// Every used field must, by construction, be a member of DeclaredFields
	// — CheckManifest against the manifest's own baked-in declaration must
	// therefore report zero violations for the real repository state today.
	selfDeclared := map[string]map[string]struct{}{}
	for _, k := range manifest.Kinds {
		fields := map[string]struct{}{}
		for _, f := range k.DeclaredFields {
			fields[f.JSONName] = struct{}{}
		}
		selfDeclared[k.FactKind] = fields
	}
	if violations := CheckManifest(manifest, selfDeclared); len(violations) != 0 {
		t.Fatalf("CheckManifest against the manifest's own declared fields reported violations, which is a construction invariant break: %+v", violations)
	}
}

// TestGateAgainstRealReducerAndSchemas proves the actual end-to-end gate (not
// just BuildManifest) is currently clean against the real checked-in schemas:
// every field a real AWS/IAM/security-group handler reads is declared by its
// fact kind's checked-in JSON Schema. A regression here means either a
// handler started reading an undeclared field, or a schema file drifted out
// of sync with its struct (schema_gen_test.go in sdk/go/factschema is the
// primary lock for the latter; this is the reducer-usage-side lock).
func TestGateAgainstRealReducerAndSchemas(t *testing.T) {
	t.Parallel()

	manifest, violations, err := Gate(Paths{RepoRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("Gate() error = %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("Gate() found %d violation(s) against the real repository state, want 0:\n%s", len(violations), violationsString(violations))
	}
	if len(manifest.Kinds) != 120 {
		t.Fatalf("len(manifest.Kinds) = %d, want 120", len(manifest.Kinds))
	}
}

// TestGateCoversIncidentFamily proves the payload-usage gate ACTUALLY protects
// the incident family, not just that it passes. Before the factschema_decode*.go
// glob, the gate parsed only factschema_decode.go, so the incident decode
// wrappers in factschema_decode_incident.go were invisible: the gate stayed
// green while covering nothing for incident (a silent false-green). This test
// asserts two things the fix must hold:
//
//  1. Positive coverage — the manifest lists the incident kinds and their real
//     field usage from incident_routing_evidence_decode.go, so the gate has an
//     incident contract to check at all.
//  2. Live reverse-break — if a field the incident handler actually reads
//     (resource_class on the applied_pagerduty_resource kind, the sharpest
//     silent-skip field) were absent from the declared schema, CheckManifest
//     reports a violation naming it. This is the reverse-break the #4573 gate
//     exists to catch (a handler requiring a field no schema declares), proven
//     live for incident rather than only for aws.
func TestGateCoversIncidentFamily(t *testing.T) {
	t.Parallel()

	manifest, err := Load(Paths{RepoRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	const (
		wiredKind      = "FactKindIncidentRoutingAppliedPagerDutyResource"
		reverseField   = "resource_class"
		reverseGoField = "ResourceClass"
	)

	var applied *KindManifest
	for i := range manifest.Kinds {
		if manifest.Kinds[i].FactKind == wiredKind {
			applied = &manifest.Kinds[i]
		}
	}
	if applied == nil {
		t.Fatalf("%s not in manifest — the factschema_decode*.go glob is not picking up the incident decode wrappers, so the gate covers nothing for incident", wiredKind)
	}
	if applied.DecodeFunc != "decodeIncidentRoutingAppliedPagerDutyResource" {
		t.Errorf("DecodeFunc = %q, want decodeIncidentRoutingAppliedPagerDutyResource", applied.DecodeFunc)
	}

	// Positive coverage: the handler's real field reads are captured.
	usedByJSON := map[string]struct{}{}
	for _, u := range applied.UsedFields {
		usedByJSON[u.JSONName] = struct{}{}
	}
	if _, ok := usedByJSON[reverseField]; !ok {
		t.Fatalf("%s used fields = %+v, want %q among them (the applied-resource decode reads it for the service-class filter)", wiredKind, applied.UsedFields, reverseField)
	}

	// Live reverse-break: drop reverseField from the declared set for this kind
	// only, then confirm CheckManifest flags the incident handler reading it.
	declared := map[string]map[string]struct{}{}
	for _, k := range manifest.Kinds {
		fields := map[string]struct{}{}
		for _, f := range k.DeclaredFields {
			fields[f.JSONName] = struct{}{}
		}
		declared[k.FactKind] = fields
	}
	delete(declared[wiredKind], reverseField)

	violations := CheckManifest(manifest, declared)
	var found bool
	for _, v := range violations {
		if v.FactKind == wiredKind && v.GoFieldName == reverseGoField {
			found = true
		}
	}
	if !found {
		t.Fatalf("CheckManifest did not flag %s reading undeclared field %q; the reverse-break check is NOT live for the incident family. violations=%s",
			wiredKind, reverseField, violationsString(violations))
	}
}

func violationsString(violations []Violation) string {
	s := ""
	for _, v := range violations {
		s += v.String() + "\n"
	}
	return s
}

// TestLoadIsIdempotent proves the manifest derivation is deterministic:
// re-running Load against the same real repository state twice produces
// byte-identical JSON, the generator-script-discipline idempotency
// requirement applied to a Go-native generator instead of a shell script.
func TestLoadIsIdempotent(t *testing.T) {
	t.Parallel()

	root := repoRoot(t)
	first, err := Load(Paths{RepoRoot: root})
	if err != nil {
		t.Fatalf("first Load() error = %v", err)
	}
	second, err := Load(Paths{RepoRoot: root})
	if err != nil {
		t.Fatalf("second Load() error = %v", err)
	}

	firstJSON := mustMarshal(t, first)
	secondJSON := mustMarshal(t, second)
	if firstJSON != secondJSON {
		t.Fatalf("Load() is not idempotent: two runs against the same repository state produced different JSON.\nfirst:\n%s\nsecond:\n%s", firstJSON, secondJSON)
	}
}

func mustMarshal(t *testing.T, m Manifest) string {
	t.Helper()
	encoded, err := MarshalIndent(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return encoded
}

// TestLoadCoversWiredAzureKinds proves the payload-usage manifest actually
// scans the azure/v1 struct dir and the wired azure decode seams — not that
// "azure isn't scanned". It asserts the wired azure kinds
// (azure_cloud_resource, azure_cloud_relationship via the reducer;
// azure_image_reference via the shared container-image-identity reducer,
// #4685; azure_tag_observation via the shared cloud-tag-evidence storage
// loader, #4686) appear in the manifest with their real handler files as the
// used-field source, so a regression that drops azure from the gate (e.g. a
// removed AzureStructDir default or a dropped factKindSchemaFile entry) fails
// here rather than silently narrowing coverage. The remaining DEFERRED azure
// kinds (identity_observation, resource_change) must NOT appear: they have no
// typed decode seam yet, so gating them would be a hollow contract.
func TestLoadCoversWiredAzureKinds(t *testing.T) {
	t.Parallel()

	manifest, err := Load(Paths{RepoRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	byKind := map[string]*KindManifest{}
	for i := range manifest.Kinds {
		byKind[manifest.Kinds[i].FactKind] = &manifest.Kinds[i]
	}

	wired := map[string]struct {
		decodeFunc string
		structType string
		usedFile   string
	}{
		"FactKindAzureCloudResource":     {"decodeAzureCloudResource", "azurev1.CloudResource", "azure_resource_materialization.go"},
		"FactKindAzureCloudRelationship": {"decodeAzureCloudRelationship", "azurev1.CloudRelationship", "azure_relationship_join.go"},
		"FactKindAzureImageReference":    {"decodeAzureImageReference", "azurev1.ImageReference", "container_image_identity_typed_evidence.go"},
		"FactKindAzureTagObservation":    {"decodeAzureTagObservation", "azurev1.TagObservation", "cloud_tag_evidence.go"},
	}
	for kind, want := range wired {
		got, ok := byKind[kind]
		if !ok {
			t.Fatalf("wired azure kind %q missing from manifest; azure/v1 is not being scanned/gated", kind)
		}
		if got.DecodeFunc != want.decodeFunc {
			t.Errorf("%s DecodeFunc = %q, want %q", kind, got.DecodeFunc, want.decodeFunc)
		}
		if got.StructType != want.structType {
			t.Errorf("%s StructType = %q, want %q", kind, got.StructType, want.structType)
		}
		usedInHandler := false
		for _, u := range got.UsedFields {
			for _, f := range u.Files {
				if f == want.usedFile {
					usedInHandler = true
				}
			}
		}
		if !usedInHandler {
			t.Errorf("%s has no used field recorded in %s; the azure handler usage is not being scanned: %+v", kind, want.usedFile, got.UsedFields)
		}
	}

	deferred := []string{
		"FactKindAzureIdentityObservation",
		"FactKindAzureResourceChange",
	}
	for _, kind := range deferred {
		if _, present := byKind[kind]; present {
			t.Errorf("deferred azure kind %q appears in the manifest; it has no typed decode seam yet and must not be gated (hollow contract)", kind)
		}
	}
}
