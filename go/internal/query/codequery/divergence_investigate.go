// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/codedivergence"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DivergenceInvestigateRequest is the POST
// /api/v0/code/divergence/investigate body: one finding addressed by
// (repo_id, kind, fingerprint). Kind is required here (no all-families
// default): a fingerprint is only unique within its equality family. For
// wrapper_bypass the fingerprint carries the target entity id.
type DivergenceInvestigateRequest struct {
	RepoID       string `json:"repo_id"`
	Kind         string `json:"kind"`
	Fingerprint  string `json:"fingerprint"`
	IncludeTests bool   `json:"include_tests"`
}

func (r DivergenceInvestigateRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	switch r.Kind {
	case "exact", "renamed", "drifted", "wrapper_bypass",
		string(codedivergence.KindExact), string(codedivergence.KindRenamed),
		string(codedivergence.KindDrifted), string(codedivergence.KindWrapperBypass):
	default:
		return fmt.Errorf("kind must be one of: \"exact\", \"renamed\", \"drifted\", \"wrapper_bypass\" (qualified \"parallel_implementation.*\" spellings accepted)")
	}
	if strings.TrimSpace(r.Fingerprint) == "" {
		return fmt.Errorf("fingerprint is required")
	}
	return nil
}

// kind normalizes the short and qualified kind spellings to one family:
// a kind copied verbatim from a findings entry (qualified) addresses the
// same family as the short form.
func (r DivergenceInvestigateRequest) kind() codedivergence.Kind {
	switch r.Kind {
	case "renamed", string(codedivergence.KindRenamed):
		return codedivergence.KindRenamed
	case "drifted", string(codedivergence.KindDrifted):
		return codedivergence.KindDrifted
	case "wrapper_bypass", string(codedivergence.KindWrapperBypass):
		return codedivergence.KindWrapperBypass
	default:
		return codedivergence.KindExact
	}
}

func (h *CodeHandler) handleDivergenceInvestigate(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCodeDivergence,
		"POST /api/v0/code/divergence/investigate",
		divergenceFindingsCapability,
	)
	defer span.End()

	var req DivergenceInvestigateRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if querycontract.CapabilityUnsupported(h.profile(), divergenceFindingsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"code divergence findings require a supported query profile",
			ErrorCodeUnsupportedCapability,
			divergenceFindingsCapability,
			h.profile(),
			querycontract.RequiredProfile(divergenceFindingsCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, divergenceFindingsCapability) {
		return
	}
	if h == nil || h.Content == nil {
		WriteError(w, http.StatusServiceUnavailable, errDivergenceFindingsUnavailable.Error())
		return
	}
	reader, ok := h.Content.(divergenceStore)
	if !ok {
		WriteError(w, http.StatusServiceUnavailable, errDivergenceFindingsUnavailable.Error())
		return
	}
	if _, blocked := codeContentGrantScope(r.Context(), req.RepoID); blocked {
		WriteError(w, http.StatusNotFound, "divergence finding not found")
		return
	}
	var finding codedivergence.Finding
	sourceBackend := "postgres_content_store"
	if req.kind() == codedivergence.KindWrapperBypass {
		assembled, err := h.investigateWrapperTarget(r.Context(), req.RepoID, req.Fingerprint, req.IncludeTests)
		if err != nil {
			if errors.Is(err, errWrapperFindingNotFound) {
				WriteError(w, http.StatusNotFound, "divergence finding not found")
				return
			}
			if WriteGraphReadError(w, r, err, divergenceFindingsCapability) {
				return
			}
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		finding = assembled
		sourceBackend = "postgres_content_store+graph"
	} else if req.kind() == codedivergence.KindDrifted {
		rows, err := reader.DriftedFindingRows(r.Context(), req.RepoID, []string{req.Fingerprint})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		row, ok := rows[req.Fingerprint]
		if !ok {
			WriteError(w, http.StatusNotFound, "divergence finding not found")
			return
		}
		assembled, ok := codedivergence.AssembleDriftedFinding(req.RepoID, row, req.IncludeTests)
		if !ok {
			WriteError(w, http.StatusNotFound, "divergence finding not found")
			return
		}
		finding = assembled
	} else {
		membersByFP, err := reader.DivergenceMembers(r.Context(), req.RepoID, req.kind(), []string{req.Fingerprint})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, err.Error())
			return
		}
		members := membersByFP[req.Fingerprint]
		if len(members) == 0 {
			WriteError(w, http.StatusNotFound, "divergence finding not found")
			return
		}
		assembled, ok := codedivergence.AssembleFinding(req.RepoID, req.kind(), req.Fingerprint, members, req.IncludeTests)
		if !ok {
			WriteError(w, http.StatusNotFound, "divergence finding not found")
			return
		}
		finding = assembled
	}
	investigateBasis := "resolved from one fingerprint equality group with bounded follow-ups"
	if req.kind() == codedivergence.KindDrifted {
		investigateBasis = "resolved from one active drifted-pair fact with bounded follow-ups"
	}
	if req.kind() == codedivergence.KindWrapperBypass {
		investigateBasis = "resolved from one qualified wrapper target with bounded follow-ups"
	}
	steps, truncated := codedivergence.InvestigateSteps(req.RepoID, finding)
	stepResults := make([]map[string]any, 0, len(steps))
	for _, step := range steps {
		stepResults = append(stepResults, map[string]any{
			"tool":    step.Tool,
			"args":    step.Args,
			"purpose": step.Purpose,
		})
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		map[string]any{
			"repo_id":        req.RepoID,
			"finding":        divergenceFindingResult(finding),
			"next_steps":     stepResults,
			"truncated":      truncated,
			"suppressions":   finding.Suppressions,
			"source_backend": sourceBackend,
		},
		BuildTruthEnvelope(h.profile(), divergenceFindingsCapability, TruthBasisContentIndex, investigateBasis),
	)
}
