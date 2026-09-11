// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// writeLanguageQueryUnsupportedCapability writes the 501 unsupported_capability
// envelope this route uses for both of its unsupported cases: the
// profile-level gate ahead of any read, and the graph-only entity kinds
// (Repository, Directory, File -- the ones graphLabelToContentEntityType
// cannot map) when no graph is configured. Both share the same envelope shape
// (error code, capability, and current/required profile), but callers pass a
// distinct message (#5761 P2-5): the profile-gate call site's message is
// generically true at every profile, while under local_lightweight the
// catalog publishes symbol_graph.language_entities as "supported" (with no
// notes field to carry a caveat), so the graph-only-residue call site must
// name the actual cause and the offending entity kind rather than reusing the
// profile-gate wording -- otherwise the response would falsely suggest a
// profile change could fix the request.
func (h *Handler) writeLanguageQueryUnsupportedCapability(w http.ResponseWriter, r *http.Request, message string) {
	querycontract.WriteContractError(
		w,
		r,
		http.StatusNotImplemented,
		message,
		querycontract.ErrorCodeUnsupportedCapability,
		languageQueryCapability,
		h.profile(),
		querycontract.RequiredProfile(languageQueryCapability),
	)
}

// writeLanguageQueryResult writes the success response for one dispatch branch
// of handleLanguageQuery. Every branch returns the same body shape but differs
// in both which truth basis it can honestly claim and which reason describes
// how that basis was reached, so the envelope construction lives here rather
// than being repeated four times. The basis and reason stay per-branch
// arguments on purpose: keeping them at the call site is what lets a
// per-branch regression test mutate exactly one dispatch path's basis (or
// reason) and see only that branch's assertion fail. source_backend is
// derived from basis (sourceBackendForTruthBasis) rather than threaded
// separately, mirroring code_symbol.go's source_backend field.
//
// Callers pass req's fields individually because req is an anonymous struct
// declared inside handleLanguageQuery and has no nameable type.
func (h *Handler) writeLanguageQueryResult(
	w http.ResponseWriter,
	r *http.Request,
	language, entityType, query string,
	results []map[string]any,
	basis querycontract.TruthBasis,
	reason string,
) {
	body := languageQueryResponseBody(language, entityType, query, results)
	body["source_backend"] = SourceBackendForTruthBasis(basis)
	querycontract.WriteSuccess(w, r, http.StatusOK, body, querycontract.BuildTruthEnvelope(h.profile(), languageQueryCapability, basis, reason))
}
