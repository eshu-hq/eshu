package reducer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	securityAlertMissingOwnedDependencyEvidence          = "owned dependency evidence missing"
	securityAlertMissingOwnedDependencyEvidenceAmbiguous = "owned dependency evidence ambiguous"
)

func extractSecurityAlertImpacts(envelopes []facts.Envelope) []securityAlertImpact {
	impacts := make([]securityAlertImpact, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != supplyChainImpactFactKind {
			continue
		}
		impacts = append(impacts, securityAlertImpact{
			factID:          envelope.FactID,
			repositoryID:    payloadStr(envelope.Payload, "repository_id"),
			packageID:       payloadStr(envelope.Payload, "package_id"),
			cveID:           payloadStr(envelope.Payload, "cve_id"),
			advisoryID:      payloadStr(envelope.Payload, "advisory_id"),
			status:          payloadStr(envelope.Payload, "impact_status"),
			observedVersion: payloadStr(envelope.Payload, "observed_version"),
			matchReason:     payloadStr(envelope.Payload, "match_reason"),
			missingEvidence: payloadStrings(envelope.Payload, "", "missing_evidence"),
		})
	}
	return impacts
}

func securityAlertConsumptionObservedVersion(
	alert providerSecurityAlert,
	consumption securityAlertConsumption,
) string {
	observedVersion := strings.TrimSpace(consumption.observedVersion)
	if observedVersion != "" {
		return observedVersion
	}
	if manifestVersion, ok := exactConsumptionDependencyVersion(alert.Ecosystem, supplyChainPackageConsumption{
		dependencyRange: consumption.dependencyRange,
		lockfile:        consumption.lockfile,
	}); ok {
		return manifestVersion
	}
	return ""
}

func securityAlertVersionMissingEvidence(observedVersion string, missing []string) []string {
	if strings.TrimSpace(observedVersion) != "" {
		return uniqueSortedStrings(missing)
	}
	if securityAlertMissingEvidenceContains(missing, supplyChainMissingMalformedInstalled) {
		return securityAlertMissingEvidenceWithout(missing, supplyChainMissingInstalledVersion)
	}
	for _, item := range missing {
		if strings.TrimSpace(item) == supplyChainMissingInstalledVersion {
			return uniqueSortedStrings(missing)
		}
	}
	return uniqueSortedStrings(append(missing, supplyChainMissingInstalledVersion))
}

func securityAlertMissingEvidenceContains(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func securityAlertMissingEvidenceWithout(values []string, drop string) []string {
	drop = strings.TrimSpace(drop)
	out := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == drop {
			continue
		}
		out = append(out, value)
	}
	return uniqueSortedStrings(out)
}
