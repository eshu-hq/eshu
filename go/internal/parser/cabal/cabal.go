// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cabal

import (
	"bytes"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// sourceKindCabalTestedWith names the tested-with declaration section.
const sourceKindCabalTestedWith = "cabal_tested_with"

// ExtractCabalRuntimeEvidence reads the tested-with specs of one .cabal body.
// Only a column-zero tested-with field is read; continuations extend it and
// entries split on commas at the line their text starts on. An indented
// tested-with, a conditional in continuation position, a package-level
// conditional, a duplicate or empty field, or a NUL byte fails closed to
// unavailable with no values retained. Every entry passes the shared
// SafeVersionSpec guard, so credential-shaped values drop counted.
func ExtractCabalRuntimeEvidence(source []byte) shared.RuntimeEvidence {
	evidence := shared.NewRuntimeEvidence(source)
	if evidence.State == "over_cap" {
		return evidence
	}
	if bytes.IndexByte(source, 0) >= 0 {
		evidence.State = "unavailable"
		return evidence
	}
	scan := cabalScan{evidence: &evidence}
	for number, line := range bytes.Split(source, []byte{'\n'}) {
		if scan.poisoned {
			break
		}
		scan.visit(string(line), number+1)
	}
	if scan.poisoned {
		evidence.Values = nil
		evidence.State = "unavailable"
	} else {
		scan.flush()
	}
	return evidence
}

type cabalSegment struct {
	text string
	line int
}
type cabalScan struct {
	evidence   *shared.RuntimeEvidence
	segments   []cabalSegment
	seen       bool
	accumulate bool
	poisoned   bool
}

func (s *cabalScan) visit(line string, number int) {
	head, _, _ := strings.Cut(line, "--")
	if line != "" && (line[0] == ' ' || line[0] == '\t') {
		if name, _, found := strings.Cut(head, ":"); found && strings.EqualFold(strings.TrimSpace(name), "tested-with") {
			s.poisoned = true
			return
		}
		if s.accumulate {
			if text := strings.TrimSpace(head); text != "" {
				switch firstWord(text) {
				case "if", "elif", "else":
					s.poisoned = true
					return
				}
				s.segments = append(s.segments, cabalSegment{text: text, line: number})
			}
		}
		return
	}
	s.flush()
	if trimmed := strings.TrimSpace(head); trimmed != "" {
		switch firstWord(trimmed) {
		case "if", "elif", "else":
			s.poisoned = true
			return
		}
		name, value, found := strings.Cut(trimmed, ":")
		if found && strings.EqualFold(strings.TrimSpace(name), "tested-with") {
			if s.seen {
				s.poisoned = true
				return
			}
			s.seen, s.accumulate = true, true
			if text := strings.TrimSpace(value); text != "" {
				s.segments = append(s.segments, cabalSegment{text: text, line: number})
			}
		}
	}
}

func (s *cabalScan) flush() {
	if !s.accumulate {
		return
	}
	s.accumulate = false
	if len(s.segments) == 0 {
		s.poisoned = true
		return
	}
	var joined strings.Builder
	type mark struct {
		offset, line int
	}
	marks := make([]mark, 0, len(s.segments))
	for i, segment := range s.segments {
		if i > 0 {
			joined.WriteByte(' ')
		}
		marks = append(marks, mark{offset: joined.Len(), line: segment.line})
		joined.WriteString(segment.text)
	}
	s.segments = nil
	value := joined.String()
	lineFor := func(offset int) int {
		line := marks[0].line
		for _, m := range marks {
			if m.offset > offset {
				break
			}
			line = m.line
		}
		return line
	}
	start := 0
	for i := 0; i <= len(value); i++ {
		if i == len(value) || value[i] == ',' {
			raw := value[start:i]
			lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
			s.evidence.Add(sourceKindCabalTestedWith, "haskell", strings.TrimSpace(raw), lineFor(start+lead))
			start = i + 1
		}
	}
}

func firstWord(line string) string {
	if i := strings.IndexAny(line, " \t:"); i >= 0 {
		return strings.ToLower(line[:i])
	}
	return strings.ToLower(line)
}

// Parse reads one .cabal file and returns the parser payload the parent
// engine expects. The payload carries the cabal language tag, never the
// haskell source tag, so a .cabal file is never parsed as Haskell source.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	payload := shared.BasePayload(path, "cabal", isDependency)
	if options.IndexSource {
		payload["source"] = string(source)
	}
	payload["cabal_runtime_evidence"] = ExtractCabalRuntimeEvidence(source)
	return payload, nil
}
