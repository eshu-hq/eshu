package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type awsScanCommitStatusStore interface {
	CommitAWSScan(context.Context, awscloud.ScanStatusCommit) error
}

type awsStatusCommitter struct {
	inner               collector.Committer
	statusStore         awsScanCommitStatusStore
	collectorInstanceID string
	clock               func() time.Time
}

func newAWSStatusCommitter(
	inner collector.Committer,
	statusStore awsScanCommitStatusStore,
	collectorInstanceID string,
	clock func() time.Time,
) awsStatusCommitter {
	return awsStatusCommitter{
		inner:               inner,
		statusStore:         statusStore,
		collectorInstanceID: collectorInstanceID,
		clock:               clock,
	}
}

func (c awsStatusCommitter) CommitScopeGeneration(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	return c.inner.CommitScopeGeneration(ctx, scopeValue, generation, factStream)
}

func (c awsStatusCommitter) CommitClaimedScopeGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	committer, ok := c.inner.(collector.ClaimedCommitter)
	if !ok {
		return fmt.Errorf("inner AWS committer must implement ClaimedCommitter")
	}
	err := committer.CommitClaimedScopeGeneration(ctx, mutation, scopeValue, generation, factStream)
	return c.recordCommitOutcome(ctx, mutation, scopeValue, generation, err)
}

func (c awsStatusCommitter) CommitClaimedScopeGenerationWithStreamError(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) error {
	committer, ok := c.inner.(collector.StreamErrorClaimedCommitter)
	if !ok {
		return fmt.Errorf("inner AWS committer must implement StreamErrorClaimedCommitter")
	}
	err := committer.CommitClaimedScopeGenerationWithStreamError(
		ctx,
		mutation,
		scopeValue,
		generation,
		factStream,
		factStreamErr,
	)
	return c.recordCommitOutcome(ctx, mutation, scopeValue, generation, err)
}

func (c awsStatusCommitter) recordCommitOutcome(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	commitErr error,
) error {
	if c.statusStore == nil {
		return commitErr
	}
	boundary, boundaryErr := c.boundary(scopeValue, generation, mutation)
	if boundaryErr != nil {
		if commitErr != nil {
			return commitErr
		}
		return boundaryErr
	}
	statusValue := awscloud.ScanCommitCommitted
	failureClass := ""
	failureMessage := ""
	if commitErr != nil {
		statusValue = awscloud.ScanCommitFailed
		failureClass = "commit_failure"
		failureMessage = commitErr.Error()
	}
	statusErr := c.statusStore.CommitAWSScan(ctx, awscloud.ScanStatusCommit{
		Boundary:       boundary,
		CommitStatus:   statusValue,
		FailureClass:   failureClass,
		FailureMessage: failureMessage,
		CompletedAt:    c.now(),
	})
	if commitErr != nil {
		return errors.Join(commitErr, statusErr)
	}
	return statusErr
}

func (c awsStatusCommitter) boundary(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	mutation workflow.ClaimMutation,
) (awscloud.Boundary, error) {
	accountID := strings.TrimSpace(scopeValue.Metadata["account_id"])
	region := strings.TrimSpace(scopeValue.Metadata["region"])
	serviceKind := strings.TrimSpace(scopeValue.Metadata["service_kind"])
	if accountID == "" || region == "" || serviceKind == "" {
		return awscloud.Boundary{}, fmt.Errorf("AWS scope metadata is missing account, region, or service kind")
	}
	return awscloud.Boundary{
		AccountID:           accountID,
		Region:              region,
		ServiceKind:         serviceKind,
		ScopeID:             scopeValue.ScopeID,
		GenerationID:        generation.GenerationID,
		CollectorInstanceID: c.collectorInstanceID,
		FencingToken:        mutation.FencingToken,
		ObservedAt:          c.now(),
	}, nil
}

func (c awsStatusCommitter) now() time.Time {
	if c.clock != nil {
		return c.clock().UTC()
	}
	return time.Now().UTC()
}

var _ collector.Committer = awsStatusCommitter{}
var _ collector.ClaimedCommitter = awsStatusCommitter{}
var _ collector.StreamErrorClaimedCommitter = awsStatusCommitter{}
