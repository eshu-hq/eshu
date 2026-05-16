package cicdrun

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func newEnvelope(ctx FixtureContext, factKind, stableKey, sourceRecordID string, payload map[string]any) facts.Envelope {
	return facts.Envelope{
		FactID:           cicdFactID(factKind, stableKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableKey,
		SchemaVersion:    facts.CICDSchemaVersion,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableKey,
			SourceURI:      stripSensitiveURL(ctx.SourceURI),
			SourceRecordID: sourceRecordID,
		},
	}
}

func cicdFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("CICDRunFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func normalizedObservedAt(observedAt time.Time) time.Time {
	if observedAt.IsZero() {
		return time.Now().UTC()
	}
	return observedAt.UTC()
}

func validateContext(ctx FixtureContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("ci/cd fixture scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("ci/cd fixture generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("ci/cd fixture collector_instance_id must not be blank")
	}
	return nil
}

func sharedPayload(ctx FixtureContext, run githubRun) map[string]any {
	runID := providerID(run.ID)
	attempt := providerID(run.RunAttempt)
	return map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(ProviderGitHubActions),
		"run_id":                runID,
		"run_attempt":           defaultAttempt(attempt),
	}
}

func providerID(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
}

func defaultAttempt(attempt string) string {
	if strings.TrimSpace(attempt) == "" {
		return "1"
	}
	return strings.TrimSpace(attempt)
}

func trim(value string) string {
	return strings.TrimSpace(value)
}

func stripSensitiveURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "?") {
		return ""
	}
	return value
}
