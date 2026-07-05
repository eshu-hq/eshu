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
			link, ok := decodeIncidentWorkItemExternalLink(row)
			if !ok {
				continue
			}
			links = append(links, link)
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
			record, ok := decodeIncidentWorkItemRecord(row)
			if !ok {
				continue
			}
			records = append(records, record)
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
			decoded, ok := decodeIncidentWorkItemProjectMetadata(row)
			if !ok {
				continue
			}
			metadata = append(metadata, decoded)
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
			decoded, ok := decodeIncidentWorkItemStatusMetadata(row)
			if !ok {
				continue
			}
			metadata = append(metadata, decoded)
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

// decodeIncidentWorkItemExternalLink decodes one work_item.external_link fact
// row through the typed sdk/go/factschema/workitem/v1 seam. ok is false when
// the fact failed decode (only "provider" is required for this kind, so this
// rarely fires; see workitem/v1/README.md) — the caller drops the fact from
// the incident-review evidence list rather than emitting an empty-identity
// row.
//
// URL is read from the deprecated raw "url" payload key (pre-existing drift:
// the Jira collector never emits an "url" key, only "url_fingerprint" and
// "url_present" — see the Wave 4d out-of-scope note), so it derefs the typed
// struct's field of the same name and is byte-identical to the pre-typing
// StringVal("") result: always empty.
func decodeIncidentWorkItemExternalLink(row incidentContextFactRow) (incidentWorkItemExternalLink, bool) {
	link, err := decodeWorkItemExternalLink(row.FactID, row.SchemaVersion, row.Payload)
	if err != nil {
		logWorkItemEvidenceDecodeDrop(err)
		return incidentWorkItemExternalLink{}, false
	}
	return incidentWorkItemExternalLink{
		FactID:      row.FactID,
		Provider:    link.Provider,
		WorkItemID:  workItemDerefString(link.ProviderWorkItemID),
		WorkItemKey: workItemDerefString(link.WorkItemKey),
		URL:         workItemDerefString(link.URL),
		Title:       workItemDerefString(link.Title),
		AnchorClass: workItemDerefString(link.AnchorClass),
		SourceURL:   row.SourceURI,
	}, true
}

// decodeIncidentWorkItemRecord decodes one work_item.record fact row through
// the typed seam. ok is false when the fact is missing its required identity
// anchor (provider_work_item_id or work_item_key) — the caller drops it
// rather than emitting an empty-identity row.
func decodeIncidentWorkItemRecord(row incidentContextFactRow) (incidentWorkItemRecord, bool) {
	record, err := decodeWorkItemRecord(row.FactID, row.SchemaVersion, row.Payload)
	if err != nil {
		logWorkItemEvidenceDecodeDrop(err)
		return incidentWorkItemRecord{}, false
	}
	return incidentWorkItemRecord{
		FactID:      row.FactID,
		Provider:    record.Provider,
		WorkItemID:  record.ProviderWorkItemID,
		WorkItemKey: record.WorkItemKey,
		Summary:     workItemDerefString(record.Summary),
		StatusID:    workItemDerefString(record.StatusID),
		StatusName:  workItemDerefString(record.StatusName),
		ProjectID:   workItemDerefString(record.ProjectID),
		ProjectKey:  workItemDerefString(record.ProjectKey),
		SourceURL:   workItemDerefString(record.SourceURL),
	}, true
}

// decodeIncidentWorkItemProjectMetadata decodes one
// work_item.project_metadata fact row through the typed seam. ok is false
// when the fact failed decode (only "provider" is required for this kind).
func decodeIncidentWorkItemProjectMetadata(row incidentContextFactRow) (incidentWorkItemProjectMetadata, bool) {
	metadata, err := decodeWorkItemProjectMetadata(row.FactID, row.SchemaVersion, row.Payload)
	if err != nil {
		logWorkItemEvidenceDecodeDrop(err)
		return incidentWorkItemProjectMetadata{}, false
	}
	return incidentWorkItemProjectMetadata{
		FactID:          row.FactID,
		Provider:        metadata.Provider,
		ProjectID:       workItemDerefString(metadata.ProjectID),
		ProjectKey:      workItemDerefString(metadata.ProjectKey),
		VisibilityState: workItemDerefString(metadata.VisibilityState),
	}, true
}

// decodeIncidentWorkItemStatusMetadata decodes one work_item.status_metadata
// fact row through the typed seam. ok is false when the fact is missing its
// required status_id anchor.
func decodeIncidentWorkItemStatusMetadata(row incidentContextFactRow) (incidentWorkItemStatusMetadata, bool) {
	metadata, err := decodeWorkItemStatusMetadata(row.FactID, row.SchemaVersion, row.Payload)
	if err != nil {
		logWorkItemEvidenceDecodeDrop(err)
		return incidentWorkItemStatusMetadata{}, false
	}
	return incidentWorkItemStatusMetadata{
		FactID:            row.FactID,
		Provider:          metadata.Provider,
		StatusID:          metadata.StatusID,
		ProjectID:         workItemDerefString(metadata.ProjectID),
		StatusCategory:    workItemDerefString(metadata.StatusCategory),
		StatusCategoryKey: workItemDerefString(metadata.StatusCategoryKey),
	}, true
}
