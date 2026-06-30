// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package replaycoverage

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LanguageLedgerFileName is the language-feature-parity ledger inside the specs
// directory. It is the registry of every language Eshu claims to parse, and it
// is the denominator for the C-11 (#4364) language-parser coverage scoreboard:
// each language is either exercised by the golden-corpus corpus (exempt) or a
// not-yet-authored parser-fixture gap (the C-12 #4365 worklist). This is a
// separate, visibility-only scoreboard from the blocking surface reconcile, so
// no language is silently absent from the coverage count.
const LanguageLedgerFileName = "language-feature-parity-ledger.v1.yaml"

// LanguageLedger is the parsed language-feature-parity ledger: the full set of
// languages Eshu claims to parse, sorted by language name for deterministic
// enumeration.
type LanguageLedger struct {
	// Version mirrors the ledger schema version.
	Version int
	// Languages are the ledger entries sorted by language name.
	Languages []LanguageLedgerEntry
}

// LanguageLedgerEntry is one language from the ledger. Only the language
// identity is modeled here; the scoreboard keys on the language name and does
// not consume the ledger's feature/source-file fields.
type LanguageLedgerEntry struct {
	// Language is the stable language name (e.g. "go", "rust", "argocd").
	Language string
}

type languageLedgerFile struct {
	Version          int                         `yaml:"version"`
	LanguageFeatures []languageLedgerFileFeature `yaml:"language_features"`
}

type languageLedgerFileFeature struct {
	Language string `yaml:"language"`
}

// LoadLanguageLedger reads the language-feature-parity ledger from path and
// returns every language sorted by name. A missing file is an error: the ledger
// is the source-of-truth denominator for the language-parser scoreboard, so a
// silent empty enumeration would falsely claim every language is covered. A
// blank or duplicate language name is rejected for the same reason.
func LoadLanguageLedger(path string) (LanguageLedger, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured language ledger under specs/, not external input
	if err != nil {
		return LanguageLedger{}, fmt.Errorf("read language ledger %s: %w", path, err)
	}
	var parsed languageLedgerFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return LanguageLedger{}, fmt.Errorf("parse language ledger %s: %w", path, err)
	}
	ledger := LanguageLedger{Version: parsed.Version}
	seen := map[string]struct{}{}
	for _, rec := range parsed.LanguageFeatures {
		name := strings.TrimSpace(rec.Language)
		if name == "" {
			return LanguageLedger{}, fmt.Errorf("language ledger %s: entry has blank language name", path)
		}
		if _, dup := seen[name]; dup {
			return LanguageLedger{}, fmt.Errorf("language ledger %s: duplicate language %q", path, name)
		}
		seen[name] = struct{}{}
		ledger.Languages = append(ledger.Languages, LanguageLedgerEntry{Language: name})
	}
	sort.Slice(ledger.Languages, func(i, j int) bool {
		return ledger.Languages[i].Language < ledger.Languages[j].Language
	})
	return ledger, nil
}
