// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package macie

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Macie metadata facts for one claimed account and region.
// It never reads sensitive-data findings, never persists custom data identifier
// regular-expression bodies, allow-list contents, findings filter criteria, or
// classification-job bucket-criteria expressions, and never mutates Macie
// resources.
type Scanner struct {
	Client Client
}

// Scan observes the Macie session status, member accounts, classification job
// metadata, allow list identities, custom data identifier identities, findings
// filter identities, and aggregate finding counts by severity through the
// configured client.
//
// When Macie is not enabled for the account the scanner emits a single disabled
// session resource and makes no further reads, so a disabled account is cheap
// and unambiguous rather than silently empty.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("macie scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceMacie:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceMacie
	default:
		return nil, fmt.Errorf("macie scanner received service_kind %q", boundary.ServiceKind)
	}

	session, err := s.Client.Session(ctx)
	if err != nil {
		return nil, fmt.Errorf("get Macie session: %w", err)
	}

	administratorID, err := s.Client.AdministratorAccountID(ctx)
	if err != nil {
		return nil, fmt.Errorf("get Macie administrator account: %w", err)
	}

	var envelopes []facts.Envelope
	sessionEnvelope, err := awscloud.NewResourceEnvelope(sessionObservation(boundary, session, administratorID))
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, sessionEnvelope)

	if !session.Enabled {
		// Macie is off for this account. There is no session to enumerate
		// members, jobs, or findings against, so stop after the disabled record.
		return envelopes, nil
	}

	severityCounts, err := s.Client.FindingCountsBySeverity(ctx)
	if err != nil {
		return nil, fmt.Errorf("get Macie finding counts by severity: %w", err)
	}
	if len(severityCounts) > 0 {
		// Re-emit the session resource with the aggregate counts attached so the
		// counts live on the account resource rather than fanning out into
		// synthetic finding nodes. The first session envelope above guarantees a
		// record even when Macie reports no findings.
		withCounts, err := awscloud.NewResourceEnvelope(sessionObservationWithFindingCounts(boundary, session, administratorID, severityCounts))
		if err != nil {
			return nil, err
		}
		envelopes[0] = withCounts
	}

	members, err := s.Client.ListMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Macie members: %w", err)
	}
	for _, member := range members {
		resource, err := awscloud.NewResourceEnvelope(memberObservation(boundary, member))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		relationship, ok := memberRelationship(boundary, member)
		if !ok {
			continue
		}
		relationshipEnvelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationshipEnvelope)
	}

	jobs, err := s.Client.ListClassificationJobs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Macie classification jobs: %w", err)
	}
	for _, job := range jobs {
		resource, err := awscloud.NewResourceEnvelope(jobObservation(boundary, job))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	allowLists, err := s.Client.ListAllowLists(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Macie allow lists: %w", err)
	}
	for _, allowList := range allowLists {
		resource, err := awscloud.NewResourceEnvelope(allowListObservation(boundary, allowList))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	identifiers, err := s.Client.ListCustomDataIdentifiers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Macie custom data identifiers: %w", err)
	}
	for _, identifier := range identifiers {
		resource, err := awscloud.NewResourceEnvelope(customDataIdentifierObservation(boundary, identifier))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	filters, err := s.Client.ListFindingsFilters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Macie findings filters: %w", err)
	}
	for _, filter := range filters {
		resource, err := awscloud.NewResourceEnvelope(findingsFilterObservation(boundary, filter))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	return envelopes, nil
}

func sessionObservation(boundary awscloud.Boundary, session Session, administratorID string) awscloud.ResourceObservation {
	return sessionObservationWithFindingCounts(boundary, session, administratorID, nil)
}

func sessionObservationWithFindingCounts(
	boundary awscloud.Boundary,
	session Session,
	administratorID string,
	severityCounts map[string]int64,
) awscloud.ResourceObservation {
	accountID := strings.TrimSpace(boundary.AccountID)
	attributes := map[string]any{
		"account_id":                   accountID,
		"enabled":                      session.Enabled,
		"status":                       strings.TrimSpace(session.Status),
		"finding_publishing_frequency": strings.TrimSpace(session.FindingPublishingFrequency),
		"service_role_arn":             strings.TrimSpace(session.ServiceRoleARN),
		"created_at":                   strings.TrimSpace(session.CreatedAt),
		"updated_at":                   strings.TrimSpace(session.UpdatedAt),
		"administrator_id":             strings.TrimSpace(administratorID),
	}
	if len(severityCounts) > 0 {
		attributes["finding_counts_by_severity"] = cloneInt64Map(severityCounts)
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ResourceID:         sessionResourceID(accountID),
		ResourceType:       awscloud.ResourceTypeMacieSession,
		Name:               accountID,
		State:              sessionState(session),
		Attributes:         attributes,
		CorrelationAnchors: []string{accountID},
		SourceRecordID:     sessionResourceID(accountID),
	}
}

func memberObservation(boundary awscloud.Boundary, member MemberAccount) awscloud.ResourceObservation {
	memberID := strings.TrimSpace(member.AccountID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   memberResourceID(memberID),
		ResourceType: awscloud.ResourceTypeMacieMemberAccount,
		Name:         memberID,
		State:        strings.TrimSpace(member.RelationshipStatus),
		Tags:         cloneStringMap(member.Tags),
		Attributes: map[string]any{
			"account_id":          memberID,
			"administrator_id":    strings.TrimSpace(member.AdministratorID),
			"relationship_status": strings.TrimSpace(member.RelationshipStatus),
			"invited_at":          strings.TrimSpace(member.InvitedAt),
			"updated_at":          strings.TrimSpace(member.UpdatedAt),
		},
		CorrelationAnchors: []string{memberID},
		SourceRecordID:     memberResourceID(memberID),
	}
}

func jobObservation(boundary awscloud.Boundary, job ClassificationJob) awscloud.ResourceObservation {
	jobID := strings.TrimSpace(job.JobID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   jobResourceID(jobID),
		ResourceType: awscloud.ResourceTypeMacieClassificationJob,
		Name:         strings.TrimSpace(job.Name),
		State:        strings.TrimSpace(job.JobStatus),
		Attributes: map[string]any{
			"job_id":     jobID,
			"job_type":   strings.TrimSpace(job.JobType),
			"job_status": strings.TrimSpace(job.JobStatus),
			"created_at": strings.TrimSpace(job.CreatedAt),
			// Aggregate bucket-criteria summary: the count of buckets the job
			// targets and whether it uses property/tag bucket criteria. The
			// explicit bucket list and the criteria expressions are never carried.
			"target_bucket_count":  job.BucketCount,
			"target_account_count": job.AccountCount,
			"uses_bucket_criteria": job.HasBucketCriteria,
		},
		CorrelationAnchors: []string{jobID},
		SourceRecordID:     jobResourceID(jobID),
	}
}

func allowListObservation(boundary awscloud.Boundary, allowList AllowList) awscloud.ResourceObservation {
	listID := strings.TrimSpace(allowList.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   allowListResourceID(listID),
		ResourceType: awscloud.ResourceTypeMacieAllowList,
		Name:         strings.TrimSpace(allowList.Name),
		Attributes: map[string]any{
			"allow_list_id": listID,
		},
		CorrelationAnchors: []string{listID},
		SourceRecordID:     allowListResourceID(listID),
	}
}

func customDataIdentifierObservation(boundary awscloud.Boundary, identifier CustomDataIdentifier) awscloud.ResourceObservation {
	identifierID := strings.TrimSpace(identifier.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   customDataIdentifierResourceID(identifierID),
		ResourceType: awscloud.ResourceTypeMacieCustomDataIdentifier,
		Name:         strings.TrimSpace(identifier.Name),
		Attributes: map[string]any{
			"custom_data_identifier_id": identifierID,
		},
		CorrelationAnchors: []string{identifierID},
		SourceRecordID:     customDataIdentifierResourceID(identifierID),
	}
}

func findingsFilterObservation(boundary awscloud.Boundary, filter FindingsFilter) awscloud.ResourceObservation {
	filterID := strings.TrimSpace(filter.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   findingsFilterResourceID(filterID),
		ResourceType: awscloud.ResourceTypeMacieFindingsFilter,
		Name:         strings.TrimSpace(filter.Name),
		Attributes: map[string]any{
			"findings_filter_id": filterID,
			"action":             strings.TrimSpace(filter.Action),
		},
		CorrelationAnchors: []string{filterID},
		SourceRecordID:     findingsFilterResourceID(filterID),
	}
}

func sessionState(session Session) string {
	if status := strings.TrimSpace(session.Status); status != "" {
		return status
	}
	if session.Enabled {
		return "ENABLED"
	}
	return "DISABLED"
}

func sessionResourceID(accountID string) string {
	return "macie2/session/" + strings.TrimSpace(accountID)
}

func memberResourceID(accountID string) string {
	return "macie2/member/" + strings.TrimSpace(accountID)
}

func jobResourceID(jobID string) string {
	return "macie2/classification-job/" + strings.TrimSpace(jobID)
}

func allowListResourceID(listID string) string {
	return "macie2/allow-list/" + strings.TrimSpace(listID)
}

func customDataIdentifierResourceID(identifierID string) string {
	return "macie2/custom-data-identifier/" + strings.TrimSpace(identifierID)
}

func findingsFilterResourceID(filterID string) string {
	return "macie2/findings-filter/" + strings.TrimSpace(filterID)
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]int64, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
