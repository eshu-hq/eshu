// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// SecretsIAMTrustChainState names the reducer read-model truth state for one
// secrets/IAM path, posture observation, or gap.
type SecretsIAMTrustChainState string

const (
	// SecretsIAMTrustChainStateExact means every required source hop resolved
	// through explicit redaction-safe evidence.
	SecretsIAMTrustChainStateExact SecretsIAMTrustChainState = "exact"
	// SecretsIAMTrustChainStatePartial means some source evidence exists but at
	// least one required hop is missing or broad.
	SecretsIAMTrustChainStatePartial SecretsIAMTrustChainState = "partial"
	// SecretsIAMTrustChainStateUnresolved means a required hop did not resolve.
	SecretsIAMTrustChainStateUnresolved SecretsIAMTrustChainState = "unresolved"
	// SecretsIAMTrustChainStateStale means same-source generations disagree for
	// evidence that must be current together.
	SecretsIAMTrustChainStateStale SecretsIAMTrustChainState = "stale"
	// SecretsIAMTrustChainStatePermissionHidden means a source reported hidden
	// evidence rather than a complete empty result.
	SecretsIAMTrustChainStatePermissionHidden SecretsIAMTrustChainState = "permission_hidden"
	// SecretsIAMTrustChainStateUnsupported means an uncollected policy layer is
	// required for exact truth.
	SecretsIAMTrustChainStateUnsupported SecretsIAMTrustChainState = "unsupported"
)

// SecretsIAMTrustChainReadModels groups all reducer-owned read-model outputs
// for one secrets/IAM evidence packet.
type SecretsIAMTrustChainReadModels struct {
	IdentityTrustChains          []SecretsIAMIdentityTrustChain
	PrivilegePostureObservations []SecretsIAMPrivilegePostureObservation
	SecretAccessPaths            []SecretsIAMSecretAccessPath
	PostureGaps                  []SecretsIAMPostureGap
}

// SecretsIAMIdentityTrustChain is an explainable workload-to-identity chain.
// Identifiers are stable fingerprints or source-safe object IDs; raw IAM role
// ARNs, ServiceAccount names, namespaces, Vault role names, and paths are not
// stored.
type SecretsIAMIdentityTrustChain struct {
	ChainID               string
	State                 SecretsIAMTrustChainState
	Confidence            string
	ServiceAccountJoinKey string
	WorkloadObjectID      string
	WorkloadKind          string
	IAMRoleFingerprint    string
	// IAMRoleCloudResourceUID is the redaction-safe CloudResource node uid for the
	// assumed IAM role, when it is resolvable at trust-chain build time. It lets
	// the graph projection promote SECRETS_IAM_ASSUMES_IAM_ROLE to the existing
	// IAM-role CloudResource node. It is empty when the build site cannot resolve a
	// joinable identity, in which case the edge stays skipped+counted (ADR #1314
	// §5.1). The raw role ARN is never stored; this is the same one-way uid the AWS
	// resource projection computes.
	IAMRoleCloudResourceUID string
	// IAMRoleAssumeMode is the bounded assume-mode classification
	// (web_identity / pod_identity) for the IAM-role edge, or empty when
	// unclassified. It never encodes a role name, ARN, or account value.
	IAMRoleAssumeMode                 string
	GCPServiceAccountFingerprint      string
	GCPServiceAccountCloudResourceUID string
	GCPServiceAccountAssumeMode       string
	VaultRoleJoinKey                  string
	VaultMountJoinKey                 string
	VaultPolicyJoinKeys               []string
	EvidenceFactIDs                   []string
	MissingEvidence                   []string
	SourceScopes                      []string
	SourceGenerations                 []string
}

// SecretsIAMPrivilegePostureObservation records risky broad or partial posture
// evidence that must not become an exact path.
type SecretsIAMPrivilegePostureObservation struct {
	ObservationID      string
	RiskType           string
	Severity           string
	State              SecretsIAMTrustChainState
	Confidence         string
	SubjectFingerprint string
	Reason             string
	EvidenceFactIDs    []string
}

// SecretsIAMSecretAccessPath is a Vault policy-to-KV metadata path reachable
// from an exact identity chain.
type SecretsIAMSecretAccessPath struct {
	PathID                         string
	ChainID                        string
	State                          SecretsIAMTrustChainState
	Confidence                     string
	KVPathFingerprint              string
	VaultMountJoinKey              string
	VaultPolicyJoinKey             string
	CloudProvider                  string
	CloudSecretResourceFingerprint string
	Capabilities                   []string
	EvidenceFactIDs                []string
}

// SecretsIAMPostureGap records missing, stale, hidden, or unsupported evidence
// that prevents exact trust-chain truth.
type SecretsIAMPostureGap struct {
	GapID                 string
	GapType               string
	State                 SecretsIAMTrustChainState
	Reason                string
	ServiceAccountJoinKey string
	EvidenceFactIDs       []string
	MissingEvidence       []string
	UnsupportedLayers     []string
}

// SecretsIAMTrustChainLoadStats summarizes the bounded evidence load.
type SecretsIAMTrustChainLoadStats struct {
	SeedFactCount   int
	LoadedFactCount int
	Truncated       bool
}

// SecretsIAMTrustChainEvidenceLoader loads the source-fact packet used by the
// secrets/IAM trust-chain reducer. Implementations should bound reads by the
// trigger fact's explicit join anchors and return unsupported/partial evidence
// instead of broad active-table scans.
type SecretsIAMTrustChainEvidenceLoader interface {
	LoadSecretsIAMTrustChainEvidence(
		context.Context,
		Intent,
	) ([]facts.Envelope, SecretsIAMTrustChainLoadStats, error)
}

// SecretsIAMTrustChainWrite carries one reducer publication request.
type SecretsIAMTrustChainWrite struct {
	IntentID     string
	ScopeID      string
	GenerationID string
	SourceSystem string
	Cause        string
	Models       SecretsIAMTrustChainReadModels
	LoadStats    SecretsIAMTrustChainLoadStats
}

// SecretsIAMTrustChainWriteResult summarizes durable read-model writes.
type SecretsIAMTrustChainWriteResult struct {
	FactsWritten    int
	EvidenceSummary string
}

// SecretsIAMTrustChainWriter persists reducer-owned secrets/IAM read models.
type SecretsIAMTrustChainWriter interface {
	WriteSecretsIAMTrustChainReadModels(
		context.Context,
		SecretsIAMTrustChainWrite,
	) (SecretsIAMTrustChainWriteResult, error)
}

// SecretsIAMTrustChainHandler builds secrets/IAM read-model outputs from
// bounded AWS IAM, Kubernetes, and Vault source facts.
type SecretsIAMTrustChainHandler struct {
	EvidenceLoader SecretsIAMTrustChainEvidenceLoader
	Writer         SecretsIAMTrustChainWriter
	Instruments    *telemetry.Instruments
}

// Handle executes one secrets/IAM reducer intent.
func (h SecretsIAMTrustChainHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainSecretsIAMTrustChain {
		return Result{}, fmt.Errorf("secrets_iam_trust_chain handler does not accept domain %q", intent.Domain)
	}
	if h.EvidenceLoader == nil {
		return Result{}, fmt.Errorf("secrets/IAM trust-chain evidence loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("secrets/IAM trust-chain writer is required")
	}
	envelopes, stats, err := h.EvidenceLoader.LoadSecretsIAMTrustChainEvidence(ctx, intent)
	if err != nil {
		return Result{}, fmt.Errorf("load secrets/IAM trust-chain evidence: %w", err)
	}
	// BuildSecretsIAMTrustChainReadModels decodes the aws_iam_principal facts it
	// uses to resolve assumed-role CloudResource uids through the factschema seam.
	// A malformed aws_iam_principal fact (a missing required identity field)
	// dead-letters the ENTIRE secrets/IAM trust-chain work item for this scope as
	// input_invalid, not just its own chain, because the read-model build is a
	// single pass over the scope's facts. That is the intended, visible,
	// replayable outcome: these facts are emitter-guaranteed to carry account_id
	// and region, so a decode failure signals a genuine collector defect rather
	// than a routine absence. The other providers' chains (K8s/GCP/Vault) still
	// read raw and are unaffected unless an aws_iam_principal fact is malformed.
	models, err := BuildSecretsIAMTrustChainReadModels(envelopes)
	if err != nil {
		return Result{}, err
	}
	writeResult, err := h.Writer.WriteSecretsIAMTrustChainReadModels(ctx, SecretsIAMTrustChainWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Models:       models,
		LoadStats:    stats,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write secrets/IAM trust-chain read models: %w", err)
	}
	h.emitCounters(ctx, models)
	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSecretsIAMTrustChain,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: secretsIAMTrustChainSummary(models, stats, writeResult.FactsWritten),
		CanonicalWrites: writeResult.FactsWritten,
	}, nil
}

func (h SecretsIAMTrustChainHandler) emitCounters(ctx context.Context, models SecretsIAMTrustChainReadModels) {
	if h.Instruments == nil {
		return
	}
	if h.Instruments.SecretsIAMReducerTrustChains != nil {
		counts := map[SecretsIAMTrustChainState]int{}
		for _, chain := range models.IdentityTrustChains {
			counts[chain.State]++
		}
		for state, count := range counts {
			h.Instruments.SecretsIAMReducerTrustChains.Add(ctx, int64(count), metric.WithAttributes(
				telemetry.AttrResult(string(state)),
				telemetry.AttrConfidence(secretsIAMConfidenceForState(state)),
			))
		}
	}
	if h.Instruments.SecretsIAMPostureObservations != nil {
		for _, observation := range models.PrivilegePostureObservations {
			h.Instruments.SecretsIAMPostureObservations.Add(ctx, 1, metric.WithAttributes(
				telemetry.AttrRiskType(observation.RiskType),
				telemetry.AttrSeverity(observation.Severity),
			))
		}
	}
}

func secretsIAMTrustChainSummary(
	models SecretsIAMTrustChainReadModels,
	stats SecretsIAMTrustChainLoadStats,
	factsWritten int,
) string {
	return fmt.Sprintf(
		"secrets/IAM trust-chain seed_facts=%d loaded_facts=%d identity_chains=%d posture_observations=%d secret_access_paths=%d posture_gaps=%d facts_written=%d truncated=%t",
		stats.SeedFactCount,
		stats.LoadedFactCount,
		len(models.IdentityTrustChains),
		len(models.PrivilegePostureObservations),
		len(models.SecretAccessPaths),
		len(models.PostureGaps),
		factsWritten,
		stats.Truncated,
	)
}

func secretsIAMConfidenceForState(state SecretsIAMTrustChainState) string {
	if state == SecretsIAMTrustChainStateExact {
		return "exact"
	}
	if state == SecretsIAMTrustChainStateUnsupported || state == SecretsIAMTrustChainStateUnresolved {
		return "unknown"
	}
	return "partial"
}
