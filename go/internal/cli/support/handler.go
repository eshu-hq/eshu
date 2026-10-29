// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package support

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/answerquality"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/reportbundle"
	"github.com/eshu-hq/eshu/go/internal/serviceintel"
)

// Body caps bound each endpoint's request. Render, compose, validate, and
// score take small captured artifacts (the 1 MiB SAML precedent); capture
// takes up to 8 MiB because private-triage mode attaches raw fact payloads
// and citation excerpts. Oversize bodies fail closed with 413 before any
// decode work.
const (
	maxRenderBodyBytes   = 1 << 20
	maxComposeBodyBytes  = 1 << 20
	maxCaptureBodyBytes  = 8 << 20
	maxValidateBodyBytes = 1 << 20
	maxScoreBodyBytes    = 1 << 20
)

// Handlers serves the CLI-support endpoints. It carries no dependencies:
// every route is a pure function of its request body.
type Handlers struct{}

// Mount registers the five CLI-support routes.
func (h *Handlers) Mount(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v0/investigation/packet/render", h.renderPacket)
	mux.HandleFunc("POST /api/v0/service/report/compose", h.composeReport)
	mux.HandleFunc("POST /api/v0/report/bundle/capture", h.captureBundle)
	mux.HandleFunc("POST /api/v0/report/bundle/validate", h.validateBundle)
	mux.HandleFunc("POST /api/v0/answer-quality/score", h.scoreEvidence)
}

// decodeBody bounds r.Body at maxBytes and decodes exactly one JSON object
// into dst. Unknown fields are rejected: a misspelled field such as
// "dossire" would otherwise be ignored while the required-field check
// below passes on the remainder. Malformed JSON and an empty body fail with
// 400; an over-cap body fails with 413. It reports whether the caller may
// proceed.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			query.WriteErrorEnvelope(w, r, http.StatusRequestEntityTooLarge, &query.ErrorEnvelope{
				Code:    query.ErrorCodeInvalidArgument,
				Message: fmt.Sprintf("request body exceeds %d bytes", maxBytes),
			})
			return false
		}
		msg := fmt.Sprintf("decode request body: %v", err)
		if errors.Is(err, io.EOF) {
			msg = "request body is required"
		}
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: msg,
		})
		return false
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: "request body must hold one JSON object",
		})
		return false
	}
	return true
}

// refuseMissing writes the 400 for a missing required request field.
func refuseMissing(w http.ResponseWriter, r *http.Request, message string) {
	query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
		Code:    query.ErrorCodeInvalidArgument,
		Message: message,
	})
}

// RenderRequest carries the already-fetched packet and the wanted format.
// Format defaults to JSON, mirroring ParseInvestigationPacketFormat.
type RenderRequest struct {
	Packet query.InvestigationEvidencePacket `json:"packet"`
	Format string                            `json:"format,omitempty"`
}

// renderPacket renders a client-supplied packet. Unlike the query routes it
// returns raw bytes with the format content type rather than an envelope:
// the caller asked for a rendering, not a query answer.
func (h *Handlers) renderPacket(w http.ResponseWriter, r *http.Request) {
	var req RenderRequest
	if !decodeBody(w, r, &req, maxRenderBodyBytes) {
		return
	}
	// PacketID is the deterministic identity every built packet carries; an
	// empty one means no packet was supplied, and the renderer would
	// otherwise answer 200 with a rendering of nothing.
	if req.Packet.PacketID == "" {
		refuseMissing(w, r, "packet.packet_id is required")
		return
	}
	format, err := query.ParseInvestigationPacketFormat(req.Format)
	if err != nil {
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: err.Error(),
		})
		return
	}
	raw, err := query.RenderInvestigationPacket(req.Packet, format)
	if err != nil {
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: err.Error(),
		})
		return
	}
	switch format {
	case query.InvestigationPacketFormatMarkdown:
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	case query.InvestigationPacketFormatHTML:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	default:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// ComposeRequest carries the captured dossier, its truth envelope, and the
// optional captured supply-chain inventory plus its truth — exactly the
// bytes the service-report command reads from its input files.
type ComposeRequest struct {
	Dossier          map[string]any       `json:"dossier"`
	Truth            *query.TruthEnvelope `json:"truth"`
	SupplyChain      map[string]any       `json:"supply_chain,omitempty"`
	SupplyChainTruth *query.TruthEnvelope `json:"supply_chain_truth,omitempty"`
}

// composeReport composes a service intelligence report from captured inputs,
// running the same FromServiceStory/FromSupplyChainInventory/Compose chain
// the offline command runs.
func (h *Handlers) composeReport(w http.ResponseWriter, r *http.Request) {
	var req ComposeRequest
	if !decodeBody(w, r, &req, maxComposeBodyBytes) {
		return
	}
	// FromServiceStory turns a missing dossier into an empty report that
	// always composes, so the endpoint answers 200 for no input without
	// this check.
	if len(req.Dossier) == 0 {
		refuseMissing(w, r, "dossier is required")
		return
	}
	input := serviceintel.FromServiceStory(req.Dossier, req.Truth)
	if req.SupplyChain != nil {
		input.Sections = append(input.Sections,
			serviceintel.FromSupplyChainInventory(req.SupplyChain, input.Subject, req.SupplyChainTruth))
	}
	report := serviceintel.Compose(input)
	query.WriteSuccess(w, r, http.StatusOK, report, report.Truth)
}

// captureBundle runs reportbundle.Capture over client-supplied inputs. The
// target is screened for embedded credentials with the same predicate the
// offline command runs, before Capture sees it. Every Capture error maps to
// 422: every failure reachable over the wire is an input refusal
// (unredactable input, tripped share-safe gate), because the envelope
// already survived a JSON round trip on the way in, so in-memory
// marshal/digest failures are unreachable. The message carries the
// diagnostic; the redaction rules keep secrets out of it.
func (h *Handlers) captureBundle(w http.ResponseWriter, r *http.Request) {
	var input reportbundle.CaptureInput
	if !decodeBody(w, r, &input, maxCaptureBodyBytes) {
		return
	}
	// SplitTargetQuery accepts an empty target and Capture answers 200 with
	// an empty bundle, so the fragment's required surface/target/envelope
	// need an explicit check. A real query envelope always carries data or
	// an error.
	if input.Surface == "" || input.Target == "" ||
		(input.Envelope.Data == nil && input.Envelope.Error == nil) {
		refuseMissing(w, r, "surface, target, and envelope are required")
		return
	}
	if err := reportbundle.CheckTargetCredentials(input.Target); err != nil {
		query.WriteErrorEnvelope(w, r, http.StatusUnprocessableEntity, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: err.Error(),
		})
		return
	}
	bundle, err := reportbundle.Capture(input)
	if err != nil {
		query.WriteErrorEnvelope(w, r, http.StatusUnprocessableEntity, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: err.Error(),
		})
		return
	}
	query.WriteSuccess(w, r, http.StatusOK, bundle, bundle.Response.Truth)
}

// ValidateRequest carries the bundle file bytes plus the --require-public
// posture.
type ValidateRequest struct {
	Bundle        reportbundle.Bundle `json:"bundle"`
	RequirePublic bool                `json:"require_public,omitempty"`
}

// ValidateVerdict is the wire verdict for bundle validation: 200 always
// carries it, valid or not, because validation is a check, not a fetch.
type ValidateVerdict struct {
	Valid   bool   `json:"valid"`
	Message string `json:"message,omitempty"`
}

// validateBundle validates bundle bytes. An invalid bundle still answers
// 200 with valid:false — the verdict is the product, and the CLI maps it to
// its exit code.
func (h *Handlers) validateBundle(w http.ResponseWriter, r *http.Request) {
	var req ValidateRequest
	if !decodeBody(w, r, &req, maxValidateBodyBytes) {
		return
	}
	err := reportbundle.Validate(req.Bundle, reportbundle.ValidateOptions{
		RequirePublic: req.RequirePublic,
	})
	verdict := ValidateVerdict{Valid: err == nil}
	if err != nil {
		verdict.Message = err.Error()
	}
	query.WriteSuccess(w, r, http.StatusOK, verdict, nil)
}

// scoreEvidence scores captured answer evidence, running the same
// ParseEvidence/Score chain the offline command runs.
func (h *Handlers) scoreEvidence(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxScoreBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			query.WriteErrorEnvelope(w, r, http.StatusRequestEntityTooLarge, &query.ErrorEnvelope{
				Code:    query.ErrorCodeInvalidArgument,
				Message: fmt.Sprintf("request body exceeds %d bytes", maxScoreBodyBytes),
			})
			return
		}
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: fmt.Sprintf("read request body: %v", err),
		})
		return
	}
	// Strict structural check before ParseEvidence: it unmarshals with the
	// default decoder, so unknown fields and trailing values would
	// otherwise be silently ignored while scoring proceeds on the rest.
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	var shape answerquality.Evidence
	if err := strict.Decode(&shape); err != nil {
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: fmt.Sprintf("decode answer-quality evidence: %v", err),
		})
		return
	}
	if err := strict.Decode(&struct{}{}); err != io.EOF {
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: "request body must hold one JSON object",
		})
		return
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		refuseMissing(w, r, "request body is required")
		return
	}
	evidence, err := answerquality.ParseEvidence(raw)
	if err != nil {
		query.WriteErrorEnvelope(w, r, http.StatusBadRequest, &query.ErrorEnvelope{
			Code:    query.ErrorCodeInvalidArgument,
			Message: err.Error(),
		})
		return
	}
	query.WriteSuccess(w, r, http.StatusOK, answerquality.Score(evidence), nil)
}
