// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

import (
	"context"
	"log/slog"

	"github.com/eshu-hq/eshu/go/internal/query/impacttrace"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// impact_seam.go is the exported seam of the impact handler family (#6060).
// A go/types pass over internal/query (tests included, zero package errors)
// found every unexported symbol the 46-file impact move set declares but
// files outside that set use. Each item below aliases or forwards to its
// unexported original with identical behavior, so the later move of the
// family into its own subpackage touches no caller: the aliases become
// cross-package references and the forwarders delegate to them. The
// ImpactHandler methods the same pass found are renamed at their
// declarations instead -- methods cannot be aliased -- with callers updated.
//
// Nothing here implements behavior. impact_seam_export_test.go trips any
// divergence or deletion.

// K8sResourceResult is the exported seam for k8sResourceResult, which the
// deployment-config-influence family reads from outside the impact move set.
// See #6060.
type K8sResourceResult = k8sResourceResult

// ProvisioningRepositoryCandidate is the exported seam for
// impacttrace.ProvisioningRepositoryCandidate, which the service family reads
// from outside the impact move set. See #6060.
type ProvisioningRepositoryCandidate = impacttrace.ProvisioningRepositoryCandidate

// DeploymentSourceResult is the exported seam for deploymentSourceResult,
// which the deployment-config-influence family reads from outside the impact
// move set. See #6060.
type DeploymentSourceResult = deploymentSourceResult

// PreChangeImpactRequest is the exported seam for preChangeImpactRequest,
// which the developer-change-plan family reads from outside the impact move
// set. See #6060.
type PreChangeImpactRequest = preChangeImpactRequest

const (
	// DeveloperChangePlanCapability is the exported seam for
	// developerChangePlanCapability, which the developer-change-plan family
	// reads from outside the impact move set. See #6060.
	DeveloperChangePlanCapability = developerChangePlanCapability

	// ImpactMaxListLimit is the exported seam for impactMaxListLimit, which
	// the compare family reads from outside the impact move set. See #6060.
	ImpactMaxListLimit = impactMaxListLimit

	// ContractImpactCapability is the exported seam for
	// contractImpactCapability, which root tests reference from outside
	// the impact move set (the capability row itself registers in this
	// package's capabilities.go). See #6060.
	ContractImpactCapability = contractImpactCapability
)

// ErrAmbiguousTraceWorkloadSelector is the exported seam for
// impacttrace.ErrAmbiguousTraceWorkloadSelector, which the deployment-config-influence
// family reads from outside the impact move set. See #6060.
var ErrAmbiguousTraceWorkloadSelector = impacttrace.ErrAmbiguousTraceWorkloadSelector

// BoundedK8sResourceResult is the exported seam for boundedK8sResourceResult,
// which the deployment-config-influence family calls from outside the impact
// move set. It forwards so the impact family can move without touching
// callers. See #6060.
func BoundedK8sResourceResult(
	contentRows []map[string]any,
	contentLowerBound bool,
	deploymentSourceRows []map[string]any,
	deploymentSourceLowerBound bool,
	selectCandidatePoolTruncated bool,
) K8sResourceResult {
	return boundedK8sResourceResult(contentRows, contentLowerBound, deploymentSourceRows, deploymentSourceLowerBound, selectCandidatePoolTruncated)
}

// DistinctSortedInstanceField is the exported seam for
// querycontract.DistinctSortedInstanceField, which the service family calls
// from outside the impact move set. It forwards so the impact family can move
// without touching callers. See #6060.
func DistinctSortedInstanceField(instances []map[string]any, key string) []string {
	return querycontract.DistinctSortedInstanceField(instances, key)
}

// CanonicalWorkloadIDCandidate is the exported seam for
// canonicalWorkloadIDCandidate, which the entity family calls from outside
// the impact move set. It forwards so the impact family can move without
// touching callers. See #6060.
func CanonicalWorkloadIDCandidate(target string) string {
	return canonicalWorkloadIDCandidate(target)
}

// NormalizePreChangeImpactRequest is the exported seam for
// normalizePreChangeImpactRequest, which the developer-change-plan family
// calls from outside the impact move set. It forwards so the impact family
// can move without touching callers. See #6060.
func NormalizePreChangeImpactRequest(req PreChangeImpactRequest) (PreChangeImpactRequest, error) {
	return normalizePreChangeImpactRequest(req)
}

// PreChangeGraphTarget is the exported seam for preChangeGraphTarget, which
// the developer-change-plan family calls from outside the impact move set.
// It forwards so the impact family can move without touching callers. See
// #6060.
func PreChangeGraphTarget(req PreChangeImpactRequest) string {
	return preChangeGraphTarget(req)
}

// PreChangeSummary is the exported seam for preChangeSummary, which the
// developer-change-plan family calls from outside the impact move set. It
// forwards so the impact family can move without touching callers. See #6060.
func PreChangeSummary(data map[string]any) string {
	return preChangeSummary(data)
}

// ImpactRepoIDAllowed is the exported seam for impactRepoIDAllowed, which the
// compare and contract families call from outside the impact move set. The
// querycontract.RepositoryAccessFilter parameter names the already-exported querycontract
// type (root's repositoryAccessFilter is an alias for it). See #6060.
func ImpactRepoIDAllowed(repoID string, access querycontract.RepositoryAccessFilter) bool {
	return impacttrace.ImpactRepoIDAllowed(repoID, access)
}

// FilterRowsByRepoIDForAccess is the exported seam for
// filterRowsByRepoIDForAccess, which the deployment-config-influence family
// calls from outside the impact move set. It forwards so the impact family
// can move without touching callers. See #6060.
func FilterRowsByRepoIDForAccess(rows []map[string]any, access querycontract.RepositoryAccessFilter) []map[string]any {
	return impacttrace.FilterRowsByRepoIDForAccess(rows, access)
}

// FilterProvisioningRepositoryCandidatesForAccess is the exported seam for
// filterProvisioningRepositoryCandidatesForAccess, which the service family
// calls from outside the impact move set. It forwards so the impact family
// can move without touching callers. See #6060.
func FilterProvisioningRepositoryCandidatesForAccess(
	candidates []ProvisioningRepositoryCandidate,
	access querycontract.RepositoryAccessFilter,
) []ProvisioningRepositoryCandidate {
	return impacttrace.FilterProvisioningRepositoryCandidatesForAccess(candidates, access)
}

// NormalizeImpactListLimit is the exported seam for normalizeImpactListLimit,
// which the compare family calls from outside the impact move set. It
// forwards so the impact family can move without touching callers. See #6060.
func NormalizeImpactListLimit(limit int) int {
	return normalizeImpactListLimit(limit)
}

// TrimImpactRows is the exported seam for trimImpactRows, which the compare
// family calls from outside the impact move set. It forwards so the impact
// family can move without touching callers. See #6060.
func TrimImpactRows(rows []map[string]any, limit int) ([]map[string]any, bool) {
	return trimImpactRows(rows, limit)
}

// UniqueStrings is the exported seam for uniqueStrings, which the content
// family calls from outside the impact move set. It forwards so the impact
// family can move without touching callers. See #6060.
func UniqueStrings(values []string) []string {
	return uniqueStrings(values)
}

// PreChangeImpactErrorStatus is the exported seam for
// preChangeImpactErrorStatus, which the developer-change-plan family calls
// from outside the impact move set. It forwards so the impact family can move
// without touching callers. See #6060.
func PreChangeImpactErrorStatus(err error) int {
	return preChangeImpactErrorStatus(err)
}

// AppendUniqueString is the exported seam for
// querycontract.AppendUniqueString, which the query root's seam tripwire
// calls from outside the impact move set. It forwards so the impact family
// can move without touching callers. See #6060.
func AppendUniqueString(values *[]string, candidate string) {
	querycontract.AppendUniqueString(values, candidate)
}

// ContainsString is the exported seam for querycontract.ContainsString,
// which moved impact tests call from outside the query root. It forwards so
// the impact family can move without touching callers. See #6060.
func ContainsString(values []string, candidate string) bool {
	return querycontract.ContainsString(values, candidate)
}

// TraceDeploymentChainRequest is the exported seam for
// traceDeploymentChainRequest, which adapter tests build from outside the
// impact move set. See #6060.
type TraceDeploymentChainRequest = traceDeploymentChainRequest

// TraceEnrichmentOptions maps a trace request to its enrichment options,
// which adapter tests build from outside the impact move set. It forwards so
// the impact family can move without touching callers. See #6060.
func TraceEnrichmentOptions(req TraceDeploymentChainRequest) TraceEnrichmentConfig {
	return traceEnrichmentOptions(req)
}

// FetchServiceTraceContext is the exported seam for the deployment-trace
// context enricher, which the query root's seam tripwire calls from outside
// the impact move set. The enricher builds a B5-entity handler in root, so
// this seam delegates to the wired backend (production adapter in root
// binaries and tests via package query's init, fakes in this package's
// tests). See #6060.
func FetchServiceTraceContext(
	ctx context.Context,
	graph querycontract.GraphQuery,
	content querycontract.ContentStore,
	logger *slog.Logger,
	serviceName string,
	traceOptions TraceEnrichmentConfig,
) (map[string]any, error) {
	return DefaultTraceContext.FetchServiceTraceContext(ctx, graph, content, logger, serviceName, traceOptions)
}

// JoinOrNone is the exported seam for impacttrace.JoinOrNone, which the repository and
// service families call from outside the impact move set. It forwards so the
// impact family can move without touching callers. See #6060.
func JoinOrNone(values []string) string {
	return impacttrace.JoinOrNone(values)
}

// ResolvedProfile is the exported seam for (*ImpactHandler).profile, which
// families outside the impact move set call on ImpactHandler (contract,
// deployment-config-influence, developer-change-plan, entity,
// exposure-path). It is named for what it returns -- the handler's
// configured Profile field, nil-safe and normalized -- because Profile
// itself is taken by that field. It forwards so the impact family can move
// without touching callers or the grandfathered bodies that call profile.
// See #6060.
func (h *ImpactHandler) ResolvedProfile() querycontract.QueryProfile {
	return h.profile()
}

// NewTraceEnrichmentConfig is the exported constructor for
// TraceEnrichmentConfig. The deployment-config-influence family is the only
// caller outside the impact move set and it only sets maxDepth (to 4), so
// the remaining fields stay unexported at their zero values rather than
// exporting every field. See #6060.
func NewTraceEnrichmentConfig(maxDepth int) TraceEnrichmentConfig {
	return TraceEnrichmentConfig{MaxDepth: maxDepth}
}

// Rows is the exported read accessor for deploymentSourceResult.rows, which
// the deployment-config-influence family reads from outside the impact move
// set. See #6060.
func (r DeploymentSourceResult) Rows() []map[string]any {
	return r.rows
}

// Limits is the exported read accessor for deploymentSourceResult.limits,
// which the deployment-config-influence family reads from outside the impact
// move set. See #6060.
func (r DeploymentSourceResult) Limits() map[string]any {
	return r.limits
}

// SetRows is the exported write accessor for deploymentSourceResult.rows,
// which the deployment-config-influence family rebinds to the caller's grant
// (filterRowsByRepoIDForAccess) from outside the impact move set. See #6060.
func (r *DeploymentSourceResult) SetRows(rows []map[string]any) {
	r.rows = rows
}

// Rows is the exported read accessor for k8sResourceResult.rows, which the
// deployment-config-influence family reads from outside the impact move set.
// See #6060.
func (r K8sResourceResult) Rows() []map[string]any {
	return r.rows
}

// Limits is the exported read accessor for k8sResourceResult.limits, which
// the deployment-config-influence family reads from outside the impact move
// set. See #6060.
func (r K8sResourceResult) Limits() map[string]any {
	return r.limits
}

// ImageRefs is the exported read accessor for k8sResourceResult.imageRefs,
// which the deployment-config-influence family reads from outside the impact
// move set. See #6060.
func (r K8sResourceResult) ImageRefs() []string {
	return r.imageRefs
}

// Candidates is the exported read accessor for
// k8sResourceResult.candidates, which the deployment-config-influence family
// threads back through boundedK8sResourceResult from outside the impact move
// set. See #6060.
func (r K8sResourceResult) Candidates() []map[string]any {
	return r.candidates
}

// ContentLowerBound is the exported read accessor for
// k8sResourceResult.contentLowerBound, which the
// deployment-config-influence family threads back through
// boundedK8sResourceResult from outside the impact move set. See #6060.
func (r K8sResourceResult) ContentLowerBound() bool {
	return r.contentLowerBound
}

// SelectCandidatePoolTruncated is the exported read accessor for
// k8sResourceResult.selectCandidatePoolTruncated, which the
// deployment-config-influence family threads back through
// boundedK8sResourceResult from outside the impact move set. See #6060.
func (r K8sResourceResult) SelectCandidatePoolTruncated() bool {
	return r.selectCandidatePoolTruncated
}
