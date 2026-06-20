package semanticpolicy

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
)

func normalizeRule(rule Rule, defaults Settings, denied []string) (Rule, error) {
	out := Rule{
		RuleID:            strings.TrimSpace(rule.RuleID),
		ProviderProfileID: strings.TrimSpace(rule.ProviderProfileID),
		Settings:          mergeSettings(defaults, rule.Settings),
	}
	if out.RuleID == "" {
		return Rule{}, fmt.Errorf("rule_id is required")
	}
	if out.ProviderProfileID == "" {
		return Rule{}, fmt.Errorf("provider_profile_id is required")
	}
	sourceClasses, err := normalizeSourceClasses(rule.SourceClasses)
	if err != nil {
		return Rule{}, fmt.Errorf("source_classes: %w", err)
	}
	if len(sourceClasses) == 0 {
		return Rule{}, fmt.Errorf("source_classes must include at least one source class")
	}
	for _, sourceClass := range sourceClasses {
		if slices.Contains(denied, sourceClass) {
			return Rule{}, fmt.Errorf("source_classes contains denied class %q", sourceClass)
		}
	}
	out.SourceClasses = sourceClasses

	scopes, err := normalizeScopes(rule.Scopes)
	if err != nil {
		return Rule{}, err
	}
	out.Scopes = scopes
	selectors, err := normalizeSourceSelectors(rule.SourceAllowlist)
	if err != nil {
		return Rule{}, err
	}
	out.SourceAllowlist = selectors
	if err := validateSettings(out.Settings); err != nil {
		return Rule{}, err
	}
	return out, nil
}

func normalizeScopes(scopes []Scope) ([]Scope, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("scopes must include at least one scope")
	}
	out := make([]Scope, 0, len(scopes))
	seen := make(map[Scope]struct{}, len(scopes))
	for _, scope := range scopes {
		row := Scope{Kind: strings.TrimSpace(scope.Kind), ID: strings.TrimSpace(scope.ID)}
		if !isSupportedScopeKind(row.Kind) {
			return nil, fmt.Errorf("scope kind %q is unsupported", row.Kind)
		}
		if row.ID == "" {
			return nil, fmt.Errorf("scope id is required")
		}
		if _, ok := seen[row]; ok {
			continue
		}
		seen[row] = struct{}{}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].ID < out[j].ID
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

func normalizeSourceSelectors(selectors []SourceSelector) ([]SourceSelector, error) {
	if len(selectors) == 0 {
		return nil, fmt.Errorf("source_allowlist must include at least one selector")
	}
	out := make([]SourceSelector, 0, len(selectors))
	seen := make(map[SourceSelector]struct{}, len(selectors))
	for _, selector := range selectors {
		row := SourceSelector{Kind: strings.TrimSpace(selector.Kind), Value: strings.TrimSpace(selector.Value)}
		if !isSupportedSourceSelector(row.Kind) {
			return nil, fmt.Errorf("source_allowlist kind %q is unsupported", row.Kind)
		}
		if row.Kind == SourceSelectorAll {
			row.Value = "*"
		}
		if row.Value == "" {
			return nil, fmt.Errorf("source_allowlist value is required")
		}
		if _, ok := seen[row]; ok {
			continue
		}
		seen[row] = struct{}{}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind == out[j].Kind {
			return out[i].Value < out[j].Value
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

func normalizeSourceClasses(sourceClasses []string) ([]string, error) {
	seen := make(map[string]struct{}, len(sourceClasses))
	out := make([]string, 0, len(sourceClasses))
	for _, sourceClass := range sourceClasses {
		sourceClass = strings.TrimSpace(sourceClass)
		if sourceClass == "" {
			continue
		}
		if !isSupportedSourceClass(sourceClass) {
			return nil, fmt.Errorf("source class %q is unsupported", sourceClass)
		}
		if _, ok := seen[sourceClass]; ok {
			continue
		}
		seen[sourceClass] = struct{}{}
		out = append(out, sourceClass)
	}
	sort.Strings(out)
	return out, nil
}

func mergeSettings(defaults Settings, override Settings) Settings {
	out := normalizeSettings(defaults)
	override = normalizeSettings(override)
	if override.Limits.MaxChunkBytes != 0 {
		out.Limits.MaxChunkBytes = override.Limits.MaxChunkBytes
	}
	if override.Limits.MaxTokensPerChunk != 0 {
		out.Limits.MaxTokensPerChunk = override.Limits.MaxTokensPerChunk
	}
	if override.Limits.MaxDailyTokens != 0 {
		out.Limits.MaxDailyTokens = override.Limits.MaxDailyTokens
	}
	if override.Limits.MaxDailyCostMicros != 0 {
		out.Limits.MaxDailyCostMicros = override.Limits.MaxDailyCostMicros
	}
	if override.Redaction.Mode != "" {
		out.Redaction.Mode = override.Redaction.Mode
	}
	if override.Redaction.PolicyRef != "" {
		out.Redaction.PolicyRef = override.Redaction.PolicyRef
	}
	if override.Retention.Posture != "" {
		out.Retention.Posture = override.Retention.Posture
	}
	if override.Retention.Prompt != "" {
		out.Retention.Prompt = override.Retention.Prompt
	}
	if override.Retention.Response != "" {
		out.Retention.Response = override.Retention.Response
	}
	return out
}

func normalizeSettings(settings Settings) Settings {
	settings.Redaction.Mode = strings.TrimSpace(settings.Redaction.Mode)
	settings.Redaction.PolicyRef = strings.TrimSpace(settings.Redaction.PolicyRef)
	settings.Retention.Posture = strings.TrimSpace(settings.Retention.Posture)
	settings.Retention.Prompt = strings.TrimSpace(settings.Retention.Prompt)
	settings.Retention.Response = strings.TrimSpace(settings.Retention.Response)
	return settings
}

func validateSettings(settings Settings) error {
	if settings.Limits.MaxChunkBytes <= 0 {
		return fmt.Errorf("settings.limits.max_chunk_bytes must be positive")
	}
	if settings.Limits.MaxTokensPerChunk <= 0 {
		return fmt.Errorf("settings.limits.max_tokens_per_chunk must be positive")
	}
	if settings.Limits.MaxDailyTokens <= 0 && settings.Limits.MaxDailyCostMicros <= 0 {
		return fmt.Errorf("settings.limits must set max_daily_tokens or max_daily_cost_micros")
	}
	if settings.Limits.MaxDailyTokens < 0 || settings.Limits.MaxDailyCostMicros < 0 {
		return fmt.Errorf("settings.limits daily budgets must not be negative")
	}
	if !slices.Contains([]string{RedactionStrict, RedactionStandard}, settings.Redaction.Mode) {
		return fmt.Errorf("settings.redaction.mode %q is unsupported", settings.Redaction.Mode)
	}
	if !slices.Contains([]string{RetentionMetadataOnly, RetentionHashOnly}, settings.Retention.Posture) {
		return fmt.Errorf("settings.retention.posture %q is unsupported", settings.Retention.Posture)
	}
	if !slices.Contains([]string{RetentionNone, RetentionHashOnly}, settings.Retention.Prompt) {
		return fmt.Errorf("settings.retention.prompt %q is unsupported", settings.Retention.Prompt)
	}
	if !slices.Contains([]string{RetentionHashOnly, RetentionBoundedExcerpt}, settings.Retention.Response) {
		return fmt.Errorf("settings.retention.response %q is unsupported", settings.Retention.Response)
	}
	return nil
}

func isSupportedSourceClass(sourceClass string) bool {
	return slices.Contains([]string{
		semanticprofile.SourceDocumentation,
		semanticprofile.SourceDiagramsImages,
		semanticprofile.SourceTicketsChat,
		semanticprofile.SourceCodeHints,
		semanticprofile.SourceSearchDocuments,
		semanticprofile.SourceAgentReasoning,
	}, sourceClass)
}

func isSupportedScopeKind(kind string) bool {
	return slices.Contains([]string{ScopeOrganization, ScopeTenant, ScopeProject, ScopeRepository}, kind)
}

func isSupportedSourceSelector(kind string) bool {
	return slices.Contains([]string{
		SourceSelectorPathPrefix,
		SourceSelectorSourceID,
		SourceSelectorDocumentID,
		SourceSelectorSourceURIHash,
		SourceSelectorAll,
	}, kind)
}
