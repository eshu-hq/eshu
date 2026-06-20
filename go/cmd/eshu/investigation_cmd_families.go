package main

import (
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// admissionDecisionsEnvelope decodes the canonical envelope from the
// admission-decisions route.
type admissionDecisionsEnvelope struct {
	Data struct {
		Decisions []query.AdmissionDecisionResult `json:"decisions"`
	} `json:"data"`
	Truth *query.TruthEnvelope `json:"truth"`
	Error *query.ErrorEnvelope `json:"error"`
}

// driftFindingsEnvelope decodes the canonical envelope from the cloud
// runtime-drift findings route.
type driftFindingsEnvelope struct {
	Data struct {
		DriftFindings []query.CloudRuntimeDriftFindingView `json:"drift_findings"`
	} `json:"data"`
	Truth *query.TruthEnvelope `json:"truth"`
	Error *query.ErrorEnvelope `json:"error"`
}

// buildDeployableUnitExportPacket reads bounded deployable-unit admission
// decisions for the requested scope and maps them into the v2 packet. A missing
// scope or a not-found/backend error yields a refusal packet.
func buildDeployableUnitExportPacket(cmd *cobra.Command, subject map[string]string) (query.InvestigationEvidencePacket, error) {
	params, ok := deployableUnitParams(subject)
	if !ok {
		return refusalPacket(query.InvestigationFamilyDeployableUnit, subject, query.PacketRefusalScopeNotFound)
	}
	client := apiClientFromCmd(cmd)
	envelope, err := investigationExportDepsValue.FetchAdmissionDecisions(client, params)
	if err != nil {
		if refusal, refused := refusalFromFetchError(err); refused {
			return refusalPacket(query.InvestigationFamilyDeployableUnit, subject, refusal)
		}
		return query.InvestigationEvidencePacket{}, err
	}
	if refusal, refused, err := refusalFromEnvelopeError(envelope.Error); err != nil {
		return query.InvestigationEvidencePacket{}, err
	} else if refused {
		return refusalPacket(query.InvestigationFamilyDeployableUnit, subject, refusal)
	}
	return query.BuildDeployableUnitPacket(envelope.Data.Decisions, subject, envelope.Truth, packetBoundsFromCmd(cmd))
}

// buildDriftExportPacket reads bounded cloud runtime drift findings for the
// requested scope and maps them into the v2 packet.
func buildDriftExportPacket(cmd *cobra.Command, subject map[string]string) (query.InvestigationEvidencePacket, error) {
	body, ok := driftRequestBody(subject)
	if !ok {
		return refusalPacket(query.InvestigationFamilyDrift, subject, query.PacketRefusalScopeNotFound)
	}
	client := apiClientFromCmd(cmd)
	envelope, err := investigationExportDepsValue.FetchDriftFindings(client, body)
	if err != nil {
		if refusal, refused := refusalFromFetchError(err); refused {
			return refusalPacket(query.InvestigationFamilyDrift, subject, refusal)
		}
		return query.InvestigationEvidencePacket{}, err
	}
	if refusal, refused, err := refusalFromEnvelopeError(envelope.Error); err != nil {
		return query.InvestigationEvidencePacket{}, err
	} else if refused {
		return refusalPacket(query.InvestigationFamilyDrift, subject, refusal)
	}
	return query.BuildDriftPacket(envelope.Data.DriftFindings, subject, envelope.Truth, packetBoundsFromCmd(cmd))
}

// deployableUnitParams builds the admission-decisions query from the subject. It
// requires scope_id and generation_id because the admission-decision store keys
// deployable-unit correlation rows by both values. Reducer decisions for this
// domain are persisted under deployable_unit_correlation and use repository
// anchors, so workload/service subjects stay packet context instead of becoming
// unsupported exact anchor filters.
func deployableUnitParams(subject map[string]string) (url.Values, bool) {
	scopeID := strings.TrimSpace(subject["scope_id"])
	generationID := strings.TrimSpace(subject["generation_id"])
	if scopeID == "" || generationID == "" {
		return nil, false
	}
	params := url.Values{}
	params.Set("domain", "deployable_unit_correlation")
	params.Set("scope_id", scopeID)
	params.Set("generation_id", generationID)
	if repositoryID := firstSubjectValue(subject, "repository_id", "repo_id"); repositoryID != "" {
		params.Set("anchor_kind", "repository")
		params.Set("anchor_id", repositoryID)
	}
	return params, true
}

// driftRequestBody builds the runtime-drift request from the subject. It requires
// a scope_id (or a provider account/project/subscription alias).
func driftRequestBody(subject map[string]string) (map[string]any, bool) {
	scopeID := firstSubjectValue(subject, "scope_id", "account_id", "project_id", "subscription_id")
	if scopeID == "" {
		return nil, false
	}
	body := map[string]any{"scope_id": scopeID}
	if provider := strings.TrimSpace(subject["provider"]); provider != "" {
		body["provider"] = provider
	}
	if uid := strings.TrimSpace(subject["cloud_resource_uid"]); uid != "" {
		body["cloud_resource_uid"] = uid
	}
	return body, true
}

func fetchAdmissionDecisions(client *APIClient, params url.Values) (admissionDecisionsEnvelope, error) {
	path := "/api/v0/evidence/admission-decisions?" + params.Encode()
	var envelope admissionDecisionsEnvelope
	if err := client.GetEnvelope(path, &envelope); err != nil {
		return admissionDecisionsEnvelope{}, err
	}
	return envelope, nil
}

func fetchDriftFindings(client *APIClient, body map[string]any) (driftFindingsEnvelope, error) {
	var envelope driftFindingsEnvelope
	if err := client.PostEnvelope("/api/v0/cloud/runtime-drift/findings", body, &envelope); err != nil {
		return driftFindingsEnvelope{}, err
	}
	return envelope, nil
}

func firstSubjectValue(subject map[string]string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(subject[key]); v != "" {
			return v
		}
	}
	return ""
}
