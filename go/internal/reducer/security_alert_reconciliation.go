package reducer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// SecurityAlertReconciliationStatus names how one provider alert compares to
// Eshu-owned dependency and impact evidence.
type SecurityAlertReconciliationStatus string

const (
	// SecurityAlertReconciliationMatched means provider alert, owned
	// dependency evidence, and reducer-owned impact evidence agree.
	SecurityAlertReconciliationMatched SecurityAlertReconciliationStatus = "matched"
	// SecurityAlertReconciliationUnmatched means Eshu sees the dependency but
	// has not admitted matching impact evidence for the provider advisory IDs.
	SecurityAlertReconciliationUnmatched SecurityAlertReconciliationStatus = "unmatched"
	// SecurityAlertReconciliationStale means newer owned dependency evidence no
	// longer matches the provider alert's manifest path.
	SecurityAlertReconciliationStale SecurityAlertReconciliationStatus = "stale"
	// SecurityAlertReconciliationDismissed means the provider alert is
	// dismissed or auto-dismissed at the source.
	SecurityAlertReconciliationDismissed SecurityAlertReconciliationStatus = "dismissed"
	// SecurityAlertReconciliationFixed means the provider alert is fixed at the
	// source.
	SecurityAlertReconciliationFixed SecurityAlertReconciliationStatus = "fixed"
	// SecurityAlertReconciliationProviderOnly means the alert has no matching
	// owned dependency evidence in the active Eshu fact set.
	SecurityAlertReconciliationProviderOnly SecurityAlertReconciliationStatus = "provider_only"
)

// SecurityAlertReconciliationDecision is one reducer-owned comparison between
// a provider-reported repository alert and Eshu-owned evidence.
type SecurityAlertReconciliationDecision struct {
	ProviderAlertFactID  string
	Provider             string
	ProviderAlertID      string
	ProviderAlertNumber  int64
	ProviderState        string
	RepositoryID         string
	PackageID            string
	Ecosystem            string
	PackageName          string
	ManifestPath         string
	DependencyScope      string
	Relationship         string
	GHSAIDs              []string
	CVEIDs               []string
	VulnerableRange      string
	PatchedVersion       string
	Severity             string
	CVSS                 map[string]any
	EPSS                 map[string]string
	CWEs                 []map[string]string
	Summary              string
	SourceURL            string
	CreatedAt            string
	UpdatedAt            string
	FixedAt              string
	DismissedAt          string
	Status               SecurityAlertReconciliationStatus
	EshuImpactStatus     string
	EshuImpactFindingID  string
	Reason               string
	CanonicalWrites      int
	EvidenceFactIDs      []string
	DependencyEvidenceID string
	ImpactEvidenceID     string
}

type providerSecurityAlert struct {
	SecurityAlertReconciliationDecision
	updatedAtTime time.Time
}

type securityAlertConsumption struct {
	factID       string
	repositoryID string
	packageID    string
	relativePath string
	observedAt   time.Time
}

type securityAlertImpact struct {
	factID       string
	repositoryID string
	packageID    string
	cveID        string
	advisoryID   string
	status       string
}

// BuildSecurityAlertReconciliations compares provider-reported repository
// alerts to active Eshu dependency and impact facts without changing
// supply-chain impact admission.
func BuildSecurityAlertReconciliations(envelopes []facts.Envelope) []SecurityAlertReconciliationDecision {
	alerts := extractProviderSecurityAlerts(envelopes)
	consumptions := extractSecurityAlertConsumptions(envelopes)
	impacts := extractSecurityAlertImpacts(envelopes)

	decisions := make([]SecurityAlertReconciliationDecision, 0, len(alerts))
	for _, alert := range alerts {
		decisions = append(decisions, classifyProviderSecurityAlert(alert, consumptions, impacts))
	}
	sort.SliceStable(decisions, func(i, j int) bool {
		if decisions[i].RepositoryID != decisions[j].RepositoryID {
			return decisions[i].RepositoryID < decisions[j].RepositoryID
		}
		if decisions[i].Provider != decisions[j].Provider {
			return decisions[i].Provider < decisions[j].Provider
		}
		return decisions[i].ProviderAlertNumber < decisions[j].ProviderAlertNumber
	})
	return decisions
}

func extractProviderSecurityAlerts(envelopes []facts.Envelope) []providerSecurityAlert {
	alerts := make([]providerSecurityAlert, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.SecurityAlertRepositoryAlertFactKind {
			continue
		}
		updatedAt := payloadStr(envelope.Payload, "updated_at")
		alerts = append(alerts, providerSecurityAlert{
			SecurityAlertReconciliationDecision: SecurityAlertReconciliationDecision{
				ProviderAlertFactID: envelope.FactID,
				Provider:            payloadStr(envelope.Payload, "provider"),
				ProviderAlertID:     payloadStr(envelope.Payload, "provider_alert_id"),
				ProviderAlertNumber: securityAlertInt64(envelope.Payload, "provider_alert_number"),
				ProviderState:       strings.ToLower(payloadStr(envelope.Payload, "provider_state")),
				RepositoryID:        payloadStr(envelope.Payload, "repository_id"),
				PackageID:           payloadStr(envelope.Payload, "package_id"),
				Ecosystem:           payloadStr(envelope.Payload, "ecosystem"),
				PackageName:         payloadStr(envelope.Payload, "package_name"),
				ManifestPath:        payloadStr(envelope.Payload, "manifest_path"),
				DependencyScope:     payloadStr(envelope.Payload, "dependency_scope"),
				Relationship:        payloadStr(envelope.Payload, "relationship"),
				GHSAIDs:             payloadStrings(envelope.Payload, "ghsa_id", "ghsa_ids"),
				CVEIDs:              payloadStrings(envelope.Payload, "cve_id", "cve_ids"),
				VulnerableRange:     payloadStr(envelope.Payload, "vulnerable_range"),
				PatchedVersion:      payloadStr(envelope.Payload, "patched_version"),
				Severity:            payloadStr(envelope.Payload, "severity"),
				CVSS:                securityAlertMap(envelope.Payload, "cvss"),
				EPSS:                securityAlertStringMap(envelope.Payload, "epss"),
				CWEs:                securityAlertStringMapSlice(envelope.Payload, "cwes"),
				Summary:             payloadStr(envelope.Payload, "summary"),
				SourceURL:           payloadStr(envelope.Payload, "source_url"),
				CreatedAt:           payloadStr(envelope.Payload, "created_at"),
				UpdatedAt:           updatedAt,
				FixedAt:             payloadStr(envelope.Payload, "fixed_at"),
				DismissedAt:         payloadStr(envelope.Payload, "dismissed_at"),
				CanonicalWrites:     0,
				EvidenceFactIDs:     compactStringSlice(envelope.FactID),
			},
			updatedAtTime: parseSecurityAlertTime(updatedAt),
		})
	}
	return alerts
}

func extractSecurityAlertConsumptions(envelopes []facts.Envelope) []securityAlertConsumption {
	consumptions := make([]securityAlertConsumption, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != packageConsumptionCorrelationFactKind {
			continue
		}
		consumptions = append(consumptions, securityAlertConsumption{
			factID:       envelope.FactID,
			repositoryID: payloadStr(envelope.Payload, "repository_id"),
			packageID:    payloadStr(envelope.Payload, "package_id"),
			relativePath: payloadStr(envelope.Payload, "relative_path"),
			observedAt:   envelope.ObservedAt,
		})
	}
	return consumptions
}

func extractSecurityAlertImpacts(envelopes []facts.Envelope) []securityAlertImpact {
	impacts := make([]securityAlertImpact, 0)
	for _, envelope := range envelopes {
		if envelope.FactKind != supplyChainImpactFactKind {
			continue
		}
		impacts = append(impacts, securityAlertImpact{
			factID:       envelope.FactID,
			repositoryID: payloadStr(envelope.Payload, "repository_id"),
			packageID:    payloadStr(envelope.Payload, "package_id"),
			cveID:        payloadStr(envelope.Payload, "cve_id"),
			advisoryID:   payloadStr(envelope.Payload, "advisory_id"),
			status:       payloadStr(envelope.Payload, "impact_status"),
		})
	}
	return impacts
}

func classifyProviderSecurityAlert(
	alert providerSecurityAlert,
	consumptions []securityAlertConsumption,
	impacts []securityAlertImpact,
) SecurityAlertReconciliationDecision {
	decision := alert.SecurityAlertReconciliationDecision
	switch decision.ProviderState {
	case "dismissed", "auto_dismissed":
		decision.Status = SecurityAlertReconciliationDismissed
		decision.Reason = "provider alert is dismissed at the source"
		return decision
	case "fixed":
		decision.Status = SecurityAlertReconciliationFixed
		decision.Reason = "provider alert is fixed at the source"
		return decision
	}

	exactConsumption, staleConsumption := matchSecurityAlertConsumption(alert, consumptions)
	if exactConsumption.factID == "" {
		if staleConsumption.factID != "" {
			decision.Status = SecurityAlertReconciliationStale
			decision.DependencyEvidenceID = staleConsumption.factID
			decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, staleConsumption.factID))
			decision.Reason = "newer owned dependency evidence no longer matches the provider alert manifest path"
			return decision
		}
		decision.Status = SecurityAlertReconciliationProviderOnly
		decision.Reason = "provider alert has no matching owned dependency evidence"
		return decision
	}
	decision.DependencyEvidenceID = exactConsumption.factID
	decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, exactConsumption.factID))

	impact := matchSecurityAlertImpact(alert, impacts)
	if impact.factID == "" {
		decision.Status = SecurityAlertReconciliationUnmatched
		decision.Reason = "owned dependency exists but no reducer impact finding matches the provider advisory identifiers"
		return decision
	}
	decision.Status = SecurityAlertReconciliationMatched
	decision.EshuImpactStatus = impact.status
	decision.EshuImpactFindingID = impact.factID
	decision.ImpactEvidenceID = impact.factID
	decision.EvidenceFactIDs = uniqueSortedStrings(append(decision.EvidenceFactIDs, impact.factID))
	decision.Reason = "provider alert matches owned dependency and reducer impact evidence"
	return decision
}

func matchSecurityAlertConsumption(
	alert providerSecurityAlert,
	consumptions []securityAlertConsumption,
) (securityAlertConsumption, securityAlertConsumption) {
	var stale securityAlertConsumption
	for _, consumption := range consumptions {
		if consumption.repositoryID != alert.RepositoryID || consumption.packageID != alert.PackageID {
			continue
		}
		if alert.ManifestPath == "" || consumption.relativePath == alert.ManifestPath {
			return consumption, securityAlertConsumption{}
		}
		if !alert.updatedAtTime.IsZero() &&
			consumption.observedAt.After(alert.updatedAtTime) &&
			securityAlertConsumptionIsNewerStaleCandidate(consumption, stale) {
			stale = consumption
		}
	}
	return securityAlertConsumption{}, stale
}

func securityAlertConsumptionIsNewerStaleCandidate(
	candidate securityAlertConsumption,
	current securityAlertConsumption,
) bool {
	if current.factID == "" {
		return true
	}
	if candidate.observedAt.After(current.observedAt) {
		return true
	}
	if candidate.observedAt.Equal(current.observedAt) {
		return candidate.factID < current.factID
	}
	return false
}

func matchSecurityAlertImpact(alert providerSecurityAlert, impacts []securityAlertImpact) securityAlertImpact {
	for _, impact := range impacts {
		if impact.repositoryID != alert.RepositoryID || impact.packageID != alert.PackageID {
			continue
		}
		if securityAlertIDMatches(alert.CVEIDs, impact.cveID) ||
			securityAlertIDMatches(alert.GHSAIDs, impact.advisoryID) {
			return impact
		}
	}
	return securityAlertImpact{}
}

func securityAlertIDMatches(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func parseSecurityAlertTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func securityAlertInt64(payload map[string]any, key string) int64 {
	raw := strings.TrimSpace(fmt.Sprint(payload[key]))
	if raw == "" || raw == "<nil>" {
		return 0
	}
	value, _ := strconv.ParseInt(raw, 10, 64)
	return value
}

func securityAlertMap(payload map[string]any, key string) map[string]any {
	raw, ok := payload[key].(map[string]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make(map[string]any, len(raw))
	for key, value := range raw {
		if strings.TrimSpace(key) == "" || value == nil {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func securityAlertStringMap(payload map[string]any, key string) map[string]string {
	raw, ok := payload[key].(map[string]string)
	if ok {
		return cloneSecurityAlertStringMap(raw)
	}
	anyMap, ok := payload[key].(map[string]any)
	if !ok || len(anyMap) == 0 {
		return nil
	}
	out := make(map[string]string, len(anyMap))
	for key, value := range anyMap {
		text := strings.TrimSpace(fmt.Sprint(value))
		if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
			out[key] = text
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneSecurityAlertStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func securityAlertStringMapSlice(payload map[string]any, key string) []map[string]string {
	switch raw := payload[key].(type) {
	case []map[string]string:
		out := make([]map[string]string, 0, len(raw))
		for _, item := range raw {
			if cloned := cloneSecurityAlertStringMap(item); len(cloned) > 0 {
				out = append(out, cloned)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []any:
		out := make([]map[string]string, 0, len(raw))
		for _, item := range raw {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			converted := make(map[string]string, len(row))
			for key, value := range row {
				text := strings.TrimSpace(fmt.Sprint(value))
				if strings.TrimSpace(key) != "" && text != "" && text != "<nil>" {
					converted[key] = text
				}
			}
			if len(converted) > 0 {
				out = append(out, converted)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}
