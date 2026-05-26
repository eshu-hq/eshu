package reducer

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const packageSecurityAlertReconciliationMaxAttempts = 3

// SecurityAlertReconciliationFactFilter bounds active evidence loading for one
// provider-alert reconciliation intent.
type SecurityAlertReconciliationFactFilter struct {
	RepositoryIDs []string
	PackageIDs    []string
	CVEIDs        []string
	GHSAIDs       []string
}

type activeSecurityAlertReconciliationFactLoader interface {
	ListActiveSecurityAlertReconciliationFacts(
		context.Context,
		SecurityAlertReconciliationFactFilter,
	) ([]facts.Envelope, error)
}

// SecurityAlertReconciliationHandler compares provider alerts with owned Eshu
// evidence without publishing supply-chain impact truth.
type SecurityAlertReconciliationHandler struct {
	FactLoader  FactLoader
	Writer      SecurityAlertReconciliationWriter
	Instruments *telemetry.Instruments
}

// Handle executes one provider alert reconciliation reducer intent.
func (h SecurityAlertReconciliationHandler) Handle(ctx context.Context, intent Intent) (Result, error) {
	if intent.Domain != DomainSecurityAlertReconciliation {
		return Result{}, fmt.Errorf(
			"security_alert_reconciliation handler does not accept domain %q",
			intent.Domain,
		)
	}
	if h.FactLoader == nil {
		return Result{}, fmt.Errorf("security alert reconciliation fact loader is required")
	}
	if h.Writer == nil {
		return Result{}, fmt.Errorf("security alert reconciliation writer is required")
	}

	envelopes, err := loadFactsForKinds(
		ctx,
		h.FactLoader,
		intent.ScopeID,
		intent.GenerationID,
		securityAlertReconciliationTriggerKinds(),
	)
	if err != nil {
		return Result{}, fmt.Errorf("load security alert facts: %w", err)
	}
	active, err := h.loadActiveEvidence(ctx, securityAlertReconciliationFilter(envelopes))
	if err != nil {
		return Result{}, fmt.Errorf("load active security alert reconciliation evidence: %w", err)
	}
	envelopes = append(envelopes, active...)
	envelopes = dedupeSecurityAlertReconciliationEnvelopes(envelopes)

	decisions := BuildSecurityAlertReconciliations(envelopes)
	if shouldDeferPackageSecurityAlertReconciliation(intent, decisions) {
		return Result{}, retryableSecurityAlertReconciliationEvidenceError{
			packageID: firstUnmatchedPackageWithDependency(decisions),
		}
	}
	writeResult, err := h.Writer.WriteSecurityAlertReconciliations(ctx, SecurityAlertReconciliationWrite{
		IntentID:     intent.IntentID,
		ScopeID:      intent.ScopeID,
		GenerationID: intent.GenerationID,
		SourceSystem: intent.SourceSystem,
		Cause:        intent.Cause,
		Decisions:    decisions,
	})
	if err != nil {
		return Result{}, fmt.Errorf("write security alert reconciliations: %w", err)
	}

	return Result{
		IntentID:        intent.IntentID,
		Domain:          DomainSecurityAlertReconciliation,
		Status:          ResultStatusSucceeded,
		EvidenceSummary: securityAlertReconciliationSummary(decisions, writeResult.CanonicalWrites),
		CanonicalWrites: writeResult.CanonicalWrites,
	}, nil
}

func securityAlertReconciliationTriggerKinds() []string {
	return []string{
		facts.SecurityAlertRepositoryAlertFactKind,
		facts.PackageRegistryPackageFactKind,
	}
}

func (h SecurityAlertReconciliationHandler) loadActiveEvidence(
	ctx context.Context,
	filter SecurityAlertReconciliationFactFilter,
) ([]facts.Envelope, error) {
	loader, ok := h.FactLoader.(activeSecurityAlertReconciliationFactLoader)
	if !ok || filter.empty() {
		return nil, nil
	}
	envelopes, err := loader.ListActiveSecurityAlertReconciliationFacts(ctx, filter)
	if err != nil {
		return nil, classifyFactLoadError(err)
	}
	return envelopes, nil
}

func securityAlertReconciliationFilter(envelopes []facts.Envelope) SecurityAlertReconciliationFactFilter {
	var repositoryIDs, packageIDs, cveIDs, ghsaIDs []string
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.SecurityAlertRepositoryAlertFactKind:
			repositoryIDs = append(repositoryIDs, payloadStr(envelope.Payload, "repository_id"))
			packageIDs = append(packageIDs, payloadStr(envelope.Payload, "package_id"))
			cveIDs = append(cveIDs, payloadStrings(envelope.Payload, "cve_id", "cve_ids")...)
			ghsaIDs = append(ghsaIDs, payloadStrings(envelope.Payload, "ghsa_id", "ghsa_ids")...)
		case facts.PackageRegistryPackageFactKind:
			packageIDs = append(packageIDs, firstNonBlank(
				payloadStr(envelope.Payload, "package_id"),
				envelope.ScopeID,
			))
		}
	}
	return SecurityAlertReconciliationFactFilter{
		RepositoryIDs: uniqueSortedStrings(repositoryIDs),
		PackageIDs:    uniqueSortedStrings(packageIDs),
		CVEIDs:        uniqueSortedStrings(cveIDs),
		GHSAIDs:       uniqueSortedStrings(ghsaIDs),
	}
}

func dedupeSecurityAlertReconciliationEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	if len(envelopes) < 2 {
		return envelopes
	}
	seen := make(map[string]struct{}, len(envelopes))
	out := make([]facts.Envelope, 0, len(envelopes))
	for _, envelope := range envelopes {
		key := strings.TrimSpace(envelope.FactID)
		if key == "" {
			key = strings.Join([]string{
				envelope.ScopeID,
				envelope.GenerationID,
				envelope.FactKind,
			}, "\x00")
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, envelope)
	}
	return out
}

func shouldDeferPackageSecurityAlertReconciliation(
	intent Intent,
	decisions []SecurityAlertReconciliationDecision,
) bool {
	if intent.AttemptCount >= packageSecurityAlertReconciliationMaxAttempts ||
		!packageTriggeredSecurityAlertReconciliation(intent) {
		return false
	}
	for _, decision := range decisions {
		if decision.Status == SecurityAlertReconciliationUnmatched &&
			strings.TrimSpace(decision.DependencyEvidenceID) != "" &&
			strings.TrimSpace(decision.ImpactEvidenceID) == "" {
			return true
		}
	}
	return false
}

func packageTriggeredSecurityAlertReconciliation(intent Intent) bool {
	return strings.EqualFold(strings.TrimSpace(intent.SourceSystem), "package_registry") ||
		strings.EqualFold(strings.TrimSpace(intent.Cause), "package registry identity observed")
}

func firstUnmatchedPackageWithDependency(decisions []SecurityAlertReconciliationDecision) string {
	for _, decision := range decisions {
		if decision.Status == SecurityAlertReconciliationUnmatched &&
			strings.TrimSpace(decision.DependencyEvidenceID) != "" {
			return strings.TrimSpace(decision.PackageID)
		}
	}
	return ""
}

type retryableSecurityAlertReconciliationEvidenceError struct {
	packageID string
}

func (e retryableSecurityAlertReconciliationEvidenceError) Error() string {
	if strings.TrimSpace(e.packageID) == "" {
		return "security alert reconciliation waiting for package impact evidence"
	}
	return fmt.Sprintf("security alert reconciliation waiting for package impact evidence: %s", e.packageID)
}

func (retryableSecurityAlertReconciliationEvidenceError) Retryable() bool {
	return true
}

func (retryableSecurityAlertReconciliationEvidenceError) FailureClass() string {
	return "security_alert_reconciliation_waiting_for_impact"
}

func (f SecurityAlertReconciliationFactFilter) empty() bool {
	return len(f.RepositoryIDs) == 0 && len(f.PackageIDs) == 0 &&
		len(f.CVEIDs) == 0 && len(f.GHSAIDs) == 0
}

func securityAlertReconciliationSummary(
	decisions []SecurityAlertReconciliationDecision,
	canonicalWrites int,
) string {
	counts := make(map[SecurityAlertReconciliationStatus]int, 6)
	for _, decision := range decisions {
		counts[decision.Status]++
	}
	return fmt.Sprintf(
		"security alert reconciliations evaluated=%d matched=%d unmatched=%d stale=%d dismissed=%d fixed=%d provider_only=%d canonical_writes=%d",
		len(decisions),
		counts[SecurityAlertReconciliationMatched],
		counts[SecurityAlertReconciliationUnmatched],
		counts[SecurityAlertReconciliationStale],
		counts[SecurityAlertReconciliationDismissed],
		counts[SecurityAlertReconciliationFixed],
		counts[SecurityAlertReconciliationProviderOnly],
		canonicalWrites,
	)
}
