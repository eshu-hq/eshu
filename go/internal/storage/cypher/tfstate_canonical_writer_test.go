// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterBuildsTerraformStateStatements(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&recordingExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "tf-scope-1",
		GenerationID: "tf-generation-1",
		TerraformStateResources: []projector.TerraformStateResourceRow{{
			UID:                   "tf-resource-uid-1",
			Address:               "module.app.aws_instance.web",
			Mode:                  "managed",
			ResourceType:          "aws_instance",
			Name:                  "web",
			ModuleAddress:         "module.app",
			ProviderAddress:       "provider[\"registry.terraform.io/hashicorp/aws\"]",
			Lineage:               "lineage-123",
			Serial:                17,
			BackendKind:           "s3",
			LocatorHash:           "locator-hash-1",
			StatePath:             "tfstate://s3/locator-hash-1",
			SourceFactID:          "tf-resource-1",
			StableFactKey:         "terraform_state_resource:resource:module.app.aws_instance.web",
			SourceSystem:          "terraform_state",
			SourceRecordID:        "module.app.aws_instance.web",
			SourceConfidence:      facts.SourceConfidenceObserved,
			CollectorKind:         "terraform_state",
			CorrelationAnchors:    []string{"arn:anchor-hash-1"},
			TagKeyHashes:          []string{"tag-key-hash-1"},
			Provider:              "aws",
			ProviderSourceAddress: "registry.terraform.io/hashicorp/aws",
			ProviderAlias:         "us_west_2",
			Attributes: map[string]any{
				"instance_type": "t3.micro",
				"ami":           "ami-0abcdef1234567890",
				"user_data":     "#!/bin/bash\necho hello",
			},
		}},
		TerraformStateModules: []projector.TerraformStateModuleRow{{
			UID:              "tf-module-uid-1",
			ModuleAddress:    "module.app",
			ResourceCount:    1,
			Lineage:          "lineage-123",
			Serial:           17,
			BackendKind:      "s3",
			LocatorHash:      "locator-hash-1",
			StatePath:        "tfstate://s3/locator-hash-1",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
		TerraformStateOutputs: []projector.TerraformStateOutputRow{{
			UID:              "tf-output-uid-1",
			Name:             "web_instance_id",
			Sensitive:        true,
			ValueShape:       "redacted_scalar",
			Lineage:          "lineage-123",
			Serial:           17,
			BackendKind:      "s3",
			LocatorHash:      "locator-hash-1",
			StatePath:        "tfstate://s3/locator-hash-1",
			SourceConfidence: facts.SourceConfidenceObserved,
			CollectorKind:    "terraform_state",
		}},
	}

	statements := writer.buildTerraformStateStatements(mat)
	// #5443/P0-2: buildTerraformStateStatements now emits, in order: the
	// migration relabel, the allowlisted-type REMOVE statement (#5441 review
	// round 9, P0), the resource upsert, the two generation-gated retraction
	// statements, the module upsert, the output upsert, and the
	// generation-gated MATCHES_STATE edge retract (#5443 P1 review finding)
	// -- 8 statements, not 4. Retraction MUST follow the resource upsert (not
	// precede it, as an earlier P0 review finding proved): see
	// TestTerraformStateStatementsEmitRetractAfterUpsert
	// (tfstate_canonical_writer_stale_attrs_test.go) for that ordering
	// contract and buildTerraformStateStatements' doc comment for why.
	// The MATCHES_STATE MERGE stays absent (this test wires no
	// TerraformStateOwnershipResolver, so OwningRepoID is never resolved),
	// but the edge retract statement itself is unconditional -- it only
	// ever deletes edges this writer's own evidence_source previously
	// wrote, so it is a harmless no-op when this cycle resolved no
	// ownership at all. See TestTerraformStateStatementsEmitRemoveBeforeUpsert
	// (tfstate_canonical_writer_stale_attrs_test.go) for the REMOVE/upsert
	// ordering contract; this test asserts each statement's own shape.
	if got, want := len(statements), 8; got != want {
		t.Fatalf("buildTerraformStateStatements() count = %d, want %d:\n%#v", got, want, statements)
	}

	migration := statements[0]
	migrationUIDs, ok := migration.Parameters["uids"].([]string)
	if !ok || len(migrationUIDs) != 1 || migrationUIDs[0] != "tf-resource-uid-1" {
		t.Fatalf("migration statement uids = %#v, want [tf-resource-uid-1]", migration.Parameters["uids"])
	}
	if !strings.Contains(migration.Cypher, "MATCH (r:TerraformResource)") ||
		!strings.Contains(migration.Cypher, "SET r:TerraformStateResource") ||
		!strings.Contains(migration.Cypher, "REMOVE r:TerraformResource") {
		t.Fatalf("migration Cypher = %q, want a MATCH TerraformResource / SET TerraformStateResource / REMOVE TerraformResource relabel", migration.Cypher)
	}

	remove := statements[1]
	if _, ok := remove.Parameters["uids"]; !ok {
		t.Fatalf("statements[1] has no uids parameter, want the standalone REMOVE statement: %#v", remove)
	}
	if strings.Contains(remove.Cypher, "SET") || strings.Contains(remove.Cypher, "MERGE") || strings.Contains(remove.Cypher, "UNWIND") {
		t.Fatalf("REMOVE statement Cypher = %q, must not combine MERGE/SET/UNWIND with REMOVE (#5441 review round 9 P0)", remove.Cypher)
	}
	if !strings.Contains(remove.Cypher, "REMOVE r.tf_attr_instance_type") || !strings.Contains(remove.Cypher, "r.tf_attr_ami") {
		t.Fatalf("REMOVE statement Cypher = %q, want tf_attr_instance_type and tf_attr_ami", remove.Cypher)
	}
	if !strings.Contains(remove.Cypher, "MATCH (r:TerraformStateResource)") {
		t.Fatalf("REMOVE statement Cypher = %q, want the TerraformStateResource label (#5443)", remove.Cypher)
	}
	uids, ok := remove.Parameters["uids"].([]string)
	if !ok || len(uids) != 1 || uids[0] != "tf-resource-uid-1" {
		t.Fatalf("REMOVE statement uids = %#v, want [tf-resource-uid-1]", remove.Parameters["uids"])
	}

	resource := statements[2]
	if !strings.Contains(resource.Cypher, "MERGE (r:TerraformStateResource {uid: row.uid})") {
		t.Fatalf("resource Cypher = %q, want TerraformStateResource uid merge (#5443)", resource.Cypher)
	}
	if strings.Contains(resource.Cypher, "REMOVE") {
		t.Fatalf("resource upsert Cypher = %q, must not contain REMOVE (#5441 review round 9 P0)", resource.Cypher)
	}
	rows := resource.Parameters["rows"].([]map[string]any)
	if got, want := rows[0]["provider_address"], "provider[\"registry.terraform.io/hashicorp/aws\"]"; got != want {
		t.Fatalf("provider_address = %q, want %q", got, want)
	}
	if got, want := rows[0]["source_confidence"], facts.SourceConfidenceObserved; got != want {
		t.Fatalf("source_confidence = %q, want %q", got, want)
	}
	if got, want := rows[0]["correlation_anchors"].([]string)[0], "arn:anchor-hash-1"; got != want {
		t.Fatalf("correlation_anchors[0] = %q, want %q", got, want)
	}
	if got, want := rows[0]["tag_key_hashes"].([]string)[0], "tag-key-hash-1"; got != want {
		t.Fatalf("tag_key_hashes[0] = %q, want %q", got, want)
	}
	if got, want := rows[0]["provider"], "aws"; got != want {
		t.Fatalf("provider = %#v, want %q", got, want)
	}
	if got, want := rows[0]["provider_source_address"], "registry.terraform.io/hashicorp/aws"; got != want {
		t.Fatalf("provider_source_address = %#v, want %q", got, want)
	}
	if got, want := rows[0]["provider_alias"], "us_west_2"; got != want {
		t.Fatalf("provider_alias = %#v, want %q", got, want)
	}
	if !strings.Contains(resource.Cypher, "r.provider = row.provider") ||
		!strings.Contains(resource.Cypher, "r.provider_source_address = row.provider_source_address") ||
		!strings.Contains(resource.Cypher, "r.provider_alias = row.provider_alias") {
		t.Fatalf("resource Cypher = %q, want unconditional SET clauses for provider/provider_source_address/provider_alias (#5446)", resource.Cypher)
	}
	if !strings.Contains(resource.Cypher, "r += row.attrs") {
		t.Fatalf("resource Cypher = %q, want an additive row.attrs merge", resource.Cypher)
	}
	attrs, ok := rows[0]["attrs"].(map[string]any)
	if !ok {
		t.Fatalf("rows[0][attrs] type = %T, want map[string]any", rows[0]["attrs"])
	}
	if got, want := attrs["tf_attr_instance_type"], "t3.micro"; got != want {
		t.Fatalf("attrs[tf_attr_instance_type] = %#v, want %q", got, want)
	}
	if got, want := attrs["tf_attr_ami"], "ami-0abcdef1234567890"; got != want {
		t.Fatalf("attrs[tf_attr_ami] = %#v, want %q", got, want)
	}
	if _, ok := attrs["tf_attr_user_data"]; ok {
		t.Fatalf("non-allowlisted user_data attribute was promoted: %#v", attrs)
	}

	if got, want := rows[0]["config_repo_id"], any(nil); got != want {
		t.Fatalf("config_repo_id = %#v, want nil (no ownership resolver wired)", got)
	}

	retractCurrent := statements[3]
	if !strings.Contains(retractCurrent.Cypher, "MATCH (r:TerraformStateResource)") || !strings.Contains(retractCurrent.Cypher, "DETACH DELETE r") {
		t.Fatalf("retract-current-label Cypher = %q, want a generation-gated TerraformStateResource DETACH DELETE", retractCurrent.Cypher)
	}
	if got, want := retractCurrent.Parameters["scope_id"], "tf-scope-1"; got != want {
		t.Fatalf("retract-current-label scope_id = %#v, want %q", got, want)
	}

	retractLegacy := statements[4]
	if !strings.Contains(retractLegacy.Cypher, "MATCH (r:TerraformResource)") || !strings.Contains(retractLegacy.Cypher, "DETACH DELETE r") {
		t.Fatalf("retract-legacy-label Cypher = %q, want a generation-gated TerraformResource DETACH DELETE", retractLegacy.Cypher)
	}

	if !strings.Contains(statements[5].Cypher, "MERGE (m:TerraformModule {uid: row.uid})") {
		t.Fatalf("module Cypher = %q, want TerraformModule uid merge", statements[5].Cypher)
	}
	if !strings.Contains(statements[6].Cypher, "MERGE (o:TerraformOutput {uid: row.uid})") {
		t.Fatalf("output Cypher = %q, want TerraformOutput uid merge", statements[6].Cypher)
	}

	edgeRetract := statements[7]
	if !strings.Contains(edgeRetract.Cypher, "MATCH (s:TerraformStateResource)") ||
		!strings.Contains(edgeRetract.Cypher, "MATCHES_STATE") ||
		!strings.Contains(edgeRetract.Cypher, "DELETE e") {
		t.Fatalf("edge-retract Cypher = %q, want a generation-gated MATCHES_STATE relationship DELETE anchored on TerraformStateResource (#5443 P1 review finding)", edgeRetract.Cypher)
	}
	if !edgeRetract.Drain || edgeRetract.DrainVar != "" {
		t.Fatalf("edge-retract Drain/DrainVar = %v/%q, want true/\"\" (bounded mixed-phase relationship retract)", edgeRetract.Drain, edgeRetract.DrainVar)
	}
	if got, want := edgeRetract.Parameters["scope_id"], "tf-scope-1"; got != want {
		t.Fatalf("edge-retract scope_id = %#v, want %q", got, want)
	}
}
