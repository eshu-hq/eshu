// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"sort"
	"strings"
)

// This file holds the #5510 semantic half of the language-parity read-surface
// gate.
//
// The existing gate (read_surface_consumer_existence.go) proves a cited read
// surface RESOLVES: the tool name is registered, the Go symbol exists. That is
// necessary and not sufficient. A row can cite a real tool that cannot answer
// for that language's claimed feature and stay green forever, because "the tool
// exists" and "the language's claim is answerable" are different assertions.
//
// #5361 found that concretely: eight languages cited trace_route_callers for
// route truth, but only Java had a chained parser -> reducer -> query proof.
// The reducer's handler-string resolution is exact-only and silently drops the
// edge for any language whose parser emits a convention the entity index does
// not key. Every gate stayed green.
//
// So this file adds the second axis. A read-surface label is classified by what
// it CLAIMS, and a label that claims per-language queryable truth needs a
// registered per-language chained proof — or the row must say, in the ledger
// itself, that the feature is partial.
//
// Registry-derived by construction: the gate reads this map and the ledger, and
// never probes a graph. A proof is registered by naming the test that runs the
// chain, so adding a language means writing the proof and citing it, not
// editing gate logic.

// readSurfaceClaimClass says what a read-surface label asserts about the
// language row citing it.
type readSurfaceClaimClass string

const (
	// claimQueryableTruth means citing this label asserts that THIS language's
	// parser output survives materialization and comes back out of the named
	// surface. That is a per-language claim, so it needs a per-language proof.
	claimQueryableTruth readSurfaceClaimClass = "queryable_truth"

	// claimGenericRead means the surface serves whatever the graph or content
	// store already holds, without a language-specific chain of its own. Citing
	// it asserts the surface exists and is reachable, which the existence gate
	// already proves. Requiring a per-language chained proof here would demand
	// evidence for a claim the row is not making.
	claimGenericRead readSurfaceClaimClass = "generic_read"
)

// readSurfaceClaimClasses classifies every label the ledger uses. A label
// missing from this map fails the gate closed: an unclassified label is one
// nobody has decided the meaning of, and defaulting it either way would either
// demand proofs that make no sense or wave through claims that need them.
var readSurfaceClaimClasses = map[string]readSurfaceClaimClass{
	// Per-language truth claims. Each asserts that this language's own parser
	// output reaches the surface.
	"trace_route_callers":         claimQueryableTruth,
	"get_code_relationship_story": claimQueryableTruth,
	"content_relationships":       claimQueryableTruth,
	"list_relationship_edges":     claimQueryableTruth,
	"find_dead_code":              claimQueryableTruth,
	"trace_deployment_chain":      claimQueryableTruth,
	"trace_resource_to_code":      claimQueryableTruth,

	// Generic reads. execute_language_query runs a caller-supplied query
	// against whatever is indexed and makes no per-language promise of its own.
	// entity_context serves the edges of ANY graph-projected node regardless of
	// which parser produced it (#5334 note on generic read surfaces), so the
	// per-language chain it would need is the projection itself, which the
	// golden corpus gate already asserts.
	"execute_language_query": claimGenericRead,
	"entity_context":         claimGenericRead,

	// The honest "no consumer" sentinel claims nothing.
	languageParityReadSurfaceNone: claimGenericRead,
}

// readSurfacePartialTokens maps a queryable-truth surface to the substrings a
// partial_features entry must contain to excuse THAT surface.
//
// Without this, "the row declares some partial feature" excuses every surface
// the row cites, so a language could mark one unrelated feature partial and
// avoid proving all of them -- a blanket waiver, and exactly the false-green
// this gate exists to remove. A partial feature only speaks for the claim it
// names.
var readSurfacePartialTokens = map[string][]string{
	"trace_route_callers":         {"route"},
	"get_code_relationship_story": {"relationship", "call", "outbound-contracts"},
	"content_relationships":       {"relationship", "reference", "dependenc"},
	"list_relationship_edges":     {"relationship", "edge", "dependenc"},
	"find_dead_code":              {"dead-code", "dead_code", "reachab"},
	"trace_deployment_chain":      {"deploy", "manifest", "render"},
	"trace_resource_to_code":      {"resource", "deploy", "provision"},
}

// partialFeatureExcuses reports whether any of the row's partial features
// actually speaks to this surface's claim.
func partialFeatureExcuses(surface string, partialFeatures []string) (string, bool) {
	tokens, ok := readSurfacePartialTokens[surface]
	if !ok {
		return "", false
	}
	for _, feature := range partialFeatures {
		lowered := strings.ToLower(feature)
		for _, token := range tokens {
			if strings.Contains(lowered, token) {
				return feature, true
			}
		}
	}
	return "", false
}

// languageQueryProof names the chained end-to-end test that backs one
// (language, surface) claim. Test is the exact test function a reader can run;
// Chain describes what it actually drives, so a reviewer can tell a real chain
// from a handler test with a hand-built fixture.
type languageQueryProof struct {
	Test  string
	Chain string
}

// languageParityQueryProofs is the registry of per-language chained proofs,
// keyed by language then surface label.
//
// A proof belongs here only when it drives the REAL chain: a real fixture file
// through the real parser, through the real materialization, into the real
// query handler. A test that hand-builds the reducer's output and asserts the
// handler formats it is not a proof of this claim — it cannot fail when the
// parser and the reducer disagree, which is the exact failure #5361 found.
var languageParityQueryProofs = map[string]map[string]languageQueryProof{
	"csharp": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/csharp",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"go": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/go",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"javascript": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/javascript",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"kotlin": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/kotlin",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"php": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/php",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"python": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/python",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"rust": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/rust",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"typescript": {
		"trace_route_callers": {
			Test:  "query.TestRouteQueryProofMatrix/typescript",
			Chain: "real fixture -> parser.DefaultEngine().ParsePath -> reducer.BuildHandlesRouteIntentRowsForQueryProof -> handleRouteToCaller",
		},
	},
	"java": {
		"trace_route_callers": {
			Test:  "query.TestHandleRouteToCallerResolvesJavaSpringHandler",
			Chain: "real Spring fixture -> parser -> handles-route intent rows -> handleRouteToCaller",
		},
	},
}

// queryProofFor reports the registered proof for one (language, surface) pair.
func queryProofFor(language, surface string) (languageQueryProof, bool) {
	bySurface, ok := languageParityQueryProofs[language]
	if !ok {
		return languageQueryProof{}, false
	}
	proof, ok := bySurface[surface]
	return proof, ok
}

// languageParityQueryProofExemptions records (language, surface) pairs that
// cite a queryable-truth surface, declare no matching partial feature, and
// still have no chained proof — each with the issue tracking the gap.
//
// This exists so the gate can be turned on at its real strictness today instead
// of waiting for every proof to be written, WITHOUT the exemption being
// invisible. Every entry is a debt with a name on it. An entry whose pair no
// longer needs it fails the staleness check below, so the list cannot quietly
// outlive its reason.
var languageParityQueryProofExemptions = map[string]map[string]string{
	// The gaps this gate found the day it was written: 27 (language, surface)
	// pairs citing a queryable-truth surface with no chained proof and no
	// partial feature that speaks to that surface. Every one passes the
	// existence gate today, because the tools they cite are real and
	// registered. Nothing verifies that these languages' parser output
	// actually reaches those surfaces.
	//
	// The first draft of this gate found only 6, because it let ANY partial
	// feature excuse EVERY surface on a row. Review (#5918) caught that blanket
	// waiver; scoping a partial feature to the surface it names surfaced the
	// other 21. That correction is the difference between a gate that looks
	// strict and one that is.
	//
	// Recorded rather than fixed here: 27 chained
	// fixture->parser->materialization->query proofs is substantial work per
	// row, and burying it inside the gate's own change would hide it. Each
	// entry is a debt with a name on it, staleness-checked so it cannot outlive
	// its reason. Tracked by #5510.
	"c": {
		"find_dead_code": "#5510: no chained proof that c parse output reaches find_dead_code",
	},
	"cloudformation": {
		"content_relationships": "#5510: no chained proof that cloudformation parse output reaches content_relationships",
	},
	"cpp": {
		"find_dead_code": "#5510: no chained proof that cpp parse output reaches find_dead_code",
	},
	"crossplane": {
		"content_relationships": "#5510: no chained proof that crossplane parse output reaches content_relationships",
	},
	"csharp": {
		"find_dead_code": "#5510: no chained proof that csharp parse output reaches find_dead_code",
	},
	"helm": {
		"content_relationships": "#5510: no chained proof that helm parse output reaches content_relationships",
	},
	"java": {
		"find_dead_code": "#5510: no chained proof that java parse output reaches find_dead_code",
	},
	"javascript": {
		"find_dead_code": "#5510: no chained proof that javascript parse output reaches find_dead_code",
	},
	"json": {
		"content_relationships": "#5510: no chained proof that json parse output reaches content_relationships",
	},
	"kotlin": {
		"find_dead_code": "#5510: no chained proof that kotlin parse output reaches find_dead_code",
	},
	"kubernetes": {
		"content_relationships":  "#5510: no chained proof that kubernetes parse output reaches content_relationships",
		"trace_deployment_chain": "#5510: no chained proof that kubernetes parse output reaches trace_deployment_chain",
	},
	"perl": {
		"find_dead_code": "#5510: no chained proof that perl parse output reaches find_dead_code",
	},
	"php": {
		"find_dead_code":              "#5510: no chained proof that php parse output reaches find_dead_code",
		"get_code_relationship_story": "#5510: no chained proof that php parse output reaches get_code_relationship_story",
	},
	"python": {
		"find_dead_code": "#5510: no chained proof that python parse output reaches find_dead_code",
	},
	"ruby": {
		"find_dead_code": "#5510: no chained proof that ruby parse output reaches find_dead_code",
	},
	"rust": {
		"find_dead_code":              "#5510: no chained proof that rust parse output reaches find_dead_code",
		"get_code_relationship_story": "#5510: no chained proof that rust parse output reaches get_code_relationship_story",
	},
	"scala": {
		"find_dead_code": "#5510: no chained proof that scala parse output reaches find_dead_code",
	},
	"swift": {
		"find_dead_code": "#5510: no chained proof that swift parse output reaches find_dead_code",
	},
	"terraform": {
		"content_relationships":  "#5510: no chained proof that terraform parse output reaches content_relationships",
		"trace_resource_to_code": "#5510: no chained proof that terraform parse output reaches trace_resource_to_code",
	},
	"terragrunt": {
		"content_relationships": "#5510: no chained proof that terragrunt parse output reaches content_relationships",
	},
	"typescript": {
		"find_dead_code": "#5510: no chained proof that typescript parse output reaches find_dead_code",
	},
	"typescriptjsx": {
		"find_dead_code":              "#5510: no chained proof that typescriptjsx parse output reaches find_dead_code",
		"get_code_relationship_story": "#5510: no chained proof that typescriptjsx parse output reaches get_code_relationship_story",
	},
	"argocd": {
		"trace_deployment_chain": "#5510: no chained proof that argocd parse output reaches trace_deployment_chain",
	},
	"atlantis": {
		"list_relationship_edges": "#5510: no chained proof that atlantis parse output reaches list_relationship_edges",
	},
	"flux": {
		"list_relationship_edges": "#5510: no chained proof that flux parse output reaches list_relationship_edges",
		"trace_deployment_chain":  "#5510: no chained proof that flux parse output reaches trace_deployment_chain",
	},
	"groovy": {
		"content_relationships": "#5510: no chained proof that groovy parse output reaches content_relationships",
	},
	"kustomize": {
		"content_relationships": "#5510: no chained proof that kustomize parse output reaches content_relationships",
	},
}

// exemptionFor reports the tracked exemption for one (language, surface) pair.
func exemptionFor(language, surface string) (string, bool) {
	bySurface, ok := languageParityQueryProofExemptions[language]
	if !ok {
		return "", false
	}
	reason, ok := bySurface[surface]
	return reason, ok
}

// sortedLanguages returns the registry's languages in a stable order so gate
// failures read the same way every run.
func sortedLanguages(registry map[string]map[string]languageQueryProof) []string {
	out := make([]string, 0, len(registry))
	for language := range registry {
		out = append(out, language)
	}
	sort.Strings(out)
	return out
}
