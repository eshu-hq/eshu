// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/terraformstate"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// parity8868SourceWarning mirrors one source-level warning block of the
// parity fixture.
type parity8868SourceWarning struct {
	WarningKind string `json:"warning_kind"`
	Reason      string `json:"reason"`
	Source      string `json:"source"`
}

// parity8868Fixture is the decoded cutover comparison input in
// testdata/8868-parity-tfstate.json.
type parity8868Fixture struct {
	ParentScopeID       string                    `json:"parent_scope_id"`
	BackendKind         string                    `json:"backend_kind"`
	Locator             string                    `json:"locator"`
	Serial              int64                     `json:"serial"`
	Lineage             string                    `json:"lineage"`
	ObservedAt          string                    `json:"observed_at"`
	FencingToken        int64                     `json:"fencing_token"`
	CollectorInstanceID string                    `json:"collector_instance_id"`
	SourceWarnings      []parity8868SourceWarning `json:"source_warnings"`
	State               json.RawMessage           `json:"state"`
	Hostile             struct {
		SecretOutputValue    string `json:"secret_output_value"`
		SecretAttributeValue string `json:"secret_attribute_value"`
		TruncatedBody        string `json:"truncated_body"`
		TrailingBody         string `json:"trailing_body"`
		ArrayRootBody        string `json:"array_root_body"`
		WrongSerial          int64  `json:"wrong_serial"`
		WrongLineage         string `json:"wrong_lineage"`
		BlankLocator         string `json:"blank_locator"`
		UnsupportedBackend   string `json:"unsupported_backend"`
	} `json:"hostile"`
}

// Frozen in-tree outputs for the representative fixture at the prep commit.
// The extracted producer must emit byte-identical stable keys and canonical
// bytes for these inputs; any derivation change breaks these assertions by
// design. parity8868FrozenDigest is the SHA-256 of the canonical envelope
// bytes (sorted by stable key, compact JSON).
var parity8868FrozenDigest = "59f0c95b5e6c65313cf1f0314308db079b1e4373d133db540f7edbcc030a8b4f"

// parity8868FrozenStableKeys is the sorted in-tree stable-key set for the
// representative fixture.
var parity8868FrozenStableKeys = []string{
	"terraform_state_module:module:module.vpc:resource:module.vpc.aws_subnet.a",
	"terraform_state_output:output:db_password",
	"terraform_state_output:output:endpoint",
	"terraform_state_provider_binding:provider_binding:aws_db_instance.main:1f4f09691e52a9d5da1aa7e0ffb551f3134c5206d50858bfbbaaf04381e0fc06",
	"terraform_state_provider_binding:provider_binding:module.vpc.aws_subnet.a:1f4f09691e52a9d5da1aa7e0ffb551f3134c5206d50858bfbbaaf04381e0fc06",
	"terraform_state_resource:resource:aws_db_instance.main",
	"terraform_state_resource:resource:module.vpc.aws_subnet.a",
	"terraform_state_snapshot:snapshot",
	"terraform_state_tag_observation:tag_observation:aws_db_instance.main:tags:d507a5993222ebb4770abf17bccd0fb1bf7f7610c6f7bfb5d509d1e27e82a049",
	"terraform_state_tag_observation:tag_observation:aws_db_instance.main:tags:fbef392a1f39fb6585b05ce0ffad1721040c6789ae3b385e3d82d263c7f23752",
	"terraform_state_warning:warning:state_in_vcs:git_local_file:approved_local",
}

func loadParity8868Fixture(t *testing.T) parity8868Fixture {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "8868-parity-tfstate.json"))
	if err != nil {
		t.Fatalf("ReadFile(parity fixture) error = %v, want nil", err)
	}
	var fixture parity8868Fixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("Unmarshal(parity fixture) error = %v, want nil", err)
	}
	if len(fixture.State) == 0 {
		t.Fatal("parity fixture state body is empty, want representative state")
	}
	return fixture
}

func parity8868ObservedAt(t *testing.T, fixture parity8868Fixture) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339, fixture.ObservedAt)
	if err != nil {
		t.Fatalf("Parse(observed_at) error = %v, want nil", err)
	}
	return value.UTC()
}

func parity8868Options(t *testing.T, fixture parity8868Fixture) terraformstate.ParseOptions {
	t.Helper()
	key, err := redact.NewKey([]byte("test-redaction-key-8868"))
	if err != nil {
		t.Fatalf("NewKey() error = %v, want nil", err)
	}
	rules, err := redact.NewRuleSet("parity-8868", []string{"password"})
	if err != nil {
		t.Fatalf("NewRuleSet() error = %v, want nil", err)
	}
	scopeValue, err := scope.NewTerraformStateSnapshotScope(
		fixture.ParentScopeID,
		fixture.BackendKind,
		fixture.Locator,
		nil,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotScope() error = %v, want nil", err)
	}
	observedAt := parity8868ObservedAt(t, fixture)
	generation, err := scope.NewTerraformStateSnapshotGeneration(
		scopeValue.ScopeID,
		fixture.Serial,
		fixture.Lineage,
		observedAt,
	)
	if err != nil {
		t.Fatalf("NewTerraformStateSnapshotGeneration() error = %v, want nil", err)
	}
	warnings := make([]terraformstate.SourceWarning, 0, len(fixture.SourceWarnings))
	for _, warning := range fixture.SourceWarnings {
		warnings = append(warnings, terraformstate.SourceWarning{
			WarningKind: warning.WarningKind,
			Reason:      warning.Reason,
			Source:      warning.Source,
		})
	}
	return terraformstate.ParseOptions{
		Scope:          scopeValue,
		Generation:     generation,
		Source:         terraformstate.StateKey{BackendKind: terraformstate.BackendS3, Locator: fixture.Locator},
		ObservedAt:     observedAt,
		RedactionKey:   key,
		RedactionRules: rules,
		FencingToken:   fixture.FencingToken,
		SourceWarnings: warnings,
	}
}

func parity8868Parse(t *testing.T, fixture parity8868Fixture, options terraformstate.ParseOptions, body []byte) terraformstate.ParseResult {
	t.Helper()
	result, err := terraformstate.Parse(context.Background(), strings.NewReader(string(body)), options)
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	return result
}

// parity8868CanonicalBytes returns the deterministic byte form the #4047
// cutover gate compares: envelopes sorted by stable key, compact JSON.
// encoding/json sorts map keys, so identical facts always produce identical
// bytes.
func parity8868CanonicalBytes(t *testing.T, envelopes []facts.Envelope) []byte {
	t.Helper()
	sorted := append([]facts.Envelope(nil), envelopes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].StableFactKey < sorted[j].StableFactKey })
	raw, err := json.Marshal(sorted)
	if err != nil {
		t.Fatalf("Marshal(canonical envelopes) error = %v, want nil", err)
	}
	return raw
}

func parity8868Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func parity8868StableKeys(envelopes []facts.Envelope) []string {
	keys := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		keys = append(keys, envelope.StableFactKey)
	}
	sort.Strings(keys)
	return keys
}

// TestParity8868RepresentativeContract pins the in-tree side of the #4047
// cutover comparison: kinds, schema versions, envelope identity, frozen
// stable keys, and frozen canonical output bytes for the representative
// fixture.
func TestParity8868RepresentativeContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8868Fixture(t)
	options := parity8868Options(t, fixture)
	result := parity8868Parse(t, fixture, options, fixture.State)

	requireFactKinds(
		t, result.Facts,
		cloud.TerraformStateSnapshotFactKind,
		cloud.TerraformStateResourceFactKind,
		cloud.TerraformStateOutputFactKind,
		cloud.TerraformStateModuleFactKind,
		cloud.TerraformStateProviderBindingFactKind,
		cloud.TerraformStateTagObservationFactKind,
		cloud.TerraformStateWarningFactKind,
	)
	if len(result.Facts) != 11 {
		t.Fatalf("len(Facts) = %d, want 11", len(result.Facts))
	}

	for _, envelope := range result.Facts {
		if got, want := envelope.CollectorKind, string(scope.CollectorTerraformState); got != want {
			t.Fatalf("CollectorKind = %q, want %q", got, want)
		}
		if got, want := envelope.SourceConfidence, facts.SourceConfidenceObserved; got != want {
			t.Fatalf("SourceConfidence = %q, want %q", got, want)
		}
		if got, want := envelope.SchemaVersion, "1.0.0"; got != want {
			t.Fatalf("SchemaVersion = %q, want %q", got, want)
		}
		if got, want := envelope.ScopeID, options.Scope.ScopeID; got != want {
			t.Fatalf("ScopeID = %q, want %q", got, want)
		}
		if got, want := envelope.GenerationID, options.Generation.GenerationID; got != want {
			t.Fatalf("GenerationID = %q, want %q", got, want)
		}
		if got, want := envelope.FencingToken, fixture.FencingToken; got != want {
			t.Fatalf("FencingToken = %d, want %d", got, want)
		}
		if !strings.HasPrefix(envelope.StableFactKey, envelope.FactKind+":") {
			t.Fatalf("StableFactKey = %q, want kind-prefixed key", envelope.StableFactKey)
		}
	}

	gotKeys := parity8868StableKeys(result.Facts)
	t.Logf("parity8868 stable keys: %#v", gotKeys)
	if len(parity8868FrozenStableKeys) == 0 {
		t.Fatalf("frozen stable keys not recorded; actual keys logged above")
	}
	if strings.Join(gotKeys, "\n") != strings.Join(parity8868FrozenStableKeys, "\n") {
		t.Fatalf("stable keys changed:\ngot  %#v\nwant %#v", gotKeys, parity8868FrozenStableKeys)
	}

	canonical := parity8868CanonicalBytes(t, result.Facts)
	t.Logf("parity8868 canonical digest: %s", parity8868Digest(canonical))
	if got, want := parity8868Digest(canonical), parity8868FrozenDigest; got != want {
		t.Fatalf("canonical digest = %q, want frozen %q", got, want)
	}

	// Identity join keys stay observable for drift correlation (#5870);
	// secrets and locators must never survive into output bytes.
	for _, secret := range []string{fixture.Hostile.SecretOutputValue, fixture.Hostile.SecretAttributeValue, fixture.Locator} {
		if strings.Contains(string(canonical), secret) {
			t.Fatalf("canonical bytes leak %q", secret)
		}
	}
	if !strings.Contains(string(canonical), "arn:aws:rds:us-east-1:123456789012:db:main") {
		t.Fatal("canonical bytes missing identity join-key arn, want #5870 exemption preserved")
	}
}

// TestParity8868RedactionContract proves raw secrets and the raw locator
// never reach payload bytes, refs, or metric-safe surfaces, while redaction
// markers and counters record the redaction that happened.
func TestParity8868RedactionContract(t *testing.T) {
	t.Parallel()

	fixture := loadParity8868Fixture(t)
	options := parity8868Options(t, fixture)
	result := parity8868Parse(t, fixture, options, fixture.State)

	assertNoRawSecret(t, result.Facts, fixture.Hostile.SecretOutputValue)
	assertNoRawSecret(t, result.Facts, fixture.Hostile.SecretAttributeValue)
	assertNoRawSecret(t, result.Facts, fixture.Locator)
	assertNoRawSecretInRefs(t, result.Facts, fixture.Locator)

	markers := 0
	for _, envelope := range result.Facts {
		markers += strings.Count(parity8868PayloadString(t, envelope), "redacted:hmac-sha256:")
	}
	if markers == 0 {
		t.Fatal("no redacted:hmac-sha256: markers in payloads, want fail-closed redaction evidence")
	}
	if len(result.RedactionsApplied) == 0 {
		t.Fatal("RedactionsApplied is empty, want redaction counters")
	}
	for _, envelope := range result.Facts {
		if strings.Contains(envelope.SourceRef.SourceURI, fixture.Locator) ||
			strings.Contains(envelope.SourceRef.SourceRecordID, fixture.Locator) {
			t.Fatalf("source ref leaks raw locator: %#v", envelope.SourceRef)
		}
	}
}

func parity8868PayloadString(t *testing.T, envelope facts.Envelope) string {
	t.Helper()
	raw, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatalf("Marshal(payload) error = %v, want nil", err)
	}
	return string(raw)
}
