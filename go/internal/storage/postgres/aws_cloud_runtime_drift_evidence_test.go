package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/cloudruntime"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

type stubAWSRuntimeDriftConfigResolver struct {
	anchors      map[string]tfstatebackend.CommitAnchor
	calls        []string
	err          error
	errByBackend map[string]error
}

func (s *stubAWSRuntimeDriftConfigResolver) ResolveConfigCommitForBackend(
	_ context.Context,
	backendKind string,
	locatorHash string,
) (tfstatebackend.CommitAnchor, error) {
	s.calls = append(s.calls, backendKind+":"+locatorHash)
	if err := s.errByBackend[backendKind+":"+locatorHash]; err != nil {
		return tfstatebackend.CommitAnchor{}, err
	}
	if s.err != nil {
		return tfstatebackend.CommitAnchor{}, s.err
	}
	anchor, ok := s.anchors[backendKind+":"+locatorHash]
	if !ok {
		return tfstatebackend.CommitAnchor{}, tfstatebackend.ErrNoConfigRepoOwnsBackend
	}
	return anchor, nil
}

func TestPostgresAWSCloudRuntimeDriftEvidenceLoaderJoinsCloudStateAndConfig(t *testing.T) {
	t.Parallel()

	const (
		awsScopeID    = "aws:123456789012:us-east-1:lambda"
		awsGeneration = "aws-gen-1"
		stateScopeID  = "state_snapshot:s3:hash-xyz"
		stateGen      = "state-gen-1"
		configScopeID = "repository:infra"
		configGen     = "config-gen-1"
	)
	orphanARN := "arn:aws:iam::123456789012:role/orphan"
	unmanagedARN := "arn:aws:lambda:us-east-1:123456789012:function:unmanaged"
	managedARN := "arn:aws:s3:::managed-bucket"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{orphanARN, []byte(`{
					"arn":"` + orphanARN + `",
					"resource_id":"orphan",
					"resource_type":"aws_iam_role",
					"tags":{"Environment":"prod"}
				}`)},
				{unmanagedARN, []byte(`{
					"arn":"` + unmanagedARN + `",
					"resource_id":"unmanaged",
					"resource_type":"aws_lambda_function",
					"tags":{"Owner":"platform"}
				}`)},
				{managedARN, []byte(`{
					"arn":"` + managedARN + `",
					"resource_id":"managed-bucket",
					"resource_type":"aws_s3_bucket",
					"tags":{"Environment":"prod"}
				}`)},
			}},
			{rows: [][]any{
				{stateScopeID, stateGen, "aws_lambda_function.unmanaged", fixtureAWSRuntimeStatePayload(
					"aws_lambda_function.unmanaged", "aws_lambda_function", unmanagedARN,
				)},
				{stateScopeID, stateGen, "aws_s3_bucket.managed", fixtureAWSRuntimeStatePayload(
					"aws_s3_bucket.managed", "aws_s3_bucket", managedARN,
				)},
			}},
			// Module-prefix walk for configScopeID/configGen.
			{rows: [][]any{}},
			// Config-side terraform_resources rows. Only the managed state
			// address is declared; unmanaged stays cloud+state without config.
			{rows: [][]any{{
				fixtureConfigResourcesArray(fixtureConfigParserRow("aws_s3_bucket", "managed")),
			}}},
		},
	}
	resolver := &stubAWSRuntimeDriftConfigResolver{
		anchors: map[string]tfstatebackend.CommitAnchor{
			"s3:hash-xyz": {
				RepoID:      "infra",
				ScopeID:     configScopeID,
				CommitID:    configGen,
				BackendKind: "s3",
				LocatorHash: "hash-xyz",
			},
		},
	}
	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{
		DB:             db,
		ConfigResolver: resolver,
	}

	rows, err := loader.LoadAWSCloudRuntimeDriftEvidence(context.Background(), awsScopeID, awsGeneration)
	if err != nil {
		t.Fatalf("LoadAWSCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	byARN := make(map[string]bool, len(rows))
	for _, row := range rows {
		byARN[row.ARN] = true
		switch row.ARN {
		case orphanARN:
			if row.Cloud == nil || row.State != nil || row.Config != nil {
				t.Fatalf("orphan row = %#v, want cloud only", row)
			}
			if got, want := row.Cloud.Tags["Environment"], "prod"; got != want {
				t.Fatalf("orphan cloud tag = %q, want %q", got, want)
			}
		case unmanagedARN:
			if row.Cloud == nil || row.State == nil || row.Config != nil {
				t.Fatalf("unmanaged row = %#v, want cloud+state only", row)
			}
			if got, want := row.State.Address, "aws_lambda_function.unmanaged"; got != want {
				t.Fatalf("unmanaged state address = %q, want %q", got, want)
			}
		case managedARN:
			if row.Cloud == nil || row.State == nil || row.Config == nil {
				t.Fatalf("managed row = %#v, want cloud+state+config", row)
			}
			if got, want := row.Config.Address, "aws_s3_bucket.managed"; got != want {
				t.Fatalf("managed config address = %q, want %q", got, want)
			}
		default:
			t.Fatalf("unexpected row ARN %q", row.ARN)
		}
	}
	for _, arn := range []string{orphanARN, unmanagedARN, managedARN} {
		if !byARN[arn] {
			t.Fatalf("missing row for ARN %q", arn)
		}
	}
	if got, want := resolver.calls, []string{"s3:hash-xyz"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("resolver calls = %#v, want %#v", got, want)
	}
	if got, want := len(db.queries), 4; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
	stateJoin := db.queries[1]
	if !strings.Contains(stateJoin.query, "jsonb_array_elements_text($3::jsonb)") {
		t.Fatalf("state join is not bounded by AWS ARN allowlist:\n%s", stateJoin.query)
	}
	if got, want := stateJoin.args[0], awsScopeID; got != want {
		t.Fatalf("state join aws scope arg = %v, want %v", got, want)
	}
	if got, want := stateJoin.args[1], awsGeneration; got != want {
		t.Fatalf("state join aws generation arg = %v, want %v", got, want)
	}
}

func TestPostgresAWSCloudRuntimeDriftEvidenceLoaderMarksUnknownAndAmbiguousMatches(t *testing.T) {
	t.Parallel()

	const (
		awsScopeID       = "aws:123456789012:us-east-1:lambda"
		awsGeneration    = "aws-gen-1"
		unknownScopeID   = "state_snapshot:s3:missing-owner"
		ambiguousScopeID = "state_snapshot:s3:ambiguous-owner"
		stateGen         = "state-gen-1"
	)
	unknownARN := "arn:aws:lambda:us-east-1:123456789012:function:unknown"
	ambiguousARN := "arn:aws:s3:::ambiguous-bucket"

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{
				{unknownARN, []byte(`{
					"arn":"` + unknownARN + `",
					"resource_id":"unknown",
					"resource_type":"aws_lambda_function"
				}`)},
				{ambiguousARN, []byte(`{
					"arn":"` + ambiguousARN + `",
					"resource_id":"ambiguous-bucket",
					"resource_type":"aws_s3_bucket"
				}`)},
			}},
			{rows: [][]any{
				{unknownScopeID, stateGen, "aws_lambda_function.unknown", fixtureAWSRuntimeStatePayload(
					"aws_lambda_function.unknown", "aws_lambda_function", unknownARN,
				)},
				{ambiguousScopeID, stateGen, "aws_s3_bucket.ambiguous_a", fixtureAWSRuntimeStatePayload(
					"aws_s3_bucket.ambiguous_a", "aws_s3_bucket", ambiguousARN,
				)},
				{ambiguousScopeID, stateGen, "aws_s3_bucket.ambiguous_b", fixtureAWSRuntimeStatePayload(
					"aws_s3_bucket.ambiguous_b", "aws_s3_bucket", ambiguousARN,
				)},
			}},
		},
	}
	resolver := &stubAWSRuntimeDriftConfigResolver{
		errByBackend: map[string]error{
			"s3:ambiguous-owner": tfstatebackend.ErrAmbiguousBackendOwner,
		},
	}
	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{
		DB:             db,
		ConfigResolver: resolver,
	}

	rows, err := loader.LoadAWSCloudRuntimeDriftEvidence(context.Background(), awsScopeID, awsGeneration)
	if err != nil {
		t.Fatalf("LoadAWSCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	for _, row := range rows {
		switch row.ARN {
		case unknownARN:
			if got, want := row.FindingKind, cloudruntime.FindingKindUnknownCloudResource; got != want {
				t.Fatalf("unknown row FindingKind = %q, want %q", got, want)
			}
			if row.Config != nil {
				t.Fatalf("unknown row Config = %#v, want nil", row.Config)
			}
		case ambiguousARN:
			if got, want := row.FindingKind, cloudruntime.FindingKindAmbiguousCloudResource; got != want {
				t.Fatalf("ambiguous row FindingKind = %q, want %q", got, want)
			}
			if !stringSliceContains(row.WarningFlags, "ambiguous_terraform_state_owner") {
				t.Fatalf("ambiguous row warnings = %#v, want state-owner warning", row.WarningFlags)
			}
		default:
			t.Fatalf("unexpected ARN %q", row.ARN)
		}
	}
}

func TestPostgresAWSCloudRuntimeDriftEvidenceLoaderSkipsStateJoinWithoutAWSRows(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{{rows: [][]any{}}},
	}
	loader := PostgresAWSCloudRuntimeDriftEvidenceLoader{
		DB:             db,
		ConfigResolver: &stubAWSRuntimeDriftConfigResolver{},
	}

	rows, err := loader.LoadAWSCloudRuntimeDriftEvidence(context.Background(), "aws:scope", "aws-gen")
	if err != nil {
		t.Fatalf("LoadAWSCloudRuntimeDriftEvidence() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(rows))
	}
	if got, want := len(db.queries), 1; got != want {
		t.Fatalf("query count = %d, want %d", got, want)
	}
}

func TestPostgresAWSCloudRuntimeDriftEvidenceLoaderRequiresDatabase(t *testing.T) {
	t.Parallel()

	_, err := PostgresAWSCloudRuntimeDriftEvidenceLoader{}.LoadAWSCloudRuntimeDriftEvidence(
		context.Background(),
		"aws:scope",
		"aws-gen",
	)
	if err == nil {
		t.Fatal("LoadAWSCloudRuntimeDriftEvidence() error = nil, want missing database error")
	}
}

func fixtureAWSRuntimeStatePayload(address, resourceType, arn string) []byte {
	return []byte(`{
		"address":"` + address + `",
		"mode":"managed",
		"type":"` + resourceType + `",
		"name":"runtime",
		"attributes":{"arn":"` + arn + `"}
	}`)
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
