package cicdrun

import (
	"encoding/json"
	"fmt"
	"net/url"
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

func sharedPayload(ctx FixtureContext, run githubRun) (map[string]any, error) {
	runID, err := providerID(run.ID)
	if err != nil {
		return nil, err
	}
	attempt, err := providerID(run.RunAttempt)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              string(ProviderGitHubActions),
		"run_id":                runID,
		"run_attempt":           defaultAttempt(attempt),
	}, nil
}

func providerID(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(typed), nil
	case json.Number:
		if strings.ContainsAny(typed.String(), ".eE") {
			return "", fmt.Errorf("provider id %q must be an integer or string", typed.String())
		}
		if _, err := strconv.ParseInt(typed.String(), 10, 64); err != nil {
			return "", fmt.Errorf("provider id %q must be an integer or string: %w", typed.String(), err)
		}
		return typed.String(), nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	default:
		return "", fmt.Errorf("provider id has unsupported shape %T", value)
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
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.User != nil || parsed.RawQuery != "" {
		return ""
	}
	return value
}

func redactSensitiveText(value string) string {
	fields := strings.Fields(value)
	for i, field := range fields {
		trimmed := strings.Trim(field, ".,;")
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			if stripSensitiveURL(trimmed) == "" {
				fields[i] = strings.Replace(field, trimmed, "[redacted_url]", 1)
			}
		}
	}
	return strings.Join(fields, " ")
}
