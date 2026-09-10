// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/imports"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const (
	importDependencyCapability = "symbol_graph.import_dependencies"
)

var errImportDependencyUnavailable = errors.New("import dependency graph is unavailable")

// The codemodel.ImportDependencyRequest type, its methods, the query-type helpers,
// and the page limit bounds split to
// codemodel/code_import_dependencies_queries.go (#6060 lane A L1); all
// seven builders take the request there. Root's family_code_shim.go
// aliases the type back so the staying handler, executors, and tests keep
// their names.

func (h *CodeHandler) handleImportDependencyInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryImportDependencyInvestigation,
		"POST /api/v0/code/imports/investigate",
		importDependencyCapability,
	)
	defer span.End()

	var req codemodel.ImportDependencyRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), importDependencyCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"import dependency investigation requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			importDependencyCapability,
			h.profile(),
			querycontract.RequiredProfile(importDependencyCapability),
		)
		return
	}
	if err := req.Validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, importDependencyCapability) {
		return
	}
	span.SetAttributes(attribute.String("eshu.import_dependencies.query_type", req.EffectiveQueryType()))

	// repo_id is optional here -- source_file, target_file, source_module or
	// target_module each satisfy req.Validate() on their own -- so a scoped
	// caller who omits it reaches every builder corpus-wide. codeContentGrantScope
	// is the front gate for the grantless case; req.access is what the builders
	// bind for everyone else.
	if _, blocked := codeContentGrantScope(r.Context(), req.RepoID); blocked {
		// This page is produced without reading anything, so it must not
		// describe itself as an authoritative graph read. It reports the same
		// basis the language-query empty page reports, and codemodel.ImportDependencyResponse's
		// blanket "graph" source_backend is replaced with the vocabulary
		// sourceBackendForTruthBasis derives from that basis.
		//
		// Both values name the absence directly since #6544:
		// TruthBasisNoBackendRead and its wire spelling
		// noBackendReadSourceBackend. Before it, the pair borrowed
		// content_index and the "unavailable" sentinel because neither
		// vocabulary had a member for a page produced without a read, so the
		// envelope claimed a content-store read that never happened. The
		// reason string still carries the detail, and it denies every backend
		// rather than only the graph: "no graph read was issued" would invite
		// the reader to infer a content read that also never happened. The
		// sentence is reasonEmptyGrantNoBackendRead, the same constant
		// language-query's empty page uses -- shared rather than duplicated so
		// the two pages cannot be reworded apart. Each route's test still pins
		// the text as a literal, which is what catches a rewording of the
		// constant itself.
		emptyPage := codemodel.ImportDependencyResponse(req, nil)
		emptyPage["source_backend"] = querycontract.NoBackendReadSourceBackend
		WriteSuccess(
			w,
			r,
			http.StatusOK,
			emptyPage,
			BuildTruthEnvelope(h.profile(), importDependencyCapability, TruthBasisNoBackendRead, querycontract.ReasonEmptyGrantNoBackendRead),
		)
		return
	}
	req.Access = codeGrantAccessFilter(r.Context())

	data, err := h.importDependencyData(r.Context(), req)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, errImportDependencyUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if errors.Is(err, codemodel.ErrImportDependencyScopeTooBroad) {
			span.SetAttributes(attribute.Bool("eshu.import_dependencies.scan_overflow", true))
			WriteError(
				w,
				http.StatusUnprocessableEntity,
				fmt.Sprintf("%v; narrow the repository, file, or module scope", err),
			)
			return
		}
		if WriteGraphReadError(w, r, err, importDependencyCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	span.SetAttributes(
		attribute.Int("eshu.import_dependencies.result_count", IntVal(data, "count")),
		attribute.Bool("eshu.import_dependencies.truncated", BoolVal(data, "truncated")),
		attribute.Bool("eshu.import_dependencies.scan_overflow", false),
	)
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), importDependencyCapability, TruthBasisAuthoritativeGraph, "resolved from bounded graph import dependency lookup"),
	)
}

// The request validation and accessors moved to
// codemodel/code_import_dependencies_queries.go with the request type
// (#6060 lane A L1).

func (h *CodeHandler) importDependencyData(ctx context.Context, req codemodel.ImportDependencyRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errImportDependencyUnavailable
	}
	rows, err := h.importDependencyRows(ctx, req)
	if err != nil {
		return nil, err
	}
	return codemodel.ImportDependencyResponse(req, rows), nil
}

// ImportDependencyParams builds the parameter map the import-dependency
// Cypher builders send: paging, repository, language, grant, and module
// file bindings from one request. The live grant proof in the parent query
// package calls it so the statement under proof is the one the handler
// sends rather than a rewrite of it.
func ImportDependencyParams(req codemodel.ImportDependencyRequest) map[string]any {
	return imports.Params(req)
}
