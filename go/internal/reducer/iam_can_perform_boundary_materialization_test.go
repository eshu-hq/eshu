// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type filteringIAMCanPerformFactLoader struct {
	envelopes []facts.Envelope
	factKinds []string
}

func (f *filteringIAMCanPerformFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return f.envelopes, nil
}

func (f *filteringIAMCanPerformFactLoader) ListFactsByKind(
	_ context.Context,
	_ string,
	_ string,
	factKinds []string,
) ([]facts.Envelope, error) {
	f.factKinds = append([]string(nil), factKinds...)
	allowed := make(map[string]struct{}, len(factKinds))
	for _, factKind := range factKinds {
		allowed[factKind] = struct{}{}
	}
	filtered := make([]facts.Envelope, 0, len(f.envelopes))
	for _, envelope := range f.envelopes {
		if _, ok := allowed[envelope.FactKind]; ok {
			filtered = append(filtered, envelope)
		}
	}
	return filtered, nil
}

func TestIAMCanPerformHandlerLoadsPermissionBoundaryFacts(t *testing.T) {
	t.Parallel()

	loader := &filteringIAMCanPerformFactLoader{
		envelopes: []facts.Envelope{
			attackerNode(),
			canPerformNode(iamCanPerformResourceTypeS3Bucket, canPerformBucketARN),
			escalationPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
			canPerformBoundaryEnvelope(attackerUserARN),
			canPerformBoundaryPermissionEnvelope(attackerUserARN, "Allow", []string{"s3:getobject"}, []string{canPerformBucketARN}),
		},
	}
	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader:           loader,
		Writer:               writer,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), iamCanPerformIntent())
	if err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if !factKindRequested(loader.factKinds, facts.AWSIAMPermissionBoundaryFactKind) {
		t.Fatalf("requested fact kinds = %v, want aws_iam_permission_boundary included", loader.factKinds)
	}
	if result.CanonicalWrites != 1 || len(writer.edgeRows) != 1 {
		t.Fatalf("CanonicalWrites=%d rows=%v, want one boundary-evaluated edge", result.CanonicalWrites, writer.edgeRows)
	}
	if writer.edgeRows[0]["evaluation_scope"] != "identity_policy_with_permission_boundary" {
		t.Fatalf("evaluation_scope = %v, want identity_policy_with_permission_boundary", writer.edgeRows[0]["evaluation_scope"])
	}
}

func factKindRequested(factKinds []string, want string) bool {
	for _, factKind := range factKinds {
		if factKind == want {
			return true
		}
	}
	return false
}
