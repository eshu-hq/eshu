// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package facet provides deterministic, pre-engine question scoping for Ask
// Eshu. It scans a natural-language question for recognizable source-tool
// tokens and programming-language names without touching the LLM loop, and
// returns a Facets value the handler can record on the response and add to
// the system-prompt context.
//
// Design principles:
//   - Only the canonical sourcetool vocabulary is matched for source_tool; no
//     guessing against arbitrary strings.
//   - Language matching uses the parser registry's Language fields; the facet
//     is informational (the LLM decides which argument to pass).
//   - UnknownToolMention is populated only when the question contains a word
//     that looks unambiguously like a named tool (bare word that follows
//     recognized trigger phrases) but is not in the canonical vocabulary.
//     When unsure, the field is left empty to avoid false positives.
//   - Collision-prone tokens (words that are also common English words, e.g.
//     "go", "salt", "chef", "cargo", "pip") are only matched when a
//     disambiguating qualifier appears nearby. When unsure, leave empty —
//     never a false positive.
//   - All detection is purely textual, whole-word, and case-insensitive.
package facet

import (
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

// Facets carries the scoped dimensions detected in a question.
//
// All fields are lowercase and already validated against the canonical
// vocabulary; they can be passed directly as tool arguments.
type Facets struct {
	// SourceTool is the canonical source_tool token detected in the question
	// (e.g. "helm", "terraform"). Empty when no canonical tool is found.
	SourceTool string
	// Language is the detected programming language name from the parser
	// registry (e.g. "go", "python", "typescript"). Empty when not detected.
	Language string
	// UnknownToolMention is set when the question appears to reference a
	// specific named tool but that name is not in the canonical vocabulary.
	// The caller should surface a "not a recognized tool" note. Empty when
	// the question is ambiguous or no unknown tool is mentioned.
	UnknownToolMention string
}

// sourcetoolAliases maps common colloquial names to their canonical source_tool
// token. Only unambiguous aliases are listed; prefer the canonical token wherever
// possible. Single-word aliases for canonical tokens may be listed here so they
// are matched before the language detector can claim the word.
var sourcetoolAliases = map[string]string{
	"github actions": "github_actions",
	"github-actions": "github_actions",
	"docker compose": "docker_compose",
	"docker-compose": "docker_compose",
	"terragrunt":     "terragrunt",
	// "jenkinsfile" is the conventional filename but questions often mention it
	// instead of "jenkins" directly.
	"jenkinsfile": "jenkins",
}

// languageAliases maps common colloquial language references to the parser
// registry Language value. Source-tool names (e.g. "terraform", "gomod") must
// NOT appear here — they are matched by detectCanonicalToken first and would
// produce a spurious language facet if also listed here.
//
// Common language names are listed explicitly so they are matched
// deterministically without depending on map iteration order over the full
// registry. Registry languages that are also canonical source_tool tokens
// (gomod, npm, pip, maven, cargo, gradle, hcl) are intentionally excluded;
// they are already covered by the source_tool detector.
var languageAliases = map[string]string{
	"go":         "go",
	"golang":     "go",
	"typescript": "typescript",
	"javascript": "javascript",
	"python":     "python",
	"ruby":       "ruby",
	"java":       "java",
	"kotlin":     "kotlin",
	"rust":       "rust",
	"scala":      "scala",
	"swift":      "swift",
	"php":        "php",
	"c#":         "c_sharp",
	"csharp":     "c_sharp",
	"c_sharp":    "c_sharp",
	"elixir":     "elixir",
	"haskell":    "haskell",
	"perl":       "perl",
	"dart":       "dart",
	"groovy":     "groovy",
	"sql":        "sql",
	"hcl":        "hcl",
}

// toolTriggerPhrases are phrase prefixes that signal an unknown-tool mention
// when the next word is not in the canonical vocabulary. The threshold is high:
// only well-known trigger phrases are included to avoid false positives.
var toolTriggerPhrases = []string{
	"via ",
	"using ",
	"deployed via ",
	"deployed using ",
	"managed via ",
	"managed using ",
}

// collisionTokens is the set of canonical source_tool tokens that are also
// common English words. Bare whole-word matching on these would produce false
// positives ("where should I go", "pinch of salt", "ask the chef"). They
// require a disambiguating qualifier before being treated as a facet.
var collisionTokens = map[string]struct{}{
	"go":    {},
	"salt":  {},
	"chef":  {},
	"cargo": {},
	"pip":   {},
	"npm":   {},
	"maven": {},
}

// collisionQualifiers are word/phrase fragments that, when appearing in close
// proximity to a collision token, confirm its use as a tool name. Each entry
// is a whole-word or phrase that must appear in the question alongside the
// token. The check is bidirectional: the qualifier can appear before or after
// the collision token within the same question.
//
// Kept deliberately tight: only phrases that are clearly tool-domain context
// are listed. Ambiguous qualifiers are not included.
var collisionQualifiers = []string{
	// package-management / build context
	"module",
	"modules",
	"package",
	"packages",
	"dependency",
	"dependencies",
	"repo",
	"repos",
	"repository",
	"repositories",
	"formula",
	"cookbook",
	"cookbooks",
	"chart",
	"charts",
	"service",
	"services",
	// deployment / tool-invocation context
	"via",
	"using",
	"deploy",
	"deploys",
	"deployed",
	"managed",
	"manages",
	"runs",
	// explicit tool references
	"install",
	"installs",
}

// collisionTokenMatches reports whether collision token tok appears as a whole
// word AND a qualifier word is also present in the question as a whole word.
// This prevents "where should I go" from matching "go" as a source tool while
// still matching "go modules" or "deploy via go".
func collisionTokenMatches(lower, tok string) bool {
	if !containsWholeWord(lower, tok) {
		return false
	}
	for _, q := range collisionQualifiers {
		if containsWholeWord(lower, q) {
			return true
		}
	}
	return false
}

// registryLanguages is the set of Language values from the default parser
// registry, built once at init time.
var registryLanguages = func() map[string]struct{} {
	reg := parser.DefaultRegistry()
	m := make(map[string]struct{})
	for _, def := range reg.Definitions() {
		m[strings.ToLower(def.Language)] = struct{}{}
	}
	return m
}()

// DetectFacets scans question for canonical source_tool tokens, recognized
// language names, and possible unknown-tool mentions. The result is
// deterministic and involves no LLM calls.
//
// Callers should pass the original user question; DetectFacets normalizes
// case internally. When the question is ambiguous or empty, the returned
// Facets has all fields empty.
func DetectFacets(question string) Facets {
	if strings.TrimSpace(question) == "" {
		return Facets{}
	}
	lower := strings.ToLower(question)

	// --- source_tool detection ---
	// Check multi-word aliases first (longest match wins).
	sourceTool := detectMultiWordAlias(lower)
	if sourceTool == "" {
		sourceTool = detectCanonicalToken(lower)
	}

	// --- language detection ---
	language := detectLanguage(lower)

	// --- unknown-tool mention ---
	// Only populate when no recognized source_tool was found and the question
	// mentions a trigger phrase followed by a word-like token.
	unknownTool := ""
	if sourceTool == "" {
		unknownTool = detectUnknownTool(lower)
	}

	return Facets{
		SourceTool:         sourceTool,
		Language:           language,
		UnknownToolMention: unknownTool,
	}
}

// detectMultiWordAlias checks for known multi-word source_tool aliases
// (e.g. "github actions", "docker compose"). Returns the canonical token or "".
func detectMultiWordAlias(lower string) string {
	for alias, canonical := range sourcetoolAliases {
		if !sourcetool.IsValid(canonical) {
			// alias points at a canonical token, confirm the target is valid
			continue
		}
		if containsPhrase(lower, alias) {
			return canonical
		}
	}
	return ""
}

// detectCanonicalToken matches single-word canonical source_tool tokens as
// whole words in the lowercased question. Collision-prone tokens (those that
// are also common English words) require an additional qualifier to avoid
// false positives; see collisionTokens and collisionTokenMatches.
func detectCanonicalToken(lower string) string {
	for _, token := range sourcetool.Canonical {
		if token == "unknown" {
			continue
		}
		if _, isCollision := collisionTokens[token]; isCollision {
			if collisionTokenMatches(lower, token) {
				return token
			}
			continue
		}
		if containsWholeWord(lower, token) {
			return token
		}
	}
	return ""
}

// collisionLanguages is the set of language alias keys that are also common
// English words. They require a qualifier before being treated as a language
// facet. "go" is the primary case: "where should I go" must not yield
// language=go, but "Go repos" or "what Go packages" should.
var collisionLanguages = map[string]struct{}{
	"go": {},
}

// detectLanguage matches the lowercased question against known language aliases
// and the registry language set. Returns the canonical language name or "".
//
// The alias table is checked first so well-known names are matched
// deterministically without relying on map iteration order. The registry
// fallback skips any language name that is also a canonical source_tool token
// (e.g. "gomod", "npm", "pip") to avoid returning tool names as languages.
// Collision-prone language aliases require a qualifier word to be present.
func detectLanguage(lower string) string {
	// Check aliases first (handles "golang" -> "go", "csharp" -> "c_sharp").
	for alias, lang := range languageAliases {
		if _, isCollision := collisionLanguages[alias]; isCollision {
			if collisionTokenMatches(lower, alias) {
				return lang
			}
			continue
		}
		if containsWholeWord(lower, alias) {
			return lang
		}
	}
	// Fall back to direct registry language names as whole words, skipping
	// names that are also canonical source_tool tokens. Collision-prone
	// language names (e.g. "go") also require a qualifier here.
	for lang := range registryLanguages {
		if lang == "" {
			continue
		}
		if sourcetool.IsValid(lang) {
			continue // already covered by source_tool detector
		}
		if _, isCollision := collisionLanguages[lang]; isCollision {
			if collisionTokenMatches(lower, lang) {
				return lang
			}
			continue
		}
		if containsWholeWord(lower, lang) {
			return lang
		}
	}
	return ""
}

// detectUnknownTool looks for a trigger phrase followed by a bare word-like
// token that is not in the canonical source_tool vocabulary. Returns the
// unknown word (lowercased) or "".
//
// This function is intentionally conservative: it only fires when the word
// follows a recognized trigger phrase AND the word is not in Canonical.
// Ambiguous cases return "".
func detectUnknownTool(lower string) string {
	for _, trigger := range toolTriggerPhrases {
		idx := strings.Index(lower, trigger)
		if idx < 0 {
			continue
		}
		after := strings.TrimSpace(lower[idx+len(trigger):])
		word := extractFirstWord(after)
		if word == "" {
			continue
		}
		// Ignore if it is a canonical token (already handled above).
		if sourcetool.IsValid(word) {
			continue
		}
		// Ignore short common prepositions/articles that are not tool names.
		if isCommonWord(word) {
			continue
		}
		return word
	}
	return ""
}

// containsPhrase reports whether s contains the exact phrase (with space
// boundaries on both sides or at the string edges).
func containsPhrase(s, phrase string) bool {
	idx := strings.Index(s, phrase)
	if idx < 0 {
		return false
	}
	end := idx + len(phrase)
	// Check that the phrase is not part of a longer word.
	before := idx == 0 || !isWordChar(rune(s[idx-1]))
	after := end >= len(s) || !isWordChar(rune(s[end]))
	return before && after
}

// containsWholeWord reports whether word appears as a whole word in s.
func containsWholeWord(s, word string) bool {
	start := 0
	for {
		idx := strings.Index(s[start:], word)
		if idx < 0 {
			return false
		}
		abs := start + idx
		end := abs + len(word)
		before := abs == 0 || !isWordChar(rune(s[abs-1]))
		after := end >= len(s) || !isWordChar(rune(s[end]))
		if before && after {
			return true
		}
		start = abs + 1
	}
}

// extractFirstWord returns the first contiguous sequence of word characters
// from s (letters, digits, hyphens within a word). Hyphens are treated as
// word characters only when surrounded by letters (e.g. "my-tool").
func extractFirstWord(s string) string {
	s = strings.TrimLeftFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	end := 0
	for end < len(s) {
		r := rune(s[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			end++
		} else {
			break
		}
	}
	return strings.TrimRight(s[:end], "-_")
}

func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// isCommonWord rejects short articles, prepositions, and pronouns that are
// not plausible tool names.
func isCommonWord(w string) bool {
	switch w {
	case "a", "an", "the", "it", "its", "my", "our", "their",
		"this", "that", "some", "all", "any", "no",
		"in", "on", "at", "to", "by", "of", "or", "and",
		"we", "they", "i", "me", "you", "he", "she":
		return true
	}
	return false
}
