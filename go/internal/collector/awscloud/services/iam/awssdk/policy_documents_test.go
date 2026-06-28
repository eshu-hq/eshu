// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// TestBoundedPermissionBoundaryStatementsFetchesOneBoundaryDocument proves a
// permissions boundary is read as one managed policy document and tagged as a
// ceiling source, not as an attached identity grant.
func TestBoundedPermissionBoundaryStatementsFetchesOneBoundaryDocument(t *testing.T) {
	const boundaryARN = "arn:aws:iam::123456789012:policy/developer-boundary"

	var fetched int
	doc := `{"Statement":[{"Effect":"Allow","Action":"s3:GetObject","Resource":"arn:aws:s3:::prod-data/*"}]}`
	statements, err := boundedPermissionBoundaryStatements(boundaryARN, func(policyARN string) (string, error) {
		fetched++
		if policyARN != boundaryARN {
			t.Fatalf("policyARN = %q, want %q", policyARN, boundaryARN)
		}
		return doc, nil
	})
	if err != nil {
		t.Fatalf("boundedPermissionBoundaryStatements() error = %v", err)
	}
	if fetched != 1 {
		t.Fatalf("fetched = %d, want 1", fetched)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].Source != "permission_boundary" {
		t.Fatalf("Source = %q, want permission_boundary", statements[0].Source)
	}
	if statements[0].PolicyARN != boundaryARN {
		t.Fatalf("PolicyARN = %q, want %q", statements[0].PolicyARN, boundaryARN)
	}
}

// TestBoundedPermissionBoundaryStatementsSkipsBlankBoundaryARN proves missing
// boundary references do not waste a policy document fetch.
func TestBoundedPermissionBoundaryStatementsSkipsBlankBoundaryARN(t *testing.T) {
	var fetched int
	statements, err := boundedPermissionBoundaryStatements("  ", func(string) (string, error) {
		fetched++
		return "", nil
	})
	if err != nil {
		t.Fatalf("boundedPermissionBoundaryStatements() error = %v", err)
	}
	if fetched != 0 {
		t.Fatalf("fetched = %d, want 0", fetched)
	}
	if len(statements) != 0 {
		t.Fatalf("len(statements) = %d, want 0", len(statements))
	}
}

// TestBoundedPermissionBoundaryStatementsPropagatesFetchError proves an
// unresolved boundary policy document remains visible to the caller instead of
// being silently dropped.
func TestBoundedPermissionBoundaryStatementsPropagatesFetchError(t *testing.T) {
	_, err := boundedPermissionBoundaryStatements("arn:aws:iam::123456789012:policy/developer-boundary", func(string) (string, error) {
		return "", fmt.Errorf("access denied")
	})
	if err == nil {
		t.Fatal("boundedPermissionBoundaryStatements() error = nil, want fetch error")
	}
}

// TestBoundedManagedPolicyStatementsSourceLabelIsAttachedManaged proves that
// every statement produced by boundedManagedPolicyStatements carries
// Source="attached_managed" so the reducer can distinguish managed-policy
// grants from inline and boundary grants.
func TestBoundedManagedPolicyStatementsSourceLabelIsAttachedManaged(t *testing.T) {
	const policyARN = "arn:aws:iam::123456789012:policy/ReadOnly"
	doc := `{"Statement":[{"Effect":"Allow","Action":["s3:GetObject","s3:ListBucket"],"Resource":"*"}]}`
	statements, err := boundedManagedPolicyStatements([]string{policyARN}, 5, func(string) (string, error) {
		return doc, nil
	})
	if err != nil {
		t.Fatalf("boundedManagedPolicyStatements() error = %v", err)
	}
	if len(statements) != 1 {
		t.Fatalf("len(statements) = %d, want 1", len(statements))
	}
	if statements[0].Source != "attached_managed" {
		t.Fatalf("Source = %q, want attached_managed", statements[0].Source)
	}
	if statements[0].PolicyARN != policyARN {
		t.Fatalf("PolicyARN = %q, want %q", statements[0].PolicyARN, policyARN)
	}
}
