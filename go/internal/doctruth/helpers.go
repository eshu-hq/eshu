package doctruth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func entityAliases(entity Entity) []string {
	values := []string{entity.ID, entity.DisplayName}
	values = append(values, entity.Aliases...)
	values = append(values, entity.CodePaths...)
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		key := normalizeText(value)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func appendRefs(existing []facts.DocumentationEvidenceRef, values []facts.DocumentationEvidenceRef) []facts.DocumentationEvidenceRef {
	out := existing
	for _, value := range values {
		out = appendUniqueRef(out, value)
	}
	return out
}

func appendUniqueRef(existing []facts.DocumentationEvidenceRef, value facts.DocumentationEvidenceRef) []facts.DocumentationEvidenceRef {
	for _, candidate := range existing {
		if candidate.Kind == value.Kind && candidate.ID == value.ID {
			return existing
		}
	}
	return append(existing, value)
}

func sortRefs(refs []facts.DocumentationEvidenceRef) {
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Kind == refs[j].Kind {
			return refs[i].ID < refs[j].ID
		}
		return refs[i].Kind < refs[j].Kind
	})
}

func containsToken(text, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	lowerText := strings.ToLower(text)
	lowerToken := strings.ToLower(token)
	offset := 0
	for {
		index := strings.Index(lowerText[offset:], lowerToken)
		if index == -1 {
			return false
		}
		start := offset + index
		end := start + len(lowerToken)
		if isTokenBoundary(text, start-1) && isTokenBoundary(text, end) {
			return true
		}
		offset = start + 1
	}
}

func isTokenBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	runeValue := rune(text[index])
	return !unicode.IsLetter(runeValue) && !unicode.IsDigit(runeValue) && runeValue != '_' && runeValue != '-'
}

func normalizeText(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func mentionID(section SectionInput, candidate mentionCandidate) string {
	return "mention:" + facts.StableID("DocumentationMention", map[string]any{
		"document_id":  section.DocumentID,
		"revision_id":  section.RevisionID,
		"section_id":   section.SectionID,
		"text":         normalizeText(candidate.text),
		"kind":         candidate.kind,
		"excerpt_hash": section.ExcerptHash,
	})
}

func claimID(section SectionInput, hint ClaimHint) string {
	return "claim:" + facts.StableID("DocumentationClaim", map[string]any{
		"document_id": section.DocumentID,
		"revision_id": section.RevisionID,
		"section_id":  section.SectionID,
		"claim_text":  hint.ClaimText,
		"claim_type":  hint.ClaimType,
	})
}

func textHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func payloadToMap(payload any) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
