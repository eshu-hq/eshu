// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"regexp"
	"strings"
)

var incidentIssueKeyRE = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)

type incidentReviewWorkItemInput struct {
	CommitSHA       string
	PullRequests    []incidentPullRequestEvidence
	WorkItems       []incidentWorkItemRecord
	ProjectMetadata []incidentWorkItemProjectMetadata
	StatusMetadata  []incidentWorkItemStatusMetadata
}

type incidentPullRequestEvidence struct {
	TriggerID          string
	Provider           string
	RepositoryFullName string
	CommitSHA          string
	Number             string
	URL                string
	Title              string
}

type incidentWorkItemRecord struct {
	FactID      string
	Provider    string
	WorkItemID  string
	WorkItemKey string
	Summary     string
	StatusID    string
	StatusName  string
	ProjectID   string
	ProjectKey  string
	SourceURL   string
}

type incidentWorkItemProjectMetadata struct {
	FactID          string
	Provider        string
	ProjectID       string
	ProjectKey      string
	VisibilityState string
}

type incidentWorkItemStatusMetadata struct {
	FactID            string
	Provider          string
	StatusID          string
	ProjectID         string
	StatusCategory    string
	StatusCategoryKey string
}

func buildIncidentReviewWorkItemEvidence(
	input incidentReviewWorkItemInput,
) []IncidentContextEvidenceEdge {
	if strings.TrimSpace(input.CommitSHA) == "" {
		return nil
	}
	edges := make([]IncidentContextEvidenceEdge, 0, 2)
	pullRequestEdge, selectedPullRequest := buildIncidentPullRequestEdge(input)
	if pullRequestEdge != nil {
		edges = append(edges, *pullRequestEdge)
	}
	if workItemEdge := buildIncidentWorkItemEdge(input, selectedPullRequest); workItemEdge != nil {
		edges = append(edges, *workItemEdge)
	}
	return edges
}

func buildIncidentPullRequestEdge(
	input incidentReviewWorkItemInput,
) (*IncidentContextEvidenceEdge, *incidentPullRequestEvidence) {
	candidates := incidentPullRequestCandidates(input.PullRequests, input.CommitSHA)
	if len(candidates) == 0 {
		return nil, nil
	}
	if len(candidates) > 1 {
		return &IncidentContextEvidenceEdge{
			Slot:        IncidentSlotPullRequest,
			TruthLabel:  IncidentTruthAmbiguous,
			Explanation: "multiple provider pull requests name the incident commit; no single pull request was selected",
			Candidates:  incidentPullRequestCandidateValues(candidates),
		}, nil
	}
	pullRequest := candidates[0]
	return &IncidentContextEvidenceEdge{
		Slot:        IncidentSlotPullRequest,
		TruthLabel:  IncidentTruthExact,
		Explanation: "GitHub pull request merge evidence matched the incident commit",
		Value: map[string]string{
			"provider":             pullRequest.Provider,
			"repository_full_name": pullRequest.RepositoryFullName,
			"commit_sha":           pullRequest.CommitSHA,
			"pull_request_number":  pullRequest.Number,
			"pull_request_url":     pullRequest.URL,
			"title":                pullRequest.Title,
		},
		Evidence: []IncidentContextEvidenceRef{
			incidentEvidenceRef("webhook.pull_request_merged", pullRequest.TriggerID, pullRequest.URL, pullRequest.Provider),
		},
	}, &pullRequest
}

func incidentPullRequestCandidates(
	pullRequests []incidentPullRequestEvidence,
	commitSHA string,
) []incidentPullRequestEvidence {
	candidates := make([]incidentPullRequestEvidence, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		if strings.TrimSpace(pullRequest.CommitSHA) != strings.TrimSpace(commitSHA) {
			continue
		}
		if strings.TrimSpace(pullRequest.URL) == "" {
			continue
		}
		candidates = append(candidates, pullRequest)
	}
	return candidates
}

func buildIncidentWorkItemEdge(
	input incidentReviewWorkItemInput,
	selectedPullRequest *incidentPullRequestEvidence,
) *IncidentContextEvidenceEdge {
	if selectedPullRequest == nil {
		return nil
	}
	records := incidentWorkItemsForIssueKeys(input.WorkItems, incidentIssueKeys(selectedPullRequest.Title))
	if len(records) == 0 {
		return nil
	}
	return incidentWorkItemRecordEdge(
		records,
		IncidentTruthDerived,
		"Jira work item key was derived from the provider-verified pull request title",
		input.ProjectMetadata,
		input.StatusMetadata,
	)
}

func incidentWorkItemRecordEdge(
	records []incidentWorkItemRecord,
	label IncidentTruthLabel,
	explanation string,
	projects []incidentWorkItemProjectMetadata,
	statuses []incidentWorkItemStatusMetadata,
) *IncidentContextEvidenceEdge {
	if len(records) > 1 {
		return &IncidentContextEvidenceEdge{
			Slot:        IncidentSlotWorkItem,
			TruthLabel:  IncidentTruthAmbiguous,
			Explanation: "multiple Jira work items matched the pull request issue key evidence",
			Candidates:  incidentWorkItemRecordCandidates(records),
		}
	}
	record := records[0]
	value := map[string]string{
		"provider":      record.Provider,
		"work_item_id":  record.WorkItemID,
		"work_item_key": record.WorkItemKey,
		"summary":       record.Summary,
		"status_name":   record.StatusName,
	}
	evidence := []IncidentContextEvidenceRef{
		incidentEvidenceRef("work_item.record", record.FactID, record.SourceURL, record.Provider),
	}
	if project := incidentWorkItemProjectMetadataForRecord(projects, record); project != nil {
		value["project_key"] = firstNonEmpty(record.ProjectKey, project.ProjectKey)
		value["project_visibility_state"] = project.VisibilityState
		evidence = append(evidence, incidentEvidenceRef("work_item.project_metadata", project.FactID, "", project.Provider))
	} else if strings.TrimSpace(record.ProjectKey) != "" {
		value["project_key"] = record.ProjectKey
	}
	if status := incidentWorkItemStatusMetadataForRecord(statuses, record); status != nil {
		value["status_category"] = status.StatusCategory
		value["status_category_key"] = status.StatusCategoryKey
		evidence = append(evidence, incidentEvidenceRef("work_item.status_metadata", status.FactID, "", status.Provider))
	}
	return &IncidentContextEvidenceEdge{
		Slot:        IncidentSlotWorkItem,
		TruthLabel:  label,
		Explanation: explanation,
		Value:       value,
		Evidence:    evidence,
	}
}

func incidentWorkItemProjectMetadataForRecord(
	projects []incidentWorkItemProjectMetadata,
	record incidentWorkItemRecord,
) *incidentWorkItemProjectMetadata {
	for i := range projects {
		project := projects[i]
		if strings.TrimSpace(project.ProjectID) != "" && strings.TrimSpace(project.ProjectID) == strings.TrimSpace(record.ProjectID) {
			return &project
		}
		if strings.TrimSpace(project.ProjectKey) != "" && strings.TrimSpace(project.ProjectKey) == strings.TrimSpace(record.ProjectKey) {
			return &project
		}
	}
	return nil
}

func incidentWorkItemStatusMetadataForRecord(
	statuses []incidentWorkItemStatusMetadata,
	record incidentWorkItemRecord,
) *incidentWorkItemStatusMetadata {
	for i := range statuses {
		status := statuses[i]
		if strings.TrimSpace(status.StatusID) != "" && strings.TrimSpace(status.StatusID) == strings.TrimSpace(record.StatusID) {
			if strings.TrimSpace(status.ProjectID) == "" || strings.TrimSpace(record.ProjectID) == "" || strings.TrimSpace(status.ProjectID) == strings.TrimSpace(record.ProjectID) {
				return &status
			}
		}
	}
	return nil
}

func incidentWorkItemsForIssueKeys(
	records []incidentWorkItemRecord,
	issueKeys []string,
) []incidentWorkItemRecord {
	if len(issueKeys) == 0 {
		return nil
	}
	keys := make(map[string]struct{}, len(issueKeys))
	for _, key := range issueKeys {
		keys[key] = struct{}{}
	}
	out := make([]incidentWorkItemRecord, 0, len(records))
	for _, record := range records {
		if _, ok := keys[strings.TrimSpace(record.WorkItemKey)]; ok {
			out = append(out, record)
		}
	}
	return out
}

func incidentIssueKeys(text string) []string {
	matches := incidentIssueKeyRE.FindAllString(strings.ToUpper(text), -1)
	out := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if _, ok := seen[match]; ok {
			continue
		}
		seen[match] = struct{}{}
		out = append(out, match)
	}
	return out
}

func incidentPullRequestCandidateValues(
	pullRequests []incidentPullRequestEvidence,
) []IncidentContextEvidenceCandidate {
	candidates := make([]IncidentContextEvidenceCandidate, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		candidates = append(candidates, IncidentContextEvidenceCandidate{
			ID:     firstNonEmpty(pullRequest.URL, pullRequest.TriggerID),
			Label:  firstNonEmpty(pullRequest.Number, pullRequest.Title),
			URL:    pullRequest.URL,
			Reason: "provider pull request merge matched the incident commit",
		})
	}
	return candidates
}

func incidentWorkItemRecordCandidates(
	records []incidentWorkItemRecord,
) []IncidentContextEvidenceCandidate {
	candidates := make([]IncidentContextEvidenceCandidate, 0, len(records))
	for _, record := range records {
		candidates = append(candidates, IncidentContextEvidenceCandidate{
			ID:     firstNonEmpty(record.WorkItemKey, record.WorkItemID, record.FactID),
			Label:  firstNonEmpty(record.WorkItemKey, record.Summary),
			URL:    record.SourceURL,
			Reason: "Jira work item key matched pull request title evidence",
		})
	}
	return candidates
}
