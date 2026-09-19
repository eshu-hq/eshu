// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/iampolicy"
)

// Production layout (#6785): the awscloud collector emits roles and
// aws_iam_permission into the IAM service scope and buckets into the S3 service
// scope, so a CAN_PERFORM target is never in the permission's own scope.
const (
	crossScopeAccount  = "123456789012"
	crossScopeIAMScope = "aws:123456789012:us-east-1:iam"
	crossScopeS3Scope  = "aws:123456789012:us-east-1:s3"
	crossScopeRoleARN  = "arn:aws:iam::123456789012:role/app-role"
	crossScopeBucket   = "arn:aws:s3:::receipts"
	crossScopeBucketB  = "arn:aws:s3:::receipts-archive"
)

func crossScopeRoleFact() facts.Envelope {
	return resourceEnvelope(crossScopeAccount, "us-east-1", iampolicy.ResourceTypeRole, crossScopeRoleARN, crossScopeRoleARN)
}

func crossScopeBucketFact(arn string) facts.Envelope {
	return resourceEnvelope(crossScopeAccount, "us-east-1", iamCanPerformResourceTypeS3Bucket, arn, arn)
}

func crossScopePermission(actions, resources []string) facts.Envelope {
	env := escalationPermissionEnvelope(crossScopeRoleARN, "Allow", actions, resources)
	env.Payload["account_id"] = crossScopeAccount
	env.Payload["region"] = "us-east-1"
	env.Payload["principal_type"] = "role"
	return env
}

func crossScopeIntent() reducercontract.Intent {
	now := time.Now()
	return reducercontract.Intent{
		IntentID:       "intent-cross-scope-1",
		ScopeID:        crossScopeIAMScope,
		GenerationID:   "iam-gen-1",
		Domain:         reducercontract.DomainIAMCanPerformMaterialization,
		EntityKeys:     []string{"aws_resource_materialization:" + crossScopeIAMScope},
		EnqueuedAt:     now,
		AvailableAt:    now,
		CycleStartedAt: now,
	}
}

// fakeCrossScopeTargets returns a fixed snapshot and records the request.
type fakeCrossScopeTargets struct {
	snapshot CrossScopeTargetSnapshot
	err      error
	requests []CrossScopeTargetRequest
}

func (f *fakeCrossScopeTargets) LoadCrossScopeTargets(
	_ context.Context,
	request CrossScopeTargetRequest,
) (CrossScopeTargetSnapshot, error) {
	f.requests = append(f.requests, request)
	return f.snapshot, f.err
}

func committedS3Scope(bucketFacts ...facts.Envelope) CrossScopeTargetSnapshot {
	return CrossScopeTargetSnapshot{
		Scopes: []CrossScopeTargetScope{{
			ScopeID:            crossScopeS3Scope,
			ActiveGenerationID: "s3-gen-1",
			GenerationActive:   true,
			NodesCommitted:     true,
		}},
		Resources: map[string][]facts.Envelope{crossScopeS3Scope: bucketFacts},
	}
}

func crossScopeHandler(loader CrossScopeTargetLoader, writer *recordingIAMCanPerformWriter) IAMCanPerformMaterializationHandler {
	return IAMCanPerformMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			crossScopeRoleFact(),
			crossScopePermission([]string{"s3:putobject"}, []string{crossScopeBucket}),
		}},
		Writer:            writer,
		CrossScopeTargets: loader,
	}
}

// TestIAMCanPerformResolvesTargetInSiblingServiceScope is the #6785 F1 closing
// test: permission in aws:…:iam, bucket in aws:…:s3. Without the cross-scope
// loader the handler produces 0 edges (the production bug); with it, exactly 1.
func TestIAMCanPerformResolvesTargetInSiblingServiceScope(t *testing.T) {
	t.Parallel()

	t.Run("same-scope only resolves nothing", func(t *testing.T) {
		t.Parallel()
		writer := &recordingIAMCanPerformWriter{}
		if _, err := crossScopeHandler(nil, writer).Handle(context.Background(), crossScopeIntent()); err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(writer.edgeRows) != 0 {
			t.Fatalf("edge rows = %d, want 0 without cross-scope resolution", len(writer.edgeRows))
		}
	})

	t.Run("cross-scope resolves the bucket", func(t *testing.T) {
		t.Parallel()
		writer := &recordingIAMCanPerformWriter{}
		loader := &fakeCrossScopeTargets{snapshot: committedS3Scope(crossScopeBucketFact(crossScopeBucket))}
		result, err := crossScopeHandler(loader, writer).Handle(context.Background(), crossScopeIntent())
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
		if len(writer.edgeRows) != 1 {
			t.Fatalf("edge rows = %d, want 1", len(writer.edgeRows))
		}
		row := writer.edgeRows[0]
		wantResource := cloudjoin.CloudResourceUID(crossScopeAccount, "us-east-1", iamCanPerformResourceTypeS3Bucket, crossScopeBucket)
		if row["resource_uid"] != wantResource {
			t.Fatalf("resource_uid = %v, want %v", row["resource_uid"], wantResource)
		}
		if writer.scopeID != crossScopeIAMScope {
			t.Fatalf("edges written under scope %q, want the permission's scope %q", writer.scopeID, crossScopeIAMScope)
		}
		if result.CanonicalWrites != 1 {
			t.Fatalf("CanonicalWrites = %d, want 1", result.CanonicalWrites)
		}
		if len(loader.requests) != 1 {
			t.Fatalf("loader calls = %d, want 1", len(loader.requests))
		}
		request := loader.requests[0]
		if request.AccountID != crossScopeAccount || request.ExcludeScopeID != crossScopeIAMScope {
			t.Fatalf("request = %+v, want account %s excluding %s", request, crossScopeAccount, crossScopeIAMScope)
		}
		want := []CrossScopeTarget{{ServiceKind: "s3", Region: "", ARN: crossScopeBucket}}
		if !slices.Equal(request.Targets, want) {
			t.Fatalf("targets = %+v, want %+v", request.Targets, want)
		}
	})
}

// TestIAMCanPerformCrossScopeRequestIsBounded proves the loader is asked only
// for exact catalog ARNs in the permission's own account: globs, wildcards,
// non-catalog ARNs, and other accounts are never looked up.
func TestIAMCanPerformCrossScopeRequestIsBounded(t *testing.T) {
	t.Parallel()
	loader := &fakeCrossScopeTargets{snapshot: CrossScopeTargetSnapshot{}}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			crossScopeRoleFact(),
			crossScopePermission(
				[]string{"s3:getobject", "kms:decrypt"},
				[]string{
					"arn:aws:s3:::receipts-*",
					"*",
					"arn:aws:kms:us-west-2:123456789012:key/abc",
					"arn:aws:kms:us-west-2:999999999999:key/other-account",
					"arn:aws:sqs:us-east-1:123456789012:not-in-catalog",
				},
			),
		}},
		Writer:            &recordingIAMCanPerformWriter{},
		CrossScopeTargets: loader,
	}
	// The kms key's scope is not registered, so inside the bound this defers;
	// the assertion here is only about what was requested.
	_, _ = handler.Handle(context.Background(), crossScopeIntent())
	if len(loader.requests) != 1 {
		t.Fatalf("loader calls = %d, want 1", len(loader.requests))
	}
	want := []CrossScopeTarget{{ServiceKind: "kms", Region: "us-west-2", ARN: "arn:aws:kms:us-west-2:123456789012:key/abc"}}
	if got := loader.requests[0].Targets; !slices.Equal(got, want) {
		t.Fatalf("targets = %+v, want %+v", got, want)
	}
}

// TestIAMCanPerformCrossScopeTargetsNeverFeedGlobMatching proves a foreign
// target loaded for an exact ARN is not visible to a glob in the same policy.
// The cross-scope view holds only exactly-named ARNs, so letting a glob match it
// could report single_glob against one bucket while the account has two.
func TestIAMCanPerformCrossScopeTargetsNeverFeedGlobMatching(t *testing.T) {
	t.Parallel()
	writer := &recordingIAMCanPerformWriter{}
	handler := IAMCanPerformMaterializationHandler{
		FactLoader: &stubFactLoader{envelopes: []facts.Envelope{
			crossScopeRoleFact(),
			crossScopePermission([]string{"s3:getobject"}, []string{crossScopeBucket}),
			crossScopePermission([]string{"s3:putobject"}, []string{"arn:aws:s3:::receipts*"}),
		}},
		Writer: writer,
		CrossScopeTargets: &fakeCrossScopeTargets{snapshot: committedS3Scope(
			crossScopeBucketFact(crossScopeBucket),
		)},
	}
	if _, err := handler.Handle(context.Background(), crossScopeIntent()); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if len(writer.edgeRows) != 1 {
		t.Fatalf("edge rows = %d, want 1", len(writer.edgeRows))
	}
	actions, _ := writer.edgeRows[0]["actions"].([]string)
	if !slices.Equal(actions, []string{"s3:getobject"}) {
		t.Fatalf("actions = %v, want only the exact-ARN grant [s3:getobject]", actions)
	}
}

// TestIAMCanPerformDefersWhenCrossScopeTargetNotReady covers both not-ready
// shapes: the target fact is in a scope whose CloudResource nodes have not
// committed, and the target could still land in a scope that has never
// activated but has a pending generation. Neither may write, and both must be a
// retryable readiness class so the queue re-offers the intent.
func TestIAMCanPerformDefersWhenCrossScopeTargetNotReady(t *testing.T) {
	t.Parallel()
	cases := map[string]CrossScopeTargetSnapshot{
		"target nodes not committed": {
			Scopes: []CrossScopeTargetScope{{
				ScopeID: crossScopeS3Scope, ActiveGenerationID: "s3-gen-1", GenerationActive: true,
			}},
			Resources: map[string][]facts.Envelope{crossScopeS3Scope: {crossScopeBucketFact(crossScopeBucket)}},
		},
		"target scope never activated but pending": {
			Scopes: []CrossScopeTargetScope{{ScopeID: crossScopeS3Scope, GenerationPending: true}},
		},
	}
	for name, snapshot := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			writer := &recordingIAMCanPerformWriter{}
			loader := &fakeCrossScopeTargets{snapshot: snapshot}
			_, err := crossScopeHandler(loader, writer).Handle(context.Background(), crossScopeIntent())
			if err == nil {
				t.Fatal("Handle() error = nil, want a readiness defer")
			}
			var classified interface {
				Retryable() bool
				FailureClass() string
			}
			if !errors.As(err, &classified) || !classified.Retryable() {
				t.Fatalf("error %v is not retryable", err)
			}
			if classified.FailureClass() != IAMCanPerformTargetNotReadyFailureClass {
				t.Fatalf("failure class = %q, want %q", classified.FailureClass(), IAMCanPerformTargetNotReadyFailureClass)
			}
			if writer.edgeCalls != 0 || writer.retractCalls != 0 {
				t.Fatalf("writer touched on defer: edges=%d retracts=%d", writer.edgeCalls, writer.retractCalls)
			}
		})
	}
}

// TestIAMCanPerformCommitsPastCrossScopeReadinessBound proves the defer is
// bounded: once the repair cycle is older than the bound, the intent commits
// its best available answer (not-ready targets become unresolved, no edge)
// instead of retrying forever.
func TestIAMCanPerformCommitsPastCrossScopeReadinessBound(t *testing.T) {
	t.Parallel()
	writer := &recordingIAMCanPerformWriter{}
	loader := &fakeCrossScopeTargets{snapshot: CrossScopeTargetSnapshot{
		Scopes: []CrossScopeTargetScope{{
			ScopeID: crossScopeS3Scope, ActiveGenerationID: "s3-gen-1", GenerationActive: true,
		}},
		Resources: map[string][]facts.Envelope{crossScopeS3Scope: {crossScopeBucketFact(crossScopeBucket)}},
	}}
	intent := crossScopeIntent()
	intent.CycleStartedAt = time.Now().Add(-iamCanPerformTargetReadinessMaxWait - time.Minute)
	result, err := crossScopeHandler(loader, writer).Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() error = %v, want commit past the bound", err)
	}
	if len(writer.edgeRows) != 0 || result.CanonicalWrites != 0 {
		t.Fatalf("edges = %d writes = %d, want 0: an uncommitted target must not become an edge", len(writer.edgeRows), result.CanonicalWrites)
	}
}

// TestIAMCanPerformSettledUnresolvedTargetDoesNotDefer proves a target that is
// absent from every settled candidate scope is the honest skipped_unresolved,
// not a defer. A newer pending generation beside an active one does not defer
// either, or rolling collection would hold CAN_PERFORM at the bound forever.
func TestIAMCanPerformSettledUnresolvedTargetDoesNotDefer(t *testing.T) {
	t.Parallel()
	writer := &recordingIAMCanPerformWriter{}
	loader := &fakeCrossScopeTargets{snapshot: CrossScopeTargetSnapshot{
		Scopes: []CrossScopeTargetScope{{
			ScopeID: crossScopeS3Scope, ActiveGenerationID: "s3-gen-1", GenerationActive: true,
			NodesCommitted: true, GenerationPending: true,
		}},
		Resources: map[string][]facts.Envelope{crossScopeS3Scope: {crossScopeBucketFact(crossScopeBucketB)}},
	}}
	if _, err := crossScopeHandler(loader, writer).Handle(context.Background(), crossScopeIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want a committed unresolved answer", err)
	}
	if len(writer.edgeRows) != 0 {
		t.Fatalf("edge rows = %d, want 0", len(writer.edgeRows))
	}
}

// TestIAMCanPerformCrossScopeLoaderErrorIsNotAReadinessMiss proves a store that
// cannot answer fails the intent as an ordinary error rather than hiding in the
// non-counting readiness class, where it could retry forever unseen.
func TestIAMCanPerformCrossScopeLoaderErrorIsNotAReadinessMiss(t *testing.T) {
	t.Parallel()
	loader := &fakeCrossScopeTargets{err: errors.New("postgres unavailable")}
	_, err := crossScopeHandler(loader, &recordingIAMCanPerformWriter{}).Handle(context.Background(), crossScopeIntent())
	if err == nil {
		t.Fatal("Handle() error = nil, want the loader error")
	}
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) && classified.FailureClass() == IAMCanPerformTargetNotReadyFailureClass {
		t.Fatalf("loader error classified as readiness miss: %v", err)
	}
}

// TestIAMCanPerformDefersWhenTargetScopeIsUnregistered is the #6785 adverse
// order: the iam scope's CAN_PERFORM intent runs before the s3 scope holding
// its target is even registered. That must be a retryable defer, not a
// success with 0 edges that nothing re-runs. Once the s3 scope registers and
// commits its nodes, the retried intent writes the edge.
func TestIAMCanPerformDefersWhenTargetScopeIsUnregistered(t *testing.T) {
	t.Parallel()
	writer := &recordingIAMCanPerformWriter{}
	loader := &fakeCrossScopeTargets{snapshot: CrossScopeTargetSnapshot{}}
	handler := crossScopeHandler(loader, writer)

	_, err := handler.Handle(context.Background(), crossScopeIntent())
	var classified interface {
		Retryable() bool
		FailureClass() string
	}
	if err == nil || !errors.As(err, &classified) || !classified.Retryable() ||
		classified.FailureClass() != IAMCanPerformTargetNotReadyFailureClass {
		t.Fatalf("Handle() error = %v, want a retryable %s defer while the s3 scope is unregistered", err, IAMCanPerformTargetNotReadyFailureClass)
	}
	if writer.edgeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("writer touched on defer: edges=%d retracts=%d", writer.edgeCalls, writer.retractCalls)
	}

	loader.snapshot = committedS3Scope(crossScopeBucketFact(crossScopeBucket))
	if _, err := handler.Handle(context.Background(), crossScopeIntent()); err != nil {
		t.Fatalf("retry Handle() error = %v", err)
	}
	if len(writer.edgeRows) != 1 {
		t.Fatalf("edge rows after the s3 scope registered = %d, want 1", len(writer.edgeRows))
	}
}

// TestIAMCanPerformExpectedScopeFollowsTheTargetARN proves the expected scope
// is derived from the ARN: a kms key in us-west-2 is not satisfied by a
// registered kms scope in another region, while an s3 bucket (whose ARN names
// no region) is satisfied by any registered s3 scope of the account.
func TestIAMCanPerformExpectedScopeFollowsTheTargetARN(t *testing.T) {
	t.Parallel()
	kmsKey := "arn:aws:kms:us-west-2:123456789012:key/abc"
	otherRegionKMS := CrossScopeTargetSnapshot{Scopes: []CrossScopeTargetScope{{
		ScopeID: "aws:123456789012:us-east-1:kms", ActiveGenerationID: "g", GenerationActive: true, NodesCommitted: true,
	}}}
	decided := decideCrossScopeTargets([]CrossScopeTarget{{ServiceKind: "kms", Region: "us-west-2", ARN: kmsKey}}, otherRegionKMS)
	if decided.notReady != 1 || decided.outcomes[crossScopeTargetScopeUnregistered] != 1 {
		t.Fatalf("kms in unregistered region: notReady=%d outcomes=%v, want 1 scope_unregistered", decided.notReady, decided.outcomes)
	}
	anyRegionS3 := CrossScopeTargetSnapshot{Scopes: []CrossScopeTargetScope{{
		ScopeID: "aws:123456789012:eu-west-1:s3", ActiveGenerationID: "g", GenerationActive: true, NodesCommitted: true,
	}}}
	decided = decideCrossScopeTargets([]CrossScopeTarget{{ServiceKind: "s3", ARN: crossScopeBucket}}, anyRegionS3)
	if decided.notReady != 0 || decided.outcomes[crossScopeTargetUnresolved] != 1 {
		t.Fatalf("s3 bucket with a settled s3 scope: notReady=%d outcomes=%v, want 1 unresolved", decided.notReady, decided.outcomes)
	}
}
