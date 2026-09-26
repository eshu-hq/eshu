// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backendconformance

import "regexp"

// singleLabelAnchor matches a read whose leading pattern is one variable
// bound to exactly one label and nothing else: `MATCH (n:Label) `. That is
// the per-label probe shape a label-dispatch or per-label fan-out read
// renders once per candidate label. Inline property maps, label
// disjunctions, and later patterns are deliberately out of scope.
var singleLabelAnchor = regexp.MustCompile(`^MATCH \((\w+):(\w+)\) `)

// unlabeledAnchorText returns text with its leading single-label anchor
// stripped (`MATCH (n:Label) ...` becomes `MATCH (n) ...`), or text
// unchanged when it has no such anchor. Two reads with equal results
// differ at most in that anchor label.
func unlabeledAnchorText(text string) string {
	return singleLabelAnchor.ReplaceAllString(text, "MATCH (${1}) ")
}

// uidSeekConjunct matches the uid index-seek conjunct a uid-anchored
// per-label read renders ahead of its id predicate:
// `v.uid = $p AND v.id = $p`. RE2 has no backreferences, so the two
// variables and the two parameters are captured separately and
// foldUIDSeekConjunct compares them.
var uidSeekConjunct = regexp.MustCompile(`\b(\w+)\.uid = (\$\w+) AND (\w+)\.id = (\$\w+)`)

// foldUIDSeekConjunct rewrites every uid index-seek conjunct
// `v.uid = $p AND v.id = $p` in text to the id predicate `v.id = $p` that it
// implies (#7089): the uid equality only steers the read to the uniquely
// indexed property, and canonical nodes carry id == uid. A conjunct whose two
// sides name different variables or different parameters is not a seek on one
// node's id, so it is left unchanged, as are uid-only and OR predicates.
func foldUIDSeekConjunct(text string) string {
	return uidSeekConjunct.ReplaceAllStringFunc(text, func(match string) string {
		parts := uidSeekConjunct.FindStringSubmatch(match)
		if parts[1] != parts[3] || parts[2] != parts[4] {
			return match
		}
		return parts[1] + ".id = " + parts[2]
	})
}

// familyKeyText returns the canonical text two sibling reads share when they
// are one logical read: the leading single-label anchor stripped and the uid
// index-seek conjunct folded to the id predicate it implies.
func familyKeyText(text string) string {
	return foldUIDSeekConjunct(unlabeledAnchorText(text))
}

// labelDispatchFamilies maps every read text that the recordings prove is
// one member of a same-parameter sibling read to that read's family key,
// its canonical anchor text (see familyKeyText). The "dispatch" in the name (and in the
// DispatchMisses field and the dispatch-miss report line) is the motivating
// case, not the only shape the rule groups.
//
// The motivating case is a label-dispatch read (issue #7006): it resolves
// one bound id by issuing one single-label MATCH per candidate label,
// most-likely-first, and stops at the first row; on the pinned NornicDB
// build a label disjunction silently matches zero rows, so the per-label
// split is the only labeled anchor that resolves ids on both backends. For
// any one id, every label tried before the owning label misses by
// construction, so judging each label text as its own read would call those
// structural misses always-empty.
//
// The key also folds a uid index-seek conjunct `v.uid = $p AND v.id = $p` to
// `v.id = $p` (#7089): labels with a uid uniqueness constraint render the
// seek, the rest render the id predicate alone, and the two are one logical
// read, so a single exemption keyed by the id-only text covers both. The
// trade-off is the fan-out one below: a uid-and-id member that misses while
// an id-only sibling hits is visible only as an advisory dispatch miss.
//
// Membership is proven from the recordings, never declared: two or more
// distinct texts that share an unlabeled anchor text AND were executed with
// byte-identical parameters form one family. Besides the first-hit-wins
// dispatch, the same evidence also groups independent per-label fan-out
// reads that share parameters and merge every label's rows, probing all
// labels with no stop at a first hit (fetchOCIImagesByDigest in
// query/impact/trace_deployment_oci.go and
// resourceInvestigationSelectorCandidates in
// query/impact/resource_investigation_selector.go). Their non-owning-label
// misses are expected, so they are judged once and their misses stay
// advisory. That carries the same masking trade-off as a dispatch: a fan-out
// member broken on every execution stays green while a sibling returns rows,
// visible only as an advisory miss. Reads that differ only in their anchor
// label but never shared parameters stay independent and are judged on
// their own text.
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
		key := call{family: familyKeyText(text), parameters: record.Fingerprint.Parameters}
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
