package awssdk

import (
	"fmt"
	"testing"
)

// TestBoundedManagedPolicyStatementsCapsFanOut proves the per-principal managed
// policy document fan-out is bounded: a principal with more attachments than the
// cap fetches at most cap documents, never one document per attachment (N+1).
func TestBoundedManagedPolicyStatementsCapsFanOut(t *testing.T) {
	const maxDocuments = 3
	policyARNs := make([]string, 10)
	for i := range policyARNs {
		policyARNs[i] = fmt.Sprintf("arn:aws:iam::123456789012:policy/p%d", i)
	}

	var fetched int
	doc := `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"*"}]}`
	statements, err := boundedManagedPolicyStatements(policyARNs, maxDocuments, func(string) (string, error) {
		fetched++
		return doc, nil
	})
	if err != nil {
		t.Fatalf("boundedManagedPolicyStatements() error = %v", err)
	}
	if fetched != maxDocuments {
		t.Fatalf("fetched = %d, want %d (fan-out must stay bounded, not 1 call per attachment)", fetched, maxDocuments)
	}
	if len(statements) != maxDocuments {
		t.Fatalf("len(statements) = %d, want %d", len(statements), maxDocuments)
	}
}

// TestBoundedManagedPolicyStatementsPropagatesFetchError proves a fetch failure
// stops the scan rather than silently dropping the principal's permissions.
func TestBoundedManagedPolicyStatementsPropagatesFetchError(t *testing.T) {
	_, err := boundedManagedPolicyStatements([]string{"arn:aws:iam::123456789012:policy/p"}, 5, func(string) (string, error) {
		return "", fmt.Errorf("access denied")
	})
	if err == nil {
		t.Fatal("boundedManagedPolicyStatements() error = nil, want fetch error")
	}
}

// TestBoundedManagedPolicyStatementsSkipsBlankARNs proves blank attachment
// entries are skipped without a wasted fetch.
func TestBoundedManagedPolicyStatementsSkipsBlankARNs(t *testing.T) {
	var fetched int
	_, err := boundedManagedPolicyStatements([]string{"   ", ""}, 5, func(string) (string, error) {
		fetched++
		return "", nil
	})
	if err != nil {
		t.Fatalf("boundedManagedPolicyStatements() error = %v", err)
	}
	if fetched != 0 {
		t.Fatalf("fetched = %d, want 0 (blank ARNs must not trigger a fetch)", fetched)
	}
}
