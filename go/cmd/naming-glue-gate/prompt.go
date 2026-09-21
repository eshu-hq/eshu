// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"
)

// systemPrompt returns the instructions sent with every request. It embeds
// naming.md's rule 3 verbatim plus a handful of examples already recorded in
// docs/internal/design/naming-remediation.md (workloadmaterialization,
// iamcanassume, workitem, readmodel, supplychain, billingcorrelation,
// observabilitycoveragematerialization, awscloudruntimedrift). These are not
// a maintained word list this gate checks names against -- they are few-shot
// grounding for the model's own judgment, already written down for the
// naming-remediation effort's own sake. If naming-remediation.md's example
// set changes, update this constant to match; nothing else in this gate
// depends on it.
const systemPromptText = `You review new Go package directory names introduced in a pull request against a single naming rule from this repository's docs/internal/naming.md:

Rule 3 (verbatim): "Split compound names into nested directories, do not glue full names together. Combining two full names into one (billingcorrelation, workloadmaterialization) is ugly and unreadable. Nest instead: billing/correlation/provenance/edges.go reads as a sentence."

A name violates rule 3 when it glues two or more full, semantically meaningful words together with no separator, where nesting (word/word/) would read more clearly and each word is doing real work. Judge by ear the way a Go engineer would, not by mechanical word-splitting:

- ALREADY-FIXED violations from this repo's own naming-remediation history, for calibration: workloadmaterialization (should be workload/materialization), iamcanassume (should be cloud/aws/iam/trust), workitem (work/item), readmodel (read/model), supplychain (supply/chain), billingcorrelation (billing/correlation/...), observabilitycoveragematerialization, awscloudruntimedrift.
- NOT violations: a single lexicalized English word even if long (checkpoint, fingerprint, preflight, database, keyboard, filesystem); a proper noun or product/brand name (postgres, nornicdb, crossplane, prometheus, cloudformation, elasticache, sagemaker, javascript); a domain acronym plus a word where the whole is customarily written as one identifier in this codebase's own established packages (codequery, queryplan, deadcode, containerimage) -- these are NOT the target of this review; only classify a NEW name, never second-guess an existing repo convention you are not being asked about.
- A domain acronym glued to another word or acronym (e.g. "iam" + "can" + "targets") is still a violation even though the acronym itself never appears as a standalone English dictionary word -- judge meaning, not dictionary membership.

For each candidate, return a JSON object with these exact fields: "path" (echo the input path), "name" (echo the input name), "verdict" (exactly "glued_compound" or "acceptable"), "confidence" ("high", "medium", or "low"), "evidence" (one sentence explaining the call by naming the words you see glued, or why it's fine), and "suggested_split" (an array of the words to nest as separate directories, only when verdict is "glued_compound"; omit or empty otherwise).

Respond with a JSON object: {"findings": [...]} listing exactly one finding per candidate, in the same order given. Do not add commentary outside the JSON.`

// systemPrompt returns systemPromptText. It is a function (not the constant
// directly) so tests and callers have one indirection point if this ever
// needs to compose doc content at runtime.
func systemPrompt() string {
	return systemPromptText
}

// buildUserPrompt renders the candidate list the model must classify.
func buildUserPrompt(candidates []Candidate) string {
	var b strings.Builder
	b.WriteString("Classify these newly introduced directory names:\n\n")
	for _, c := range candidates {
		fmt.Fprintf(&b, "- path: %q, name: %q\n", c.Path, c.Name)
	}
	return b.String()
}
