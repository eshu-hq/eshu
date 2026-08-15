// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/query"
)

// VerifyOptions is the resolved input for one `eshu docs verify` run. The CLI
// wrapper builds it from cobra flags; nothing in this package reads a flag.
//
// Path is the file or directory to scan. Limit and MaxDocumentBytes bound that
// scan (document count and bytes read per document). Persist, Scope, and Repo
// select the Postgres persistence behavior. ImageTruth is the already-resolved
// container-image truth source ("local" or "api"); "auto" is resolved by the
// caller, because deciding it means reading flags and the process environment.
type VerifyOptions struct {
	Path             string
	Limit            int
	MaxDocumentBytes int
	Persist          bool
	Scope            string
	Repo             string
	ImageTruth       string
}

// Deps carries the seams a caller supplies so this package stays free of
// process wiring. OpenPersistence opens the Postgres persistence (the CLI
// wrapper owns the DSN lookup); a nil factory with VerifyOptions.Persist set is
// an error. CommandTruth reports the CLI command surface claims are checked
// against, which only the wrapper can walk. Now stamps committed generations.
// ContainerImageResolver is the already-selected image truth resolver, either
// LocalContainerImageResolver or APIContainerImageResolver.
type Deps struct {
	OpenPersistence        PersistenceFactory
	CommandTruth           func() []doctruth.CommandTruth
	Now                    func() time.Time
	ContainerImageResolver doctruth.ContainerImageResolver
}

// Result pairs the verification outcome with what persistence did with it.
type Result struct {
	Verification doctruth.VerificationResult
	Persistence  PersistenceSummary
}

// Envelope is the JSON shape `eshu docs verify --json` writes.
type Envelope struct {
	Data  EnvelopeData   `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// EnvelopeData is the payload half of Envelope.
type EnvelopeData struct {
	Findings        []doctruth.VerificationFinding        `json:"findings"`
	EvidencePackets []doctruth.VerificationEvidencePacket `json:"evidence_packets"`
	Summary         doctruth.VerificationSummary          `json:"summary"`
	Truncated       bool                                  `json:"truncated"`
	Persistence     PersistenceSummary                    `json:"persistence,omitempty"`
}

// EnvelopeError reports why the command is exiting non-zero. It describes the
// verification outcome only; the exit code itself is the CLI's contract.
type EnvelopeError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Verify inventories the documentation under opts.Path, checks each claim
// against Eshu truth sources, and optionally persists the findings.
//
// When persistence is enabled and the stored generation's freshness hint still
// matches the current inventory, Verify re-reads the stored findings instead of
// re-verifying, and Result.Persistence.Skipped reports that. Verify never
// writes to the filesystem and never exits the process; it returns the result
// and lets the caller choose output format and exit code.
func Verify(ctx context.Context, opts VerifyOptions, deps Deps) (Result, error) {
	inventory, err := InventoryDocuments(opts)
	if err != nil {
		return Result{}, err
	}
	persistence, closePersistence, summary, err := preparePersistence(ctx, opts, inventory, deps)
	if err != nil {
		return Result{}, err
	}
	if closePersistence != nil {
		defer func() { _ = closePersistence() }()
	}
	if summary.Skipped {
		stored, err := resultFromPersisted(ctx, persistence, summary)
		if err != nil {
			return Result{}, err
		}
		applyInventorySummary(&stored, inventory)
		return Result{Verification: stored, Persistence: summary}, nil
	}
	verifier := doctruth.NewVerifier(doctruth.VerifierOptions{
		Commands:               commandTruth(deps),
		HTTPEndpoints:          HTTPEndpointTruth(),
		EnvironmentVariables:   EnvironmentTruth(opts.Path),
		LocalPathResolver:      LocalPathResolver(opts.Path),
		ContainerImageResolver: deps.ContainerImageResolver,
		TerraformResolver:      TerraformAddressResolver(opts.Path),
		MaxDocuments:           opts.Limit,
		MaxDocumentBytes:       opts.MaxDocumentBytes,
		ScopeID:                summary.ScopeID,
		GenerationID:           summary.GenerationID,
	})
	result, err := verifier.Verify(ctx, inventory.Documents)
	if err != nil {
		return Result{}, err //nolint:wrapcheck // the CLI prints this verbatim; wrapping it would change the operator-visible message for an unchanged failure.
	}
	result.Truncated = result.Truncated || inventory.Truncated
	if persistence != nil {
		if err := commitResult(ctx, persistence, summary, result, deps.Now); err != nil {
			return Result{}, err
		}
		summary.Persisted = true
	}
	return Result{Verification: result, Persistence: summary}, nil
}

// commandTruth reads the CLI command surface from Deps, tolerating a caller
// that supplied none.
func commandTruth(deps Deps) []doctruth.CommandTruth {
	if deps.CommandTruth == nil {
		return nil
	}
	return deps.CommandTruth()
}

// NormalizeImageTruthMode trims and lowercases an image truth selector,
// defaulting an empty value to "auto". It does not validate the result --
// rejecting an unknown mode is the CLI's flag contract.
func NormalizeImageTruthMode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return "auto"
	}
	return value
}

// HTTPEndpointTruth reports the HTTP routes documentation may claim: every
// path/method pair in the query package's OpenAPI spec, plus the routes the
// services mount outside that spec (docs UI, health, MCP, admin, metrics).
func HTTPEndpointTruth() []doctruth.HTTPEndpointTruth {
	out := endpointTruthFromOpenAPI(query.OpenAPISpec())
	out = append(
		out,
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/api/v0/docs"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/api/v0/redoc"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/health"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/sse"},
		doctruth.HTTPEndpointTruth{Method: http.MethodPost, Path: "/mcp/message"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/healthz"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/readyz"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/admin/status"},
		doctruth.HTTPEndpointTruth{Method: http.MethodPost, Path: "/admin/replay"},
		doctruth.HTTPEndpointTruth{Method: http.MethodPost, Path: "/admin/refinalize"},
		doctruth.HTTPEndpointTruth{Method: http.MethodGet, Path: "/metrics"},
	)
	return out
}

// endpointTruthFromOpenAPI extracts routed method/path pairs from an OpenAPI
// document. A spec that will not parse yields no endpoints rather than an
// error: documentation claims then read as missing evidence, not contradicted.
func endpointTruthFromOpenAPI(spec string) []doctruth.HTTPEndpointTruth {
	var raw struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal([]byte(spec), &raw); err != nil {
		return nil
	}
	out := []doctruth.HTTPEndpointTruth{}
	for path, methods := range raw.Paths {
		for method := range methods {
			method = strings.ToUpper(method)
			switch method {
			case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
				out = append(out, doctruth.HTTPEndpointTruth{Method: method, Path: path})
			}
		}
	}
	return out
}

// NewEnvelope wraps a verification result in the command's JSON envelope. A
// non-nil err is reported as the envelope's error block; the caller still owns
// the exit code.
func NewEnvelope(result doctruth.VerificationResult, err error) Envelope {
	envelope := Envelope{
		Data: EnvelopeData{
			Findings:        result.Findings,
			EvidencePackets: result.EvidencePackets,
			Summary:         result.Summary,
			Truncated:       result.Truncated,
		},
		Truth: map[string]any{
			"capability": "documentation.verify",
			"basis":      "active documentation claim verification",
			"freshness":  map[string]any{"state": "fresh"},
		},
	}
	if err != nil {
		envelope.Error = &EnvelopeError{Code: "documentation_verification_failed", Message: err.Error()}
	}
	return envelope
}

// RenderText writes the human-readable summary line plus one line per
// non-valid finding. Valid findings are omitted so the default output stays
// scannable.
func RenderText(w io.Writer, result doctruth.VerificationResult) error {
	summary := result.Summary
	if _, err := fmt.Fprintf(
		w,
		"Docs verify: documents=%d claims=%d valid=%d contradicted=%d missing_evidence=%d unsupported=%d truncated=%t\n",
		summary.DocumentsScanned,
		summary.ClaimsChecked,
		summary.Valid,
		summary.Contradicted,
		summary.MissingEvidence,
		summary.UnsupportedClaimType,
		result.Truncated,
	); err != nil {
		return err //nolint:wrapcheck // a write failure on the CLI's own output stream is printed verbatim; wrapping it would change that operator-visible text.
	}
	for _, finding := range result.Findings {
		if finding.Status == doctruth.VerificationStatusValid {
			continue
		}
		if _, err := fmt.Fprintf(w, "- %s %s %s\n", finding.Status, finding.ClaimType, finding.NormalizedClaim); err != nil {
			return err //nolint:wrapcheck // same verbatim-output contract as above.
		}
	}
	return nil
}

// WriteJSON writes the envelope as indented JSON.
func WriteJSON(w io.Writer, envelope Envelope) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(envelope) //nolint:wrapcheck // same verbatim-output contract as RenderText: the CLI prints this error unchanged.
}
