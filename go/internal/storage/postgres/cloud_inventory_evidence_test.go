// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// TestPostgresCloudInventoryEvidenceLoaderMapsProviderSourceFacts proves the
// loader reads the three provider inventory source fact kinds for one scope
// generation and maps each provider payload into the shared admission record
// shape with the correct raw identity, resource type, and observed source layer.
func TestPostgresCloudInventoryEvidenceLoaderMapsProviderSourceFacts(t *testing.T) {
	t.Parallel()

	const (
		scopeID      = "cloud:tenant-1"
		generationID = "gen-1"
	)
	awsARN := "arn:aws:s3:::managed-bucket"
	gcpName := "//compute.googleapis.com/projects/p/zones/z/instances/i"
	azureID := "/subscriptions/sub-1/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.AWSResourceFactKind, awsARN, []byte(`{
					"arn":"` + awsARN + `",
					"resource_type":"aws_s3_bucket"
				}`)},
				{facts.GCPCloudResourceFactKind, gcpName, []byte(`{
					"full_resource_name":"` + gcpName + `",
					"asset_type":"compute.googleapis.com/Instance"
				}`)},
				{facts.AzureCloudResourceFactKind, azureID, []byte(`{
					"arm_resource_id":"` + azureID + `",
					"resource_type":"microsoft.compute/virtualmachines"
				}`)},
			}},
		},
	}

	loader := PostgresCloudInventoryEvidenceLoader{DB: db}

	records, err := loader.LoadCloudInventoryEvidence(context.Background(), scopeID, generationID)
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 3; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}

	byProvider := make(map[string]reducer.CloudInventoryRecord, len(records))
	for _, record := range records {
		byProvider[record.Provider] = record
		if record.SourceLayer != reducer.SourceLayerObserved {
			t.Fatalf("provider %q source layer = %q, want observed", record.Provider, record.SourceLayer)
		}
	}

	aws := byProvider["aws"]
	if aws.FactKind != facts.AWSResourceFactKind || aws.RawIdentity != awsARN || aws.ResourceType != "aws_s3_bucket" {
		t.Fatalf("aws record = %#v", aws)
	}
	gcp := byProvider["gcp"]
	if gcp.FactKind != facts.GCPCloudResourceFactKind || gcp.RawIdentity != gcpName || gcp.ResourceType != "compute.googleapis.com/Instance" {
		t.Fatalf("gcp record = %#v", gcp)
	}
	azure := byProvider["azure"]
	if azure.FactKind != facts.AzureCloudResourceFactKind || azure.RawIdentity != azureID || azure.ResourceType != "microsoft.compute/virtualmachines" {
		t.Fatalf("azure record = %#v", azure)
	}

	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	q := db.queries[0]
	if got, want := q.args[0], scopeID; got != want {
		t.Fatalf("scope arg = %v, want %v", got, want)
	}
	if got, want := q.args[1], generationID; got != want {
		t.Fatalf("generation arg = %v, want %v", got, want)
	}
	// The load must be bound to scope_id and generation_id so a stale generation
	// cannot leak rows into a newer admission.
	if !strings.Contains(q.query, "scope_id = $1") || !strings.Contains(q.query, "generation_id = $2") {
		t.Fatalf("query is not bound to scope+generation:\n%s", q.query)
	}
	for _, kind := range []string{
		facts.AWSResourceFactKind,
		facts.GCPCloudResourceFactKind,
		facts.AzureCloudResourceFactKind,
	} {
		if !strings.Contains(q.query, kind) {
			t.Fatalf("query missing source fact kind %q:\n%s", kind, q.query)
		}
	}
	// Active facts only: tombstones must be excluded from canonical admission.
	if !strings.Contains(q.query, "is_tombstone = FALSE") {
		t.Fatalf("query does not exclude tombstones:\n%s", q.query)
	}
}

// TestPostgresCloudInventoryEvidenceLoaderSkipsBlankAndMalformedRows proves the
// loader drops rows with a blank raw identity or undecodable payload rather than
// emitting an empty-identity record that would resolve to a non-admitted outcome
// it cannot key. Blank/malformed provider evidence is a collector defect, not
// canonical truth, so it is skipped at load time and never reaches admission.
func TestPostgresCloudInventoryEvidenceLoaderSkipsBlankAndMalformedRows(t *testing.T) {
	t.Parallel()

	awsARN := "arn:aws:s3:::ok"
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{facts.AWSResourceFactKind, awsARN, []byte(`{"arn":"` + awsARN + `","resource_type":"aws_s3_bucket"}`)},
				// Blank raw identity: dropped.
				{facts.GCPCloudResourceFactKind, "", []byte(`{"full_resource_name":"","asset_type":"x"}`)},
				// Undecodable payload: dropped.
				{facts.AzureCloudResourceFactKind, "x", []byte(`{not json`)},
			}},
		},
	}

	loader := PostgresCloudInventoryEvidenceLoader{DB: db}

	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if got, want := len(records), 1; got != want {
		t.Fatalf("len(records) = %d, want %d", got, want)
	}
	if records[0].RawIdentity != awsARN {
		t.Fatalf("records[0] = %#v, want only the aws record", records[0])
	}
}

// TestPostgresCloudInventoryEvidenceLoaderKeysRawIdentityPerProvider proves the
// per-fact-kind raw-identity selection: an aws_resource row whose own arn is
// blank is dropped even if the payload carries a stray foreign provider key, so
// one provider's key can never supply a raw identity for a different provider
// and resolve into the wrong keyspace. The fake mirrors the production SQL by
// only returning a non-null raw identity for the matching provider key; this
// test asserts the loader does not resurrect a foreign key on its own.
func TestPostgresCloudInventoryEvidenceLoaderKeysRawIdentityPerProvider(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				// Production SQL returns raw_identity NULL for this row because the
				// aws CASE branch reads only arn, which is blank. A NULL scanned as
				// *string is impossible here, so the fake returns the empty string
				// the loader must reject.
				{facts.AWSResourceFactKind, "", []byte(`{
					"arn":"",
					"full_resource_name":"//compute.googleapis.com/projects/p/zones/z/instances/i",
					"resource_type":"aws_s3_bucket"
				}`)},
			}},
		},
	}

	loader := PostgresCloudInventoryEvidenceLoader{DB: db}
	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Fatalf("len(records) = %d, want 0 (blank aws arn must not borrow a foreign key)", len(records))
	}
	// The SQL must read aws raw identity strictly from arn, not from a blind
	// COALESCE across provider keys.
	q := db.queries[0]
	if strings.Contains(q.query, "COALESCE(") {
		t.Fatalf("raw identity must be selected per fact kind, not via COALESCE across providers:\n%s", q.query)
	}
	for _, branch := range []string{
		"WHEN 'aws_resource'",
		"WHEN 'gcp_cloud_resource'",
		"WHEN 'azure_cloud_resource'",
	} {
		if !strings.Contains(q.query, branch) {
			t.Fatalf("query missing per-fact-kind branch %q:\n%s", branch, q.query)
		}
	}
}

// TestPostgresCloudInventoryEvidenceLoaderEmptyGeneration proves an empty
// generation yields no records and no error so an empty scan does not fabricate
// canonical identities.
func TestPostgresCloudInventoryEvidenceLoaderEmptyGeneration(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{rows: [][]any{}}}}
	loader := PostgresCloudInventoryEvidenceLoader{DB: db}

	records, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1")
	if err != nil {
		t.Fatalf("LoadCloudInventoryEvidence() error = %v, want nil", err)
	}
	if len(records) != 0 {
		t.Fatalf("len(records) = %d, want 0", len(records))
	}
}

// TestPostgresCloudInventoryEvidenceLoaderRejectsBlankScopeOrGeneration proves
// the loader refuses a blank scope or generation rather than issuing an
// unbounded scan across every generation.
func TestPostgresCloudInventoryEvidenceLoaderRejectsBlankScopeOrGeneration(t *testing.T) {
	t.Parallel()

	loader := PostgresCloudInventoryEvidenceLoader{DB: &fakeExecQueryer{}}
	if _, err := loader.LoadCloudInventoryEvidence(context.Background(), "  ", "gen-1"); err == nil {
		t.Fatal("LoadCloudInventoryEvidence() with blank scope error = nil, want error")
	}
	if _, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "  "); err == nil {
		t.Fatal("LoadCloudInventoryEvidence() with blank generation error = nil, want error")
	}
}

// TestPostgresCloudInventoryEvidenceLoaderRequiresDB proves a nil database is a
// programmer error surfaced as an error rather than a panic.
func TestPostgresCloudInventoryEvidenceLoaderRequiresDB(t *testing.T) {
	t.Parallel()

	loader := PostgresCloudInventoryEvidenceLoader{}
	if _, err := loader.LoadCloudInventoryEvidence(context.Background(), "cloud:tenant-1", "gen-1"); err == nil {
		t.Fatal("LoadCloudInventoryEvidence() with nil DB error = nil, want error")
	}
}
