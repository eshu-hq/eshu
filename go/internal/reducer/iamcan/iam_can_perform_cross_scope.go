// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package iamcan

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/schemadecode"
)

// Cross-scope target outcome labels for
// eshu_dp_iam_can_perform_cross_scope_targets_total. The set is closed.
const (
	crossScopeTargetResolved   = "resolved"
	crossScopeTargetUnresolved = "unresolved"
	crossScopeTargetNotReady   = "not_ready"
	// crossScopeTargetScopeUnregistered is a not-ready target whose expected
	// scope (derived from the ARN) is not registered at all yet.
	crossScopeTargetScopeUnregistered = "scope_unregistered"
	crossScopeTargetAbandoned         = "abandoned"
	crossScopeTargetGlobLocalOnly     = "glob_local_only"
)

// CrossScopeTarget is one exact identity-policy target ARN the CAN_PERFORM
// handler asks sibling scopes for. ServiceKind is the ARN's service segment,
// which equals the awscloud collector service kind for every catalog target.
// Region is empty for S3, whose bucket ARNs carry no region, meaning any region
// of the account.
type CrossScopeTarget struct {
	ServiceKind string
	Region      string
	ARN         string
}

// CrossScopeTargetRequest bounds one cross-scope lookup: one AWS account, the
// scope to exclude (the permission's own scope, already loaded), and the exact
// target ARNs. It never names a glob, a wildcard, or another account.
type CrossScopeTargetRequest struct {
	AccountID      string
	ExcludeScopeID string
	Targets        []CrossScopeTarget
}

// CrossScopeTargetScope is the readiness state of one candidate target scope,
// sampled BEFORE its facts were loaded so the load is pinned to the generation
// the sample saw.
type CrossScopeTargetScope struct {
	// ScopeID is the aws:<account>:<region>:<service> collector scope.
	ScopeID string
	// ActiveGenerationID is the scope's active generation, or "" when none.
	ActiveGenerationID string
	// GenerationActive reports that ActiveGenerationID is set and in status
	// 'active'. Facts are only ever loaded from such a generation.
	GenerationActive bool
	// NodesCommitted reports that aws_resource_materialization published
	// canonical_nodes_committed on cloud_resource_uid for the active generation,
	// so a target node the edge writer MATCHes exists.
	NodesCommitted bool
	// GenerationPending reports that some generation of the scope is still in
	// status 'pending' (mid-ingestion).
	GenerationPending bool
}

// CrossScopeTargetSnapshot is a loader's answer: every candidate scope's state
// plus, per scope, the aws_resource facts from its pinned active generation
// whose arn was requested.
type CrossScopeTargetSnapshot struct {
	Scopes    []CrossScopeTargetScope
	Resources map[string][]facts.Envelope
}

// CrossScopeTargetLoader resolves exact CAN_PERFORM target ARNs from the other
// AWS service scopes of the same account (#6785). The awscloud collector emits
// IAM principals and permissions into the IAM scope and every catalog target
// into its own service scope, so without this port no production evaluation
// resolves a target. An implementation must bound its reads by the request
// (account, service/region, exact ARN) and must return an error, never an empty
// snapshot, when it cannot answer.
type CrossScopeTargetLoader interface {
	LoadCrossScopeTargets(ctx context.Context, request CrossScopeTargetRequest) (CrossScopeTargetSnapshot, error)
}

// IAMCanPerformTargetNotReadyFailureClass classifies a CAN_PERFORM intent
// deferred because an exact target ARN sits in a sibling scope whose
// CloudResource nodes have not committed, could still land in a sibling scope
// that has never activated but is mid-ingestion, or names a scope that is not
// registered at all yet (the iam scope was collected first). The handler
// returns it only AFTER committing the edges it could resolve, so the class
// governs when a late edge is added, never whether ready edges are written.
// Enrolled in nonCountingReducerRetryFailureClasses, and bounded by the
// readiness-wait ledger's first-defer anchor (crossscope.DecideWait).
const IAMCanPerformTargetNotReadyFailureClass = "iam_can_perform_target_not_ready"

// iamCanPerformTargetNotReadyError re-offers the intent, after its ready edges
// committed, until its missing cross-scope targets are ready or settle.
type iamCanPerformTargetNotReadyError struct {
	scopeID      string
	generationID string
	notReady     int
}

func (e iamCanPerformTargetNotReadyError) Error() string {
	return fmt.Sprintf(
		"%d cross-scope CAN_PERFORM target(s) not ready for scope %s generation %s; ready edges committed, waiting for the rest",
		e.notReady, e.scopeID, e.generationID,
	)
}

// Retryable reports that the queue should re-offer the intent.
func (iamCanPerformTargetNotReadyError) Retryable() bool { return true }

// FailureClass returns the non-counting readiness class.
func (iamCanPerformTargetNotReadyError) FailureClass() string {
	return IAMCanPerformTargetNotReadyFailureClass
}

// crossScopeTargetResult is the handler's decision over one snapshot.
type crossScopeTargetResult struct {
	// resources are the aws_resource facts usable as exact-ARN targets: only
	// requested ARNs, only from scopes whose nodes committed.
	resources []facts.Envelope
	outcomes  map[string]int
	// missing holds the ARN of every not-ready or scope-unregistered target.
	missing []string
}

// resolveCrossScopeTargets runs the cross-scope lookup for the loaded
// permission facts. It only classifies: the caller decides, through
// crossscope.DecideWait, whether to commit, defer, or settle, and records
// telemetry only on an evaluation that commits.
func (h IAMCanPerformMaterializationHandler) resolveCrossScopeTargets(
	ctx context.Context,
	intent reducercontract.Intent,
	permissionEnvelopes []facts.Envelope,
) (crossScopeTargetResult, error) {
	resolution := crossScopeTargetResult{outcomes: make(map[string]int)}
	if h.CrossScopeTargets == nil {
		return resolution, nil
	}
	requests, globs := iamCanPerformCrossScopeRequests(intent.ScopeID, permissionEnvelopes)
	resolution.outcomes[crossScopeTargetGlobLocalOnly] = globs
	for _, request := range requests {
		decided, err := h.loadAndDecideCrossScopeTargets(ctx, request)
		if err != nil {
			return crossScopeTargetResult{}, err
		}
		resolution.resources = append(resolution.resources, decided.resources...)
		resolution.missing = append(resolution.missing, decided.missing...)
		for outcome, count := range decided.outcomes {
			resolution.outcomes[outcome] += count
		}
	}
	return resolution, nil
}

// loadAndDecideCrossScopeTargets asks the loader for one bounded request and
// applies the per-ARN readiness table to its answer.
func (h IAMCanPerformMaterializationHandler) loadAndDecideCrossScopeTargets(
	ctx context.Context,
	request CrossScopeTargetRequest,
) (crossScopeTargetResult, error) {
	snapshot, err := h.CrossScopeTargets.LoadCrossScopeTargets(ctx, request)
	if err != nil {
		return crossScopeTargetResult{}, fmt.Errorf("load cross-scope iam can_perform targets: %w", err)
	}
	return decideCrossScopeTargets(request.Targets, snapshot), nil
}

// iamCanPerformCrossScopeRequests collects, per account, the exact Allow
// identity-policy resource ARNs that classify as a catalog target type. It also
// counts catalog-service glob patterns, which stay local-only. Undecodable
// permission facts are skipped here: the extractor quarantines them.
func iamCanPerformCrossScopeRequests(scopeID string, permissionEnvelopes []facts.Envelope) ([]CrossScopeTargetRequest, int) {
	byAccount := make(map[string]map[CrossScopeTarget]struct{})
	globs := 0
	for _, env := range permissionEnvelopes {
		if env.FactKind != facts.AWSIAMPermissionFactKind || env.IsTombstone {
			continue
		}
		permission, err := schemadecode.DecodeAWSIAMPermission(env)
		if err != nil || !strings.EqualFold(permission.Effect, "Allow") ||
			!iamCanPerformIdentityPolicySource(permission.PolicySource) {
			continue
		}
		for _, resource := range permission.Resources {
			target, glob, ok := crossScopeTargetForARN(resource, permission.AccountID)
			if glob {
				globs++
			}
			if !ok {
				continue
			}
			if byAccount[permission.AccountID] == nil {
				byAccount[permission.AccountID] = make(map[CrossScopeTarget]struct{})
			}
			byAccount[permission.AccountID][target] = struct{}{}
		}
	}
	accounts := make([]string, 0, len(byAccount))
	for account := range byAccount {
		accounts = append(accounts, account)
	}
	sort.Strings(accounts)
	requests := make([]CrossScopeTargetRequest, 0, len(accounts))
	for _, account := range accounts {
		targets := make([]CrossScopeTarget, 0, len(byAccount[account]))
		for target := range byAccount[account] {
			targets = append(targets, target)
		}
		sort.Slice(targets, func(a, b int) bool {
			left, right := targets[a], targets[b]
			if left.ServiceKind != right.ServiceKind {
				return left.ServiceKind < right.ServiceKind
			}
			if left.Region != right.Region {
				return left.Region < right.Region
			}
			return left.ARN < right.ARN
		})
		requests = append(requests, CrossScopeTargetRequest{AccountID: account, ExcludeScopeID: scopeID, Targets: targets})
	}
	return requests, globs
}

// crossScopeTargetForARN classifies one statement resource. It returns ok only
// for an exact catalog-type ARN in accountID (S3 bucket ARNs carry no account,
// and bucket names are partition-unique). glob reports a catalog-service glob,
// which is matched only locally.
func crossScopeTargetForARN(arn, accountID string) (CrossScopeTarget, bool, bool) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" {
		return CrossScopeTarget{}, false, false
	}
	service, region, account := parts[2], parts[3], parts[4]
	if strings.ContainsAny(arn, "*?") {
		_, catalogService := iamCanPerformCatalogServices[service]
		return CrossScopeTarget{}, catalogService, false
	}
	if iamCanPerformResourceTypeOfARN(arn) == "" {
		return CrossScopeTarget{}, false, false
	}
	if service == "s3" {
		return CrossScopeTarget{ServiceKind: service, ARN: arn}, false, true
	}
	if account != accountID || region == "" {
		return CrossScopeTarget{}, false, false
	}
	return CrossScopeTarget{ServiceKind: service, Region: region, ARN: arn}, false, true
}

// iamCanPerformCatalogServices is the set of ARN service segments the closed
// catalog targets; each equals its awscloud collector service kind.
var iamCanPerformCatalogServices = map[string]struct{}{
	"s3": {}, "kms": {}, "secretsmanager": {}, "ssm": {},
	"dynamodb": {}, "ec2": {}, "rds": {}, "lambda": {},
}

// decideCrossScopeTargets applies the per-ARN readiness table (design §3.3).
func decideCrossScopeTargets(targets []CrossScopeTarget, snapshot CrossScopeTargetSnapshot) crossScopeTargetResult {
	result := crossScopeTargetResult{outcomes: make(map[string]int)}
	committed := make(map[string]bool, len(snapshot.Scopes))
	for _, scope := range snapshot.Scopes {
		committed[scope.ScopeID] = scope.GenerationActive && scope.NodesCommitted
	}
	type located struct {
		scopeID string
		env     facts.Envelope
	}
	byARN := make(map[string][]located)
	scopeIDs := make([]string, 0, len(snapshot.Resources))
	for scopeID := range snapshot.Resources {
		scopeIDs = append(scopeIDs, scopeID)
	}
	sort.Strings(scopeIDs)
	for _, scopeID := range scopeIDs {
		for _, env := range snapshot.Resources[scopeID] {
			if env.FactKind != facts.AWSResourceFactKind || env.IsTombstone {
				continue
			}
			arn := payloadcore.AnyToString(env.Payload["arn"])
			byARN[arn] = append(byARN[arn], located{scopeID: scopeID, env: env})
		}
	}

	for _, target := range targets {
		found := byARN[target.ARN]
		usable := false
		for _, candidate := range found {
			if committed[candidate.scopeID] {
				result.resources = append(result.resources, candidate.env)
				usable = true
			}
		}
		switch {
		case usable:
			result.outcomes[crossScopeTargetResolved]++
		case len(found) > 0 || pendingCandidateScope(target, snapshot.Scopes):
			result.outcomes[crossScopeTargetNotReady]++
			result.missing = append(result.missing, target.ARN)
		case !registeredCandidateScope(target, snapshot.Scopes):
			// The scope the ARN names is not registered at all yet, which is
			// the adverse collection order: the iam scope ran first. Absence
			// is "not yet", never a verdict, until the bound expires.
			result.outcomes[crossScopeTargetScopeUnregistered]++
			result.missing = append(result.missing, target.ARN)
		default:
			result.outcomes[crossScopeTargetUnresolved]++
		}
	}
	return result
}

// pendingCandidateScope reports whether a scope that could hold target has
// never activated but is mid-ingestion. A newer pending generation beside an
// active one does not count: policies routinely name resources that do not
// exist, and rolling collection would otherwise hold every intent at the bound.
func pendingCandidateScope(target CrossScopeTarget, scopes []CrossScopeTargetScope) bool {
	for _, scope := range scopes {
		if scope.GenerationActive || !scope.GenerationPending {
			continue
		}
		if scopeCouldHoldTarget(scope.ScopeID, target) {
			return true
		}
	}
	return false
}

// registeredCandidateScope reports whether the scope the target ARN names is
// registered: aws:<account>:<region>:<service> for a regional ARN, and any
// region's s3 scope of the account for an S3 bucket ARN, which names no region.
// The loader already restricts candidates to the request's account.
func registeredCandidateScope(target CrossScopeTarget, scopes []CrossScopeTargetScope) bool {
	for _, scope := range scopes {
		if scopeCouldHoldTarget(scope.ScopeID, target) {
			return true
		}
	}
	return false
}

// scopeCouldHoldTarget matches an aws:<account>:<region>:<service> scope id
// against the target's service and, when the ARN names one, its region.
func scopeCouldHoldTarget(scopeID string, target CrossScopeTarget) bool {
	parts := strings.Split(scopeID, ":")
	if len(parts) != 4 || parts[3] != target.ServiceKind {
		return false
	}
	return target.Region == "" || parts[2] == target.Region
}
