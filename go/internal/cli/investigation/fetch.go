// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package investigation

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// Client is the transport the packet builders read the Eshu API through. It is
// declared here, where it is consumed, rather than in the CLI: go/cmd/eshu is
// package main, so its concrete *APIClient cannot be named from any importable
// package. The two methods are the only ones this family calls.
//
// Implementations must return the API's transport error unchanged, so that a
// status-carrying error still satisfies apierr.HTTPStatusError after unwrapping
// and RefusalFromFetchError can classify it.
type Client interface {
	GetEnvelope(path string, result any) error
	PostEnvelope(path string, body, result any) error
}

// SupplyChainExplainEnvelope decodes the canonical envelope returned by the
// supply-chain impact explain route.
type SupplyChainExplainEnvelope struct {
	Data  query.SupplyChainImpactExplanationResult `json:"data"`
	Truth *query.TruthEnvelope                     `json:"truth"`
	Error *query.ErrorEnvelope                     `json:"error"`
}

// AdmissionDecisionsEnvelope decodes the canonical envelope from the
// admission-decisions route.
type AdmissionDecisionsEnvelope struct {
	Data struct {
		Decisions []query.AdmissionDecisionResult `json:"decisions"`
	} `json:"data"`
	Truth *query.TruthEnvelope `json:"truth"`
	Error *query.ErrorEnvelope `json:"error"`
}

// DriftFindingsEnvelope decodes the canonical envelope from the cloud
// runtime-drift findings route.
type DriftFindingsEnvelope struct {
	Data struct {
		DriftFindings []query.CloudRuntimeDriftFindingView `json:"drift_findings"`
	} `json:"data"`
	Truth *query.TruthEnvelope `json:"truth"`
	Error *query.ErrorEnvelope `json:"error"`
}

// Deps carries the read fetches BuildPacket dispatches to, one per family. Tests
// substitute them to drive every refusal and success path without a live API;
// production callers pass DefaultDeps.
//
// A nil field for the family being built panics rather than failing softly,
// which is why DefaultDeps sets all three and TestDefaultDepsWiresEveryFamily
// guards it.
type Deps struct {
	FetchSupplyChainExplain func(client Client, filter query.SupplyChainImpactExplanationFilter) (SupplyChainExplainEnvelope, error)
	FetchAdmissionDecisions func(client Client, params url.Values) (AdmissionDecisionsEnvelope, error)
	FetchDriftFindings      func(client Client, body map[string]any) (DriftFindingsEnvelope, error)
}

// DefaultDeps returns the fetches that talk to a real Eshu API.
func DefaultDeps() Deps {
	return Deps{
		FetchSupplyChainExplain: FetchSupplyChainExplain,
		FetchAdmissionDecisions: FetchAdmissionDecisions,
		FetchDriftFindings:      FetchDriftFindings,
	}
}

// FetchSupplyChainExplain reads the supply-chain impact explanation for filter.
// Empty and whitespace-only filter fields are omitted from the query string so
// the API sees only the scope the operator actually named.
func FetchSupplyChainExplain(client Client, filter query.SupplyChainImpactExplanationFilter) (SupplyChainExplainEnvelope, error) {
	values := url.Values{}
	addQueryValue(values, "finding_id", filter.FindingID)
	addQueryValue(values, "advisory_id", filter.AdvisoryID)
	addQueryValue(values, "cve_id", filter.CVEID)
	addQueryValue(values, "package_id", filter.PackageID)
	addQueryValue(values, "repository_id", filter.RepositoryID)
	addQueryValue(values, "subject_digest", filter.SubjectDigest)
	path := "/api/v0/supply-chain/impact/explain?" + values.Encode()
	var envelope SupplyChainExplainEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		// Returned unwrapped on purpose: RefusalFromFetchError classifies by
		// status, and the operator reads this text verbatim when no refusal
		// applies. Adding a prefix here changes CLI output.
		return SupplyChainExplainEnvelope{}, err //nolint:wrapcheck
	}
	return envelope, nil
}

// FetchAdmissionDecisions reads bounded admission decisions for params.
func FetchAdmissionDecisions(client Client, params url.Values) (AdmissionDecisionsEnvelope, error) {
	path := "/api/v0/evidence/admission-decisions?" + params.Encode()
	var envelope AdmissionDecisionsEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return AdmissionDecisionsEnvelope{}, err //nolint:wrapcheck // see FetchSupplyChainExplain
	}
	return envelope, nil
}

// FetchDriftFindings reads cloud runtime-drift findings for body.
func FetchDriftFindings(client Client, body map[string]any) (DriftFindingsEnvelope, error) {
	var envelope DriftFindingsEnvelope
	if err := client.PostEnvelope("/api/v0/cloud/runtime-drift/findings", body, &envelope); err != nil {
		return DriftFindingsEnvelope{}, err //nolint:wrapcheck // see FetchSupplyChainExplain
	}
	return envelope, nil
}

func addQueryValue(values url.Values, key, value string) {
	if v := strings.TrimSpace(value); v != "" {
		values.Set(key, v)
	}
}
