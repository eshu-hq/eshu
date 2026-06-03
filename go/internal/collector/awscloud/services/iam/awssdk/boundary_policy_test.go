package awssdk

import (
	"fmt"
	"testing"

	iamservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/iam"
)

// TestBoundaryPolicyStatementsNormalizesWithBoundarySource proves the
// permission-boundary document is fetched and normalized into metadata-only
// statements tagged policy_source = "boundary", reusing the same identity-policy
// normalization path that produces derived effect/actions/resources/condition-key
// names and never raw JSON.
func TestBoundaryPolicyStatementsNormalizesWithBoundarySource(t *testing.T) {
	boundaryARN := "arn:aws:iam::123456789012:policy/developer-boundary"
	doc := `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::team-bucket","Condition":{"StringEquals":{"aws:RequestedRegion":"us-east-1"}}}]}`

	var fetchedARN string
	statements, err := boundaryPolicyStatements(boundaryARN, func(arn string) (string, error) {
		fetchedARN = arn
		return doc, nil
	})
	if err != nil {
		t.Fatalf("boundaryPolicyStatements() error = %v", err)
	}
	if fetchedARN != boundaryARN {
		t.Fatalf("fetched ARN = %q, want %q", fetchedARN, boundaryARN)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	got := statements[0]
	if got.Source != iamservice.PolicySourceBoundary {
		t.Fatalf("Source = %q, want %q", got.Source, iamservice.PolicySourceBoundary)
	}
	if got.PolicyARN != boundaryARN {
		t.Fatalf("PolicyARN = %q, want %q", got.PolicyARN, boundaryARN)
	}
	if got.Effect != "Allow" || len(got.Actions) != 1 || got.Actions[0] != "s3:GetObject" {
		t.Fatalf("normalized statement = %#v, want one Allow s3:GetObject", got)
	}
	if len(got.ConditionKeys) != 1 || got.ConditionKeys[0] != "aws:RequestedRegion" {
		t.Fatalf("ConditionKeys = %v, want [aws:RequestedRegion] (key name only, no value)", got.ConditionKeys)
	}
}

// TestBoundaryPolicyStatementsSkipsBlankBoundaryARN proves a principal with no
// permission boundary triggers no boundary fetch and yields no boundary statements.
func TestBoundaryPolicyStatementsSkipsBlankBoundaryARN(t *testing.T) {
	var fetched int
	statements, err := boundaryPolicyStatements("   ", func(string) (string, error) {
		fetched++
		return "", nil
	})
	if err != nil {
		t.Fatalf("boundaryPolicyStatements() error = %v", err)
	}
	if fetched != 0 {
		t.Fatalf("fetched = %d, want 0 (no boundary means no fetch)", fetched)
	}
	if len(statements) != 0 {
		t.Fatalf("len(statements) = %d, want 0", len(statements))
	}
}

// TestBoundaryPolicyStatementsPropagatesFetchError proves a boundary fetch failure
// stops the scan rather than silently dropping the ceiling and over-reporting
// effective permissions.
func TestBoundaryPolicyStatementsPropagatesFetchError(t *testing.T) {
	_, err := boundaryPolicyStatements("arn:aws:iam::123456789012:policy/b", func(string) (string, error) {
		return "", fmt.Errorf("access denied")
	})
	if err == nil {
		t.Fatal("boundaryPolicyStatements() error = nil, want fetch error")
	}
}
