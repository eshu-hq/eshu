// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Boilerplate thresholds. Wording repeated across many tool descriptions, such
// as the scoped-token refusal sentence, says nothing about which tool fits a
// task, so it must not drive the score. The rules only apply to a registry of at
// least boilerplateMinCorpus tools, so a small fixture's legitimately shared
// wording is not stripped.
const (
	// boilerplateMinDocs is how many distinct tool descriptions must carry a
	// sentence or word run before it counts as boilerplate.
	boilerplateMinDocs = 3
	// boilerplateMinCorpus is the smallest registry the rules apply to.
	boilerplateMinCorpus = 12
	// boilerplateGram is the length, in stemmed non-stopword tokens, of the word
	// run compared across descriptions. Four is long enough that an ordinary
	// two- or three-word phrase such as "dead code" is never flagged by itself.
	boilerplateGram = 4
)

// boilerplate holds the corpus-wide repeated wording found in a registry.
//
// Two rules apply to description text only; names and curated intents are never
// stripped. A sentence repeated verbatim (ignoring case and spacing) in
// boilerplateMinDocs descriptions is dropped whole. A template that varies a
// slot, such as "Scoped tokens receive only <X> whose every hop is in a granted
// repository", is caught by the second rule: any run of boilerplateGram tokens
// shared by boilerplateMinDocs descriptions is flagged, and a token is dropped
// when a flagged run covers it. The slot word is in no shared run, so it stays
// searchable.
//
// The sets are a property of the registry build, not of the caller, so they do
// not vary with the caller's allowed set.
type boilerplate struct {
	sentences map[string]struct{}
	grams     map[string]struct{}
}

// findBoilerplate scans every tool description once. It returns nil, which
// strips nothing, for a registry below boilerplateMinCorpus tools.
func findBoilerplate(tools []toolcontract.ToolDefinition) *boilerplate {
	if len(tools) < boilerplateMinCorpus {
		return nil
	}
	sentenceDocs := map[string]int{}
	gramDocs := map[string]int{}
	for _, t := range tools {
		seenSentence := map[string]struct{}{}
		seenGram := map[string]struct{}{}
		for _, s := range sentences(t.Description) {
			if k := sentenceKey(s); !has(seenSentence, k) {
				seenSentence[k] = struct{}{}
				sentenceDocs[k]++
			}
			toks := tokens(s)
			for i := 0; i+boilerplateGram <= len(toks); i++ {
				if g := gram(toks, i); !has(seenGram, g) {
					seenGram[g] = struct{}{}
					gramDocs[g]++
				}
			}
		}
	}
	b := &boilerplate{sentences: map[string]struct{}{}, grams: map[string]struct{}{}}
	for k, n := range sentenceDocs {
		if n >= boilerplateMinDocs {
			b.sentences[k] = struct{}{}
		}
	}
	for g, n := range gramDocs {
		if n >= boilerplateMinDocs {
			b.grams[g] = struct{}{}
		}
	}
	return b
}

// keep returns the tokens of one description sentence that are not boilerplate,
// or nil when the whole sentence is. A nil receiver keeps every token.
func (b *boilerplate) keep(sentence string) []string {
	toks := tokens(sentence)
	if b == nil {
		return toks
	}
	if has(b.sentences, sentenceKey(sentence)) {
		return nil
	}
	covered := make([]bool, len(toks))
	for i := 0; i+boilerplateGram <= len(toks); i++ {
		if has(b.grams, gram(toks, i)) {
			for j := i; j < i+boilerplateGram; j++ {
				covered[j] = true
			}
		}
	}
	kept := toks[:0:0]
	for i, tok := range toks {
		if !covered[i] {
			kept = append(kept, tok)
		}
	}
	return kept
}

// gram joins boilerplateGram tokens starting at i into one comparable key.
func gram(toks []string, i int) string {
	return strings.Join(toks[i:i+boilerplateGram], " ")
}

func has(set map[string]struct{}, k string) bool {
	_, ok := set[k]
	return ok
}
