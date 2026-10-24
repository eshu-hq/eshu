// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"regexp"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// ExtensionDefinedFailureToken is the one bounded class a collector kind that
// is not first-party reports for a failure class it defines itself. The
// extension host admits any class matching ^[a-z0-9][a-z0-9._-]{0,95}$, so the
// set cannot be enumerated in tree; every such class collapses to this token
// rather than growing the status enum or the metric label.
const ExtensionDefinedFailureToken = "extension_defined"

// extensionFailureClassPattern is the shape the extension host enforces on a
// class an extension reports (extensionhost.failureClassPattern). A class that
// does not match it is free text and reads UnknownFailureToken.
var extensionFailureClassPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,95}$`)

// commonFailureClasses is every failure class the claim runner persists on a
// terminal work item whatever the collector kind. It holds the five provider
// classes the shared SDK writes and the four the claim runner writes itself.
// The literals are pinned here, not imported, so the status leaf stays free of
// collector runtime packages. Sources:
//
//   - auth_denied, not_found, rate_limited, terminal, retryable: the SDK
//     provider classes (go/internal/collector/sdk/failure.go, also mapped by
//     collector/securityalerts/alertruntime/failure.go). failTerminal prefers
//     the class from the error chain (classifiedFailureClass in
//     collector/claimed_service_validation.go), and budgetExhaustedError wraps
//     the cause, so a budget exhausted on a transient provider failure
//     persists retryable on a terminal row.
//   - attempt_budget_exhausted: the budget class when the cause carries no
//     class (FailureClassAttemptBudgetExhausted, collector/claimed_service.go).
//   - identity_mismatch: a claim whose collected identity does not match (the
//     FailClaimTerminal calls in collector/claimed_service.go).
//   - collect_failure, commit_failure: the claim-stage fallbacks for a
//     non-retryable collect or commit error (collector/claimed_service.go).
//
// Keep the nine aligned with securityAlertTerminalFailureClasses in the
// security-alert read (#8803), which pins the same set for that one kind.
var commonFailureClasses = []string{
	"auth_denied", "not_found", "rate_limited", "terminal", "retryable",
	"attempt_budget_exhausted", "identity_mismatch", "collect_failure", "commit_failure",
}

// registryFailureClasses are the bounded classes collector.RegistryFailure
// carries (collector/failure.go). The OCI registry, package registry, and SBOM
// attestation collectors all build their errors from it. The rate-limited,
// retryable, and canceled classes reach a terminal row when the attempt
// budget runs out on the cause or the claim is canceled.
var registryFailureClasses = []string{
	"registry_auth_denied", "registry_not_found", "registry_rate_limited",
	"registry_retryable_failure", "registry_canceled", "registry_terminal_failure",
}

// kindFailureClasses holds, per first-party collector kind, the classes that
// kind's own writer persists in addition to commonFailureClasses. A class
// outside a kind's own set, the common set, and extensionHostFailureClasses
// reads UnknownFailureToken for that kind, even when another kind owns it, so
// one kind's vocabulary never vouches for another's. A first-party kind absent
// from this map persists only the common and extension host classes. Sources,
// by kind:
//
//   - jira: permission_hidden, deleted, archived, from jiraStatusPolicy
//     (collector/jira/source.go) over sdk.StatusPolicy for 401 and 403, 404,
//     and 410.
//   - aws: permission_denied, unsupported_permission, stale_fence
//     (collector/awscloud/awsruntime/scan_status.go).
//   - oci_registry, sbom_attestation: registryFailureClasses.
//   - package_registry: registryFailureClasses plus
//     registry_metadata_too_large (collector/registry/package/runtime/
//     http_provider.go).
//   - vulnerability_intelligence: non_retryable, the terminal sourceFailure
//     class (collector/vulnerabilityintelligence/vulnruntime/source_state.go
//     over vulnsource.ErrorClassNonRetryable). Its rate_limited and retryable
//     classes are common.
//   - scanner_worker: the scannerworker.FailureClass vocabulary
//     (collector/scannerworker/failure.go). The worker dead-letters a claim
//     with FailClaimTerminal and the analyzer's class.
//   - terraform_state: waiting_on_git_generation, the class of
//     terraformstate.WaitingOnGitGenerationError, which reaches a terminal
//     row when the attempt budget runs out on the wait.
var kindFailureClasses = map[string][]string{
	string(scope.CollectorJira):            {"permission_hidden", "deleted", "archived"},
	string(scope.CollectorAWS):             {"permission_denied", "unsupported_permission", "stale_fence"},
	string(scope.CollectorOCIRegistry):     registryFailureClasses,
	string(scope.CollectorSBOMAttestation): registryFailureClasses,
	string(scope.CollectorPackageRegistry): append(
		slices.Clone(registryFailureClasses), "registry_metadata_too_large"),
	string(scope.CollectorVulnerabilityIntelligence): {"non_retryable"},
	string(scope.CollectorScannerWorker): {
		"analyzer_failed", "cpu_limit_exceeded", "memory_limit_exceeded", "timeout",
		"input_limit_exceeded", "file_limit_exceeded", "fact_limit_exceeded",
		"target_unavailable", "unsupported_target", "source_unavailable", "commit_failed",
	},
	string(scope.CollectorTerraformState): {"waiting_on_git_generation"},
}

// extensionHostFailureClasses are the classes the extension host itself writes
// for a collector kind it hosts (collector/extensionhost/failure.go).
// identity_mismatch is also written there and is already common. Every kind
// admits them, first-party kinds included, because the component-extension
// runtime takes its collector kind from the manifest
// (cmd/collector-component-extension collectorKindForManifest): a
// component-configured instance can carry a first-party kind such as jira, and
// the host then writes these classes for it.
var extensionHostFailureClasses = []string{
	"invalid_claim", "invalid_result", "launch_failure", "status_record",
}

// firstPartyKinds is every collector kind the platform ships, derived from the
// scope package so a new first-party kind is never mistaken for an extension.
var firstPartyKinds = func() map[string]struct{} {
	out := make(map[string]struct{})
	for _, kind := range scope.AllCollectorKinds() {
		out[string(kind)] = struct{}{}
	}
	return out
}()

// FailureClassesForKind returns the sorted failure classes a status surface
// can report for collectorKind, excluding UnknownFailureToken. Every kind gets
// the common classes and the extension host's own classes. A first-party kind
// adds its own classes. A kind that is not first-party is hosted by the
// extension host and adds ExtensionDefinedFailureToken.
func FailureClassesForKind(collectorKind string) []string {
	kind := strings.TrimSpace(collectorKind)
	out := slices.Clone(commonFailureClasses)
	out = append(out, extensionHostFailureClasses...)
	if _, ok := firstPartyKinds[kind]; ok {
		out = append(out, kindFailureClasses[kind]...)
	} else {
		out = append(out, ExtensionDefinedFailureToken)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// BoundedFailureClasses returns every class any collector kind can report,
// sorted and without duplicates. It excludes UnknownFailureToken, which a
// status surface reports for any class outside a kind's set. The OpenAPI enum
// tests, the docs tie test, and the metric label cardinality pin compare to it.
func BoundedFailureClasses() []string {
	out := slices.Clone(commonFailureClasses)
	for _, classes := range kindFailureClasses {
		out = append(out, classes...)
	}
	out = append(out, extensionHostFailureClasses...)
	out = append(out, ExtensionDefinedFailureToken)
	slices.Sort(out)
	return slices.Compact(out)
}

// BoundedFailureClassForKind returns class when collectorKind may report it
// and UnknownFailureToken otherwise. Every kind reports the common classes and
// the extension host classes as they are, and a first-party kind adds its own.
// A kind that is not first-party also collapses any other class matching the
// extension host's class pattern to ExtensionDefinedFailureToken. Collapsing is
// not applied to a first-party kind: the status leaf has no signal that a
// first-party kind is extension-hosted, and relabeling would hide an in-tree
// class that the writer ties should catch. So an extension-defined class on a
// component-configured first-party instance reads UnknownFailureToken. Free
// text, an empty class, and a class only another kind owns read
// UnknownFailureToken, so no persisted text reaches a status payload or a
// metric label.
func BoundedFailureClassForKind(collectorKind string, class string) string {
	kind := strings.TrimSpace(collectorKind)
	class = strings.TrimSpace(class)
	if slices.Contains(commonFailureClasses, class) || slices.Contains(extensionHostFailureClasses, class) {
		return class
	}
	if _, ok := firstPartyKinds[kind]; ok {
		if slices.Contains(kindFailureClasses[kind], class) {
			return class
		}
		return UnknownFailureToken
	}
	if extensionFailureClassPattern.MatchString(class) {
		return ExtensionDefinedFailureToken
	}
	return UnknownFailureToken
}
