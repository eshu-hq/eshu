// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "regexp"

// singleLabelAnchor matches a read whose leading pattern is one variable
// bound to exactly one label and nothing else: `MATCH (n:Label) `. That is
// the per-label probe shape a label-dispatch read renders once per
// candidate label. Inline property maps, label disjunctions, and later
// patterns are deliberately out of scope.
var singleLabelAnchor = regexp.MustCompile(`^MATCH \((\w+):(\w+)\) `)

// unlabeledAnchorText returns text with its leading single-label anchor
// stripped (`MATCH (n:Label) ...` becomes `MATCH (n) ...`), or text
// unchanged when it has no such anchor. Two reads with equal results
// differ at most in that anchor label.
func unlabeledAnchorText(text string) string {
	return singleLabelAnchor.ReplaceAllString(text, "MATCH (${1}) ")
}

// labelDispatchFamilies maps every read text that the recordings prove is
// one member of a label-dispatch read to that read's family key, its
// unlabeled anchor text.
//
// A label-dispatch read (issue #7006) resolves one bound id by issuing one
// single-label MATCH per candidate label, most-likely-first, and stops at
// the first row; on the pinned NornicDB build a label disjunction silently
// matches zero rows, so the per-label split is the only labeled anchor that
// resolves ids on both backends. For any one id, every label tried before
// the owning label misses by construction, so judging each label text as
// its own read would call those structural misses always-empty.
//
// Membership is proven from the recordings, never declared: two or more
// distinct texts that share an unlabeled anchor text AND were executed with
// byte-identical parameters are one lookup dispatched across labels. Reads
// that differ only in their anchor label but never shared parameters stay
// independent and are judged on their own text.
func labelDispatchFamilies(records []DifferentialRecord) map[string]string {
	type call struct {
		family     string
		parameters string
	}
	textsByCall := make(map[call]map[string]struct{})
	for _, record := range records {
		if record.Digest == "" {
			continue
		}
		text := normalizeCoverageText(record.Fingerprint.Statement)
		key := call{family: unlabeledAnchorText(text), parameters: record.Fingerprint.Parameters}
		texts, ok := textsByCall[key]
		if !ok {
			texts = make(map[string]struct{})
			textsByCall[key] = texts
		}
		texts[text] = struct{}{}
	}
	families := make(map[string]string)
	for key, texts := range textsByCall {
		if len(texts) < 2 {
			continue
		}
		for text := range texts {
			families[text] = key.family
		}
	}
	return families
}

// readFamilyExempt reports whether an always-empty read family is excused:
// by an exemption naming its key (for a dispatch family, the unlabeled read
// it replaced), or by exemptions naming every one of its member texts.
func readFamilyExempt(key string, members []string, exemptions map[string]string) bool {
	if _, ok := exemptions[key]; ok {
		return true
	}
	for _, member := range members {
		if _, ok := exemptions[member]; !ok {
			return false
		}
	}
	return len(members) > 0
}
