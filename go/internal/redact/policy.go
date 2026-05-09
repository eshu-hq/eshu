package redact

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// ReasonKnownSensitiveKey marks a caller-provided sensitive field match.
	ReasonKnownSensitiveKey = "known_sensitive_key"
	// ReasonKnownProviderSchema marks a field preserved under known schema coverage.
	ReasonKnownProviderSchema = "known_provider_schema"
	// ReasonUnknownProviderSchema marks conservative handling for unavailable schema coverage.
	ReasonUnknownProviderSchema = "unknown_provider_schema"
	// ReasonUnknownRuleSet marks fail-closed handling for an uninitialized policy.
	ReasonUnknownRuleSet = "unknown_redaction_ruleset"
	// ReasonUnknownFieldKind marks fail-closed handling for an unsafe field shape.
	ReasonUnknownFieldKind = "unknown_field_kind"
)

// SchemaTrust describes whether the caller has reliable schema coverage for a
// field being classified.
type SchemaTrust string

const (
	// SchemaKnown means the caller has schema coverage for the field.
	SchemaKnown SchemaTrust = "known"
	// SchemaUnknown means the caller cannot classify field sensitivity safely.
	SchemaUnknown SchemaTrust = "unknown"
)

// FieldKind describes the shape of a field before a redaction decision.
type FieldKind string

const (
	// FieldScalar is a scalar value that can be represented by Value.
	FieldScalar FieldKind = "scalar"
	// FieldComposite is a map, object, list, block, or other nested value.
	FieldComposite FieldKind = "composite"
)

// Action is the collector action selected by a redaction rule set.
type Action string

const (
	// ActionPreserve keeps the caller's value because it is not classified as sensitive.
	ActionPreserve Action = "preserve"
	// ActionRedact replaces the caller's scalar value with a redacted Value.
	ActionRedact Action = "redact"
	// ActionDrop omits a value that cannot be safely represented.
	ActionDrop Action = "drop"
)

// Decision is the collector-neutral classification result for one field.
type Decision struct {
	// Action tells the caller whether to preserve, redact, or omit the value.
	Action Action
	// Reason is the stable classification label callers may attach to evidence.
	Reason string
	// Source is the normalized caller-provided field path or source label.
	Source string
	// RuleSetVersion identifies the caller-owned sensitive-key policy version.
	RuleSetVersion string
}

// RuleSet is a versioned caller-owned sensitive-key policy.
//
// The package intentionally does not ship Terraform, AWS, or provider-specific
// keys. Collectors construct RuleSet values from their own schema bundles and
// config so this package remains collector-neutral.
type RuleSet struct {
	version       string
	sensitiveKeys map[string]struct{}
}

// NewRuleSet constructs a versioned sensitive-key classifier.
//
// Blank key entries are ignored. The version is required so downstream tests
// and audit evidence can prove which provider schema policy made a decision.
func NewRuleSet(version string, sensitiveKeys []string) (RuleSet, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return RuleSet{}, fmt.Errorf("redaction rule set version must not be blank")
	}

	rules := RuleSet{
		version:       version,
		sensitiveKeys: make(map[string]struct{}, len(sensitiveKeys)),
	}
	for _, key := range sensitiveKeys {
		normalized := normalizeSensitiveKey(key)
		if normalized == "" {
			continue
		}
		rules.sensitiveKeys[normalized] = struct{}{}
	}
	return rules, nil
}

// Version identifies the caller-owned provider schema or key policy version.
func (r RuleSet) Version() string {
	return normalizeContext(r.version)
}

// Classify chooses the redaction action for a caller-owned field path.
//
// Unknown schema coverage fails closed: scalar values are redacted and
// composite values are dropped because this package cannot safely serialize
// nested structures. Known schema coverage redacts caller-supplied sensitive
// keys and preserves other fields.
func (r RuleSet) Classify(source string, schemaTrust SchemaTrust, fieldKind FieldKind) Decision {
	normalizedSource := normalizeContext(source)
	if fieldKind != FieldScalar && fieldKind != FieldComposite {
		return r.decision(ActionDrop, ReasonUnknownFieldKind, normalizedSource)
	}
	if r.version == "" {
		return r.unknownRuleSetDecision(normalizedSource, fieldKind)
	}
	if schemaTrust != SchemaKnown {
		return r.unknownSchemaDecision(normalizedSource, fieldKind)
	}
	if r.isSensitiveSource(normalizedSource) {
		if fieldKind != FieldScalar {
			return r.decision(ActionDrop, ReasonKnownSensitiveKey, normalizedSource)
		}
		return r.decision(ActionRedact, ReasonKnownSensitiveKey, normalizedSource)
	}
	return r.decision(ActionPreserve, ReasonKnownProviderSchema, normalizedSource)
}

func (r RuleSet) unknownRuleSetDecision(source string, fieldKind FieldKind) Decision {
	if fieldKind == FieldScalar {
		return r.decision(ActionRedact, ReasonUnknownRuleSet, source)
	}
	return r.decision(ActionDrop, ReasonUnknownRuleSet, source)
}

func (r RuleSet) unknownSchemaDecision(source string, fieldKind FieldKind) Decision {
	if fieldKind == FieldScalar {
		return r.decision(ActionRedact, ReasonUnknownProviderSchema, source)
	}
	return r.decision(ActionDrop, ReasonUnknownProviderSchema, source)
}

func (r RuleSet) decision(action Action, reason string, source string) Decision {
	return Decision{
		Action:         action,
		Reason:         reason,
		Source:         source,
		RuleSetVersion: r.Version(),
	}
}

func (r RuleSet) isSensitiveSource(source string) bool {
	normalizedSource := normalizeSensitiveKey(source)
	if _, ok := r.sensitiveKeys[normalizedSource]; ok {
		return true
	}
	for _, segment := range sourceSegments(source) {
		if _, ok := r.sensitiveKeys[normalizeSensitiveKey(segment)]; ok {
			return true
		}
	}
	return false
}

func sourceSegments(source string) []string {
	parts := strings.FieldsFunc(source, func(value rune) bool {
		return value == '.' || value == '/' || value == '[' || value == ']'
	})
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if segment := strings.TrimSpace(part); segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

func normalizeSensitiveKey(value string) string {
	var builder strings.Builder
	value = strings.TrimSpace(value)
	runes := []rune(value)
	lastUnderscore := false
	var previous rune

	for index, current := range runes {
		if isIdentifierSeparator(current) {
			writeUnderscore(&builder, &lastUnderscore)
			previous = current
			continue
		}
		if unicode.IsUpper(current) && shouldSplitCamel(previous, nextRune(runes, index)) {
			writeUnderscore(&builder, &lastUnderscore)
		}
		builder.WriteRune(unicode.ToLower(current))
		lastUnderscore = false
		previous = current
	}

	return strings.Trim(builder.String(), "_")
}

func isIdentifierSeparator(value rune) bool {
	return value == '-' || value == '.' || value == '/' || value == '[' ||
		value == ']' || unicode.IsSpace(value)
}

func shouldSplitCamel(previous rune, next rune) bool {
	if !unicode.IsLetter(previous) && !unicode.IsDigit(previous) {
		return false
	}
	return unicode.IsLower(previous) || unicode.IsDigit(previous) ||
		(unicode.IsUpper(previous) && unicode.IsLower(next))
}

func nextRune(runes []rune, index int) rune {
	if index+1 >= len(runes) {
		return 0
	}
	return runes[index+1]
}

func writeUnderscore(builder *strings.Builder, lastUnderscore *bool) {
	if builder.Len() == 0 || *lastUnderscore {
		return
	}
	builder.WriteRune('_')
	*lastUnderscore = true
}
