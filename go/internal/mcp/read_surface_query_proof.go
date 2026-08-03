// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import "sort"

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
	// The six gaps this gate found on the day it was written. Each row cites a
	// queryable-truth surface, declares no partial feature, and has no chained
	// proof -- so today nothing verifies that THIS language's parser output
	// actually reaches that surface. The existence gate passed them all,
	// because the tools they cite are real and registered.
	//
	// They are recorded rather than fixed here for the reason #5510 gives:
	// writing six chained fixture->parser->materialization->query proofs is the
	// work each row deserves, and doing it inside the gate's own change would
	// bury six substantive proofs in a gate PR. Tracked by #5510.
	"argocd": {
		"trace_deployment_chain": "#5510: no chained proof that ArgoCD Application parse output reaches trace_deployment_chain",
	},
	"atlantis": {
		"list_relationship_edges": "#5510: no chained proof that Atlantis project/workflow edges reach list_relationship_edges",
	},
	"flux": {
		"list_relationship_edges": "#5510: no chained proof that Flux Kustomization/HelmRelease edges reach list_relationship_edges",
		"trace_deployment_chain":  "#5510: no chained proof that Flux parse output reaches trace_deployment_chain",
	},
	"groovy": {
		"content_relationships": "#5510: no chained proof that Groovy/Jenkinsfile parse output reaches content_relationships",
	},
	"kustomize": {
		"content_relationships": "#5510: no chained proof that Kustomize overlay parse output reaches content_relationships",
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
