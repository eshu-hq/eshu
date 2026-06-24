// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"strings"
)

func (s PostgresIncidentContextStore) readIncidentReviewWorkItemEvidence(
	ctx context.Context,
	incident IncidentContextIncident,
	changes []IncidentContextChangeCandidate,
	evidence []IncidentContextEvidenceEdge,
) ([]IncidentContextEvidenceEdge, error) {
	commitSHA := incidentSelectedCommitSHA(evidence)
	if commitSHA == "" {
		return nil, nil
	}
	pullRequests, err := s.readIncidentPullRequestsByCommit(ctx, commitSHA)
	if err != nil {
		return nil, err
	}
	urls := incidentReviewWorkItemURLs(incident, changes, pullRequests)
	workItemLinks, err := s.readIncidentWorkItemLinksByURLs(ctx, urls)
	if err != nil {
		return nil, err
	}
	issueKeys := incidentReviewIssueKeys(pullRequests)
	workItems, err := s.readIncidentWorkItemsByKeys(ctx, issueKeys)
	if err != nil {
		return nil, err
	}
	projectMetadata, err := s.readIncidentWorkItemProjectMetadata(ctx, workItems)
	if err != nil {
		return nil, err
	}
	statusMetadata, err := s.readIncidentWorkItemStatusMetadata(ctx, workItems)
	if err != nil {
		return nil, err
	}
	return buildIncidentReviewWorkItemEvidence(incidentReviewWorkItemInput{
		CommitSHA:       commitSHA,
		IncidentURL:     incident.SourceURL,
		PullRequests:    pullRequests,
		WorkItemLinks:   workItemLinks,
		WorkItems:       workItems,
		ProjectMetadata: projectMetadata,
		StatusMetadata:  statusMetadata,
	}), nil
}

func incidentSelectedCommitSHA(edges []IncidentContextEvidenceEdge) string {
	for _, edge := range edges {
		if edge.Slot != IncidentSlotCommit {
			continue
		}
		switch edge.TruthLabel {
		case IncidentTruthExact, IncidentTruthDerived:
			return strings.TrimSpace(edge.Value["commit_sha"])
		default:
			return ""
		}
	}
	return ""
}

func (s PostgresIncidentContextStore) readIncidentPullRequestsByCommit(
	ctx context.Context,
	commitSHA string,
) ([]incidentPullRequestEvidence, error) {
	rows, err := s.DB.QueryContext(
		ctx,
		listIncidentPullRequestsByCommitQuery,
		commitSHA,
		incidentRuntimeEvidenceLimit+1,
	)
	if err != nil {
		return nil, fmt.Errorf("list incident pull requests by commit: %w", err)
	}
	defer func() { _ = rows.Close() }()
	pullRequests := make([]incidentPullRequestEvidence, 0)
	for rows.Next() {
		var pullRequest incidentPullRequestEvidence
		if err := rows.Scan(
			&pullRequest.TriggerID,
			&pullRequest.Provider,
			&pullRequest.RepositoryFullName,
			&pullRequest.CommitSHA,
			&pullRequest.Number,
			&pullRequest.URL,
			&pullRequest.Title,
		); err != nil {
			return nil, fmt.Errorf("scan incident pull request: %w", err)
		}
		pullRequests = append(pullRequests, pullRequest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan incident pull requests: %w", err)
	}
	return pullRequests, nil
}

func (s PostgresIncidentContextStore) readIncidentWorkItemLinksByURLs(
	ctx context.Context,
	urls []string,
) ([]incidentWorkItemExternalLink, error) {
	links := make([]incidentWorkItemExternalLink, 0)
	for _, linkURL := range urls {
		rows, err := s.queryIncidentContextRows(
			ctx,
			listIncidentWorkItemExternalLinksByURLQuery,
			linkURL,
			incidentRuntimeEvidenceLimit+1,
		)
		if err != nil {
			return nil, fmt.Errorf("list incident work item external links: %w", err)
		}
		for _, row := range rows {
			links = append(links, decodeIncidentWorkItemExternalLink(row))
		}
	}
	return links, nil
}

func (s PostgresIncidentContextStore) readIncidentWorkItemsByKeys(
	ctx context.Context,
	keys []string,
) ([]incidentWorkItemRecord, error) {
	records := make([]incidentWorkItemRecord, 0)
	for _, key := range keys {
		rows, err := s.queryIncidentContextRows(
			ctx,
			listIncidentWorkItemRecordsByKeyQuery,
			key,
			incidentRuntimeEvidenceLimit+1,
		)
		if err != nil {
			return nil, fmt.Errorf("list incident work item records: %w", err)
		}
		for _, row := range rows {
			records = append(records, decodeIncidentWorkItemRecord(row))
		}
	}
	return records, nil
}

func (s PostgresIncidentContextStore) readIncidentWorkItemProjectMetadata(
	ctx context.Context,
	records []incidentWorkItemRecord,
) ([]incidentWorkItemProjectMetadata, error) {
	projectIDs := incidentWorkItemProjectIDs(records)
	metadata := make([]incidentWorkItemProjectMetadata, 0, len(projectIDs))
	for _, projectID := range projectIDs {
		rows, err := s.queryIncidentContextRows(
			ctx,
			listIncidentWorkItemProjectMetadataByIDQuery,
			projectID,
			incidentRuntimeEvidenceLimit+1,
		)
		if err != nil {
			return nil, fmt.Errorf("list incident work item project metadata: %w", err)
		}
		for _, row := range rows {
			metadata = append(metadata, decodeIncidentWorkItemProjectMetadata(row))
		}
	}
	return metadata, nil
}

func (s PostgresIncidentContextStore) readIncidentWorkItemStatusMetadata(
	ctx context.Context,
	records []incidentWorkItemRecord,
) ([]incidentWorkItemStatusMetadata, error) {
	statusIDs := incidentWorkItemStatusIDs(records)
	metadata := make([]incidentWorkItemStatusMetadata, 0, len(statusIDs))
	for _, statusID := range statusIDs {
		rows, err := s.queryIncidentContextRows(
			ctx,
			listIncidentWorkItemStatusMetadataByIDQuery,
			statusID,
			incidentRuntimeEvidenceLimit+1,
		)
		if err != nil {
			return nil, fmt.Errorf("list incident work item status metadata: %w", err)
		}
		for _, row := range rows {
			metadata = append(metadata, decodeIncidentWorkItemStatusMetadata(row))
		}
	}
	return metadata, nil
}

func incidentWorkItemProjectIDs(records []incidentWorkItemRecord) []string {
	ids := make([]string, 0)
	for _, record := range records {
		ids = appendIncidentReviewDistinct(ids, record.ProjectID)
	}
	return ids
}

func incidentWorkItemStatusIDs(records []incidentWorkItemRecord) []string {
	ids := make([]string, 0)
	for _, record := range records {
		ids = appendIncidentReviewDistinct(ids, record.StatusID)
	}
	return ids
}

func incidentReviewWorkItemURLs(
	incident IncidentContextIncident,
	changes []IncidentContextChangeCandidate,
	pullRequests []incidentPullRequestEvidence,
) []string {
	urls := make([]string, 0, 1+len(changes)+len(pullRequests))
	urls = appendIncidentReviewURL(urls, incident.SourceURL)
	for _, pullRequest := range pullRequests {
		urls = appendIncidentReviewURL(urls, pullRequest.URL)
	}
	for _, change := range changes {
		for _, link := range change.Links {
			if incidentIsGitHubPullRequestURL(link.Href) {
				urls = appendIncidentReviewURL(urls, link.Href)
			}
		}
	}
	return urls
}

func incidentReviewIssueKeys(
	pullRequests []incidentPullRequestEvidence,
) []string {
	keys := make([]string, 0)
	for _, pullRequest := range pullRequests {
		for _, key := range incidentIssueKeys(pullRequest.Title) {
			keys = appendIncidentIssueKey(keys, key)
		}
	}
	return keys
}

func appendIncidentReviewURL(urls []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return urls
	}
	for _, existing := range urls {
		if existing == value {
			return urls
		}
	}
	return append(urls, value)
}

func appendIncidentIssueKey(keys []string, value string) []string {
	return appendIncidentReviewDistinct(keys, value)
}

func appendIncidentReviewDistinct(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func decodeIncidentWorkItemExternalLink(row incidentContextFactRow) incidentWorkItemExternalLink {
	return incidentWorkItemExternalLink{
		FactID:      row.FactID,
		Provider:    StringVal(row.Payload, "provider"),
		WorkItemID:  StringVal(row.Payload, "provider_work_item_id"),
		WorkItemKey: StringVal(row.Payload, "work_item_key"),
		URL:         StringVal(row.Payload, "url"),
		Title:       StringVal(row.Payload, "title"),
		AnchorClass: StringVal(row.Payload, "correlation_anchor_class"),
		SourceURL:   row.SourceURI,
	}
}

func decodeIncidentWorkItemRecord(row incidentContextFactRow) incidentWorkItemRecord {
	return incidentWorkItemRecord{
		FactID:      row.FactID,
		Provider:    StringVal(row.Payload, "provider"),
		WorkItemID:  StringVal(row.Payload, "provider_work_item_id"),
		WorkItemKey: StringVal(row.Payload, "work_item_key"),
		Summary:     StringVal(row.Payload, "summary"),
		StatusID:    StringVal(row.Payload, "status_id"),
		StatusName:  StringVal(row.Payload, "status_name"),
		ProjectID:   StringVal(row.Payload, "project_id"),
		ProjectKey:  StringVal(row.Payload, "project_key"),
		SourceURL:   StringVal(row.Payload, "source_url"),
	}
}

func decodeIncidentWorkItemProjectMetadata(row incidentContextFactRow) incidentWorkItemProjectMetadata {
	return incidentWorkItemProjectMetadata{
		FactID:          row.FactID,
		Provider:        StringVal(row.Payload, "provider"),
		ProjectID:       StringVal(row.Payload, "project_id"),
		ProjectKey:      StringVal(row.Payload, "project_key"),
		VisibilityState: StringVal(row.Payload, "visibility_state"),
	}
}

func decodeIncidentWorkItemStatusMetadata(row incidentContextFactRow) incidentWorkItemStatusMetadata {
	return incidentWorkItemStatusMetadata{
		FactID:            row.FactID,
		Provider:          StringVal(row.Payload, "provider"),
		StatusID:          StringVal(row.Payload, "status_id"),
		ProjectID:         StringVal(row.Payload, "project_id"),
		StatusCategory:    StringVal(row.Payload, "status_category"),
		StatusCategoryKey: StringVal(row.Payload, "status_category_key"),
	}
}
