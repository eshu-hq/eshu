// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestTerraformStateCassetteDecodesCleanlyThroughSeam is the durable guard
// against the stale-cassette regression the Wave 4b typed-payload migration
// surfaced (the same class of bug the k8s and oci_registry migrations hit
// first): the B-7 golden-corpus cassette (testdata/cassettes/terraformstate/
// supply-chain-demo.json) must carry the shape the CURRENT collector emitter
// produces (go/internal/collector/terraformstate/*.go), because the projector
// now decodes each terraform_state fact through the typed factschema seam. A
// cassette payload whose required field is absent, or whose fact_kind string
// does not match the wire constant, dead-letters the whole fact through the
// projector's per-fact quarantine (or never matches the extractor's switch at
// all), producing ZERO resource/module/output nodes and a red golden-corpus
// gate — with no unit-test signal, because the gate runs Docker.
//
// This test closes that gap without Docker: it reads the real checked-in
// cassette, feeds each terraform_state fact through the SAME
// extractTerraformStateRows path the projector runs, and asserts that (a) no
// valid cassette fact is quarantined as input_invalid, and (b) every consumed
// terraform_state kind in the cassette materializes its canonical row. If a
// future cassette edit drifts from the collector's emit shape into a shape the
// typed decode rejects, this fails immediately at `go test ./internal/projector`,
// long before the Docker gate.
func TestTerraformStateCassetteDecodesCleanlyThroughSeam(t *testing.T) {
	t.Parallel()

	envelopes := loadTerraformStateCassetteEnvelopes(t)
	if len(envelopes) == 0 {
		t.Fatal("cassette carried no terraform_state facts; the golden-corpus gate would project nothing")
	}

	mat := &CanonicalMaterialization{ScopeID: "cassette-tfstate-scd"}
	quarantined := extractTerraformStateRows(mat, envelopes)

	// Every valid cassette fact must decode cleanly — a quarantine here means
	// the cassette drifted from the collector's emit shape into an
	// input_invalid shape (the stale-cassette regression this test exists to
	// catch).
	if len(quarantined) != 0 {
		for _, q := range quarantined {
			t.Errorf("cassette fact %s (%s) quarantined as input_invalid on field %q; the cassette payload has drifted from the current collector emitter's shape — reconcile testdata/cassettes/terraformstate/supply-chain-demo.json to go/internal/collector/terraformstate/*.go", q.factID, q.factKind, q.field)
		}
		t.FailNow()
	}

	// The cassette carries a snapshot + two ECS resources + modules + a tag
	// observation + an output; every consumed kind must materialize so the
	// golden-corpus gate's TerraformResource node-count check ("at least one
	// ECS resource expected") can pass.
	if len(mat.TerraformStateResources) == 0 {
		t.Error("cassette resource facts did not materialize any TerraformStateResource row; the golden-corpus gate's TerraformResource node-count check would fail")
	}
	if len(mat.TerraformStateModules) == 0 {
		t.Error("cassette module facts did not materialize any TerraformStateModule row")
	}
	if len(mat.TerraformStateOutputs) == 0 {
		t.Error("cassette output fact did not materialize a TerraformStateOutput row")
	}
	var sawTagJoin bool
	for _, resource := range mat.TerraformStateResources {
		if len(resource.TagKeyHashes) > 0 {
			sawTagJoin = true
		}
	}
	if !sawTagJoin {
		t.Error("cassette tag_observation fact did not join to any resource's TagKeyHashes")
	}
}

// TestTerraformStateCassetteResourcesCarryDriftAttributes locks in that the
// golden-corpus cassette's terraform_state_resource facts carry the classified
// attributes object the CURRENT collector emits unconditionally
// (go/internal/collector/terraformstate/resources.go emitResourceInstance writes
// payload["attributes"]). The Wave 4b cassette reconciliation once dropped this
// object; the projector ignores attributes, so no projector test caught the loss.
// But the Postgres query-time drift loaders join Terraform state to observed
// cloud resources on payload->'attributes'->>'arn'/'id'/'self_link'
// (go/internal/storage/postgres/multi_cloud_runtime_drift_evidence_sql.go). A
// resource fact with no attributes materializes a node but can never participate
// in AWS/multi-cloud drift matching, so the cassette would silently stop
// exercising the state/cloud drift path. This asserts every resource fact carries
// a non-empty attributes object with at least one drift-join key, so a future
// reconciliation that drops the field fails here rather than silently eroding
// drift coverage.
func TestTerraformStateCassetteResourcesCarryDriftAttributes(t *testing.T) {
	t.Parallel()

	driftJoinKeys := []string{"arn", "id", "self_link"}
	var sawResource bool
	for _, envelope := range loadTerraformStateCassetteEnvelopes(t) {
		if envelope.FactKind != "terraform_state_resource" {
			continue
		}
		sawResource = true
		attributes, ok := envelope.Payload["attributes"].(map[string]any)
		if !ok || len(attributes) == 0 {
			t.Errorf("resource fact %s carries no non-empty attributes object; the Postgres drift loaders join Terraform state to cloud on payload->'attributes'->>'arn'/'id'/'self_link', so this fact can never drift-match — restore the collector-emitted attributes in testdata/cassettes/terraformstate/supply-chain-demo.json", envelope.FactID)
			continue
		}
		var sawJoinKey bool
		for _, key := range driftJoinKeys {
			if value, present := attributes[key].(string); present && value != "" {
				sawJoinKey = true
				break
			}
		}
		if !sawJoinKey {
			t.Errorf("resource fact %s attributes carry none of the drift-join keys %v; the cassette no longer exercises the state/cloud drift path", envelope.FactID, driftJoinKeys)
		}
	}
	if !sawResource {
		t.Fatal("cassette carried no terraform_state_resource facts; the drift-attributes guard exercised nothing")
	}
}

// TestTerraformStateCassetteResourceCarriesProviderBinding is the #5446
// non-vacuous regression guard for the provider-binding pre-pass: the
// terraformstate supply-chain-demo cassette carries a
// terraform_state_provider_binding fact for module.ecs.aws_ecs_cluster.supply-chain-demo
// (provider_type "aws"), which terraformStateProviderBindingsByResource joins
// onto that resource's row by ResourceAddress. This proves the join is
// non-vacuous through the SAME extractTerraformStateRows path the projector
// runs in production and the golden-corpus gate replays -- zero without the
// #5446 pre-pass (the field did not exist before), and zero without the new
// cassette fact (no other resource in this cassette has a provider binding
// fact). This is the hermetic, no-Docker counterpart to the golden-corpus
// gate's `MATCH (r:TerraformStateResource) WHERE r.provider = 'aws' RETURN
// count(r)` non-vacuous assertion (testdata/golden/e2e-20repo-snapshot.json's
// rn-terraform-state-provider-binding required_node entry).
func TestTerraformStateCassetteResourceCarriesProviderBinding(t *testing.T) {
	t.Parallel()

	envelopes := loadTerraformStateCassetteEnvelopes(t)
	mat := &CanonicalMaterialization{ScopeID: "cassette-tfstate-scd"}
	quarantined := extractTerraformStateRows(mat, envelopes)
	if len(quarantined) != 0 {
		t.Fatalf("extractTerraformStateRows quarantined %d facts, want 0: %#v", len(quarantined), quarantined)
	}

	var sawProvider bool
	for _, resource := range mat.TerraformStateResources {
		if resource.Address != "module.ecs.aws_ecs_cluster.supply-chain-demo" {
			continue
		}
		if resource.Provider != "aws" {
			t.Fatalf("resource %s Provider = %q, want %q", resource.Address, resource.Provider, "aws")
		}
		if resource.ProviderSourceAddress != "registry.terraform.io/hashicorp/aws" {
			t.Fatalf("resource %s ProviderSourceAddress = %q, want %q", resource.Address, resource.ProviderSourceAddress, "registry.terraform.io/hashicorp/aws")
		}
		sawProvider = true
	}
	if !sawProvider {
		t.Fatal("cassette provider_binding fact did not join to module.ecs.aws_ecs_cluster.supply-chain-demo; the golden-corpus gate's r.provider='aws' non-vacuous check would fail")
	}
}

// loadTerraformStateCassetteEnvelopes reads the real checked-in terraform_state
// cassette and converts each recorded fact into a facts.Envelope carrying the
// fact kind, schema version, and payload the projector's
// extractTerraformStateRows consumes. It intentionally reads the same file the
// golden-corpus gate replays, so a drift in that file is caught here.
func loadTerraformStateCassetteEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	// This file lives at <repoRoot>/go/internal/projector/; the cassette lives
	// at <repoRoot>/testdata/cassettes/terraformstate/supply-chain-demo.json.
	wd, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolve absolute path: %v", err)
	}
	cassettePath := filepath.Join(wd, "..", "..", "..", "testdata", "cassettes", "terraformstate", "supply-chain-demo.json")

	raw, err := os.ReadFile(cassettePath)
	if err != nil {
		t.Fatalf("read terraform_state cassette %s: %v", cassettePath, err)
	}

	var cassette struct {
		Scopes []struct {
			GenerationID string `json:"generation_id"`
			ScopeID      string `json:"scope_id"`
			Facts        []struct {
				FactKind      string         `json:"fact_kind"`
				SchemaVersion string         `json:"schema_version"`
				StableFactKey string         `json:"stable_fact_key"`
				Payload       map[string]any `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(raw, &cassette); err != nil {
		t.Fatalf("unmarshal terraform_state cassette: %v", err)
	}

	var envelopes []facts.Envelope
	for _, scope := range cassette.Scopes {
		for i, fact := range scope.Facts {
			envelopes = append(envelopes, facts.Envelope{
				FactID:        terraformStateCassetteFactID(fact.FactKind, i),
				ScopeID:       scope.ScopeID,
				GenerationID:  scope.GenerationID,
				FactKind:      fact.FactKind,
				SchemaVersion: fact.SchemaVersion,
				StableFactKey: fact.StableFactKey,
				Payload:       fact.Payload,
			})
		}
	}
	return envelopes
}

// terraformStateCassetteFactID synthesizes a stable, unique fact id for a
// cassette fact so a quarantine message can name the offending fact
// deterministically.
func terraformStateCassetteFactID(factKind string, index int) string {
	return fmt.Sprintf("cassette:%s:%d", factKind, index)
}
