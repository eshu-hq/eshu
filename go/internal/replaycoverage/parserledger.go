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

// ParserLedgerFileName is the parser-backing ledger file inside the specs
// directory. It is the registry of supported parsers the coverage gate
// enumerates as required parser-fixture replay targets.
const ParserLedgerFileName = "parser-backing-ledger.v1.yaml"

// ParserLedger is the parsed parser-backing ledger: the set of supported parsers
// Eshu claims, each of which must have a green replay (parser-fixture) scenario.
type ParserLedger struct {
	// Version mirrors the ledger schema version.
	Version int
	// Parsers are the ledger entries sorted by parser name for deterministic
	// enumeration.
	Parsers []ParserLedgerEntry
}

// ParserLedgerEntry is one supported parser from the ledger. Only the parser
// identity is modeled here; the coverage gate keys on the parser name and does
// not consume the ledger's prose backing/source-file fields.
type ParserLedgerEntry struct {
	// Parser is the stable parser name (e.g. "hcl", "dockerfile").
	Parser string
}

type parserLedgerFile struct {
	Version       int                     `yaml:"version"`
	ParserBacking []parserLedgerFileEntry `yaml:"parser_backing"`
}

type parserLedgerFileEntry struct {
	Parser string `yaml:"parser"`
}

// LoadParserLedger reads the parser-backing ledger from path and returns the
// supported parsers sorted by name. A missing file is an error: the ledger is a
// required source of truth for the coverage gate, so a silent empty enumeration
// would be a false green. A blank parser name is rejected for the same reason.
func LoadParserLedger(path string) (ParserLedger, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- path is the operator-configured parser ledger under specs/, not external input
	if err != nil {
		return ParserLedger{}, fmt.Errorf("read parser ledger %s: %w", path, err)
	}
	var parsed parserLedgerFile
	if err := yaml.Unmarshal(raw, &parsed); err != nil {
		return ParserLedger{}, fmt.Errorf("parse parser ledger %s: %w", path, err)
	}
	ledger := ParserLedger{Version: parsed.Version}
	seen := map[string]struct{}{}
	for _, rec := range parsed.ParserBacking {
		name := strings.TrimSpace(rec.Parser)
		if name == "" {
			return ParserLedger{}, fmt.Errorf("parser ledger %s: entry has blank parser name", path)
		}
		if _, dup := seen[name]; dup {
			return ParserLedger{}, fmt.Errorf("parser ledger %s: duplicate parser %q", path, name)
		}
		seen[name] = struct{}{}
		ledger.Parsers = append(ledger.Parsers, ParserLedgerEntry{Parser: name})
	}
	sort.Slice(ledger.Parsers, func(i, j int) bool {
		return ledger.Parsers[i].Parser < ledger.Parsers[j].Parser
	})
	return ledger, nil
}
