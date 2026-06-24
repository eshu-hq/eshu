// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// maxResourceGraphPages bounds one Azure scan so a malformed provider response
// that never clears its $skipToken cannot loop forever. A bounded shard is
// expected to finish well within this limit.
const maxResourceGraphPages = 1000

// PageProvider yields Resource Graph pages for one bounded scope and reports
// partial-scope access. Fixtures and a future live ARM client both satisfy this
// interface, so the collector logic is identical under test and in production.
//
// Implementations must not mutate Azure state; they perform read-only Resource
// Graph inventory or fixture resource-change reads only.
type PageProvider interface {
	// NextPage returns the Resource Graph page for the given $skipToken. The
	// empty token requests the first page. An empty SkipToken on the returned
	// page ends pagination.
	NextPage(ctx context.Context, skipToken string) (ResourceGraphPage, error)
	// ScopeAccess reports whether the configured scope was only partially
	// readable for this claim. A zero ScopeAccess means full access.
	ScopeAccess(ctx context.Context) ScopeAccess
}

// ResourceChangesProvider yields Resource Graph resourcechanges pages for one
// bounded scope. Implementations are fixture-only in this slice; live Azure
// transport remains gated in azureruntime.LiveProviderFactory.
type ResourceChangesProvider interface {
	// NextResourceChangesPage returns the resourcechanges page for the given
	// $skipToken. The empty token requests the first page.
	NextResourceChangesPage(ctx context.Context, skipToken string) (ResourceChangesPage, error)
}

// ScopeAccess reports partial-scope coverage for one claim. Partial true means
// the principal could read only part of the configured subscriptions or
// management groups, which the collector surfaces as explicit warning evidence
// rather than silent success.
type ScopeAccess struct {
	// Partial reports incomplete scope coverage.
	Partial bool
	// HiddenResourceCount counts resources hidden from the principal, when
	// known.
	HiddenResourceCount int
	// Reason is a bounded Warning* enum describing why coverage is partial.
	Reason string
	// Message is an operator-facing detail, sanitized before persistence.
	Message string
}

// ScanResult summarizes one bounded Azure scan: the ordered emitted facts and
// bounded telemetry counters. Counters carry no resource identifiers.
type ScanResult struct {
	// Facts are the ordered emitted facts (resources first, then warnings).
	Facts []facts.Envelope
	// ResourceCount counts azure_cloud_resource facts emitted.
	ResourceCount int
	// WarningCount counts azure_collection_warning facts emitted.
	WarningCount int
	// PageCount counts Resource Graph pages read.
	PageCount int
	// SkipTokenResumes counts pages fetched via a non-empty $skipToken.
	SkipTokenResumes int
	// Truncated reports whether any page set resultTruncated.
	Truncated bool
	// Partial reports whether scope access was partial.
	Partial bool
	// TagObservationCount counts azure_tag_observation facts emitted. It is
	// zero unless the collector was given a redaction key (tag values are never
	// fingerprinted or carried without one).
	TagObservationCount int
	// RelationshipCount counts azure_cloud_relationship facts emitted from the
	// ARM managedBy owning-resource reference. It is provenance-only and needs no
	// redaction key.
	RelationshipCount int
	// IdentityObservationCount counts azure_identity_observation facts emitted
	// from system-assigned managed identities. It is zero unless the collector
	// was given a redaction key (principal GUIDs are never carried without one).
	IdentityObservationCount int
	// ResourceChangeCount counts azure_resource_change facts emitted from the
	// Resource Graph resourcechanges source lane. These facts remain
	// provenance-only; they do not admit graph state.
	ResourceChangeCount int
	// DNSRecordCount counts azure_dns_record facts emitted from supported DNS
	// record-set rows. Record names and targets are fingerprinted.
	DNSRecordCount int
	// ImageReferenceCount counts azure_image_reference facts emitted from
	// supported runtime image metadata. Owning resources remain source evidence.
	ImageReferenceCount int
}

// Collector turns fixture or live Resource Graph pages into ordered Azure
// source facts for one claim. It owns pagination, normalization, redaction,
// fact emission, and bounded telemetry. It does not commit facts, schedule
// claims, choose credentials, or write graph truth.
type Collector struct {
	provider     PageProvider
	metrics      Metrics
	redactionKey redact.Key
}

// CollectorOption configures an optional Collector behavior.
type CollectorOption func(*Collector)

// WithRedactionKey enables azure_tag_observation emission keyed by the given
// redaction key. Tag values are fingerprinted with this key (never stored raw),
// so a zero key (the default) means no tag observation facts are emitted at all.
func WithRedactionKey(key redact.Key) CollectorOption {
	return func(c *Collector) {
		c.redactionKey = key
	}
}

// NewCollector builds a Collector over a PageProvider. A nil Metrics is
// tolerated; telemetry recording becomes a no-op. Without WithRedactionKey the
// collector emits resource and warning facts only; tag observation emission
// requires a redaction key so tag values are never carried unfingerprinted.
func NewCollector(provider PageProvider, metrics Metrics, opts ...CollectorOption) *Collector {
	if metrics == nil {
		metrics = NopMetrics{}
	}
	c := &Collector{provider: provider, metrics: metrics}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Collect reads all Resource Graph pages for the boundary, emits one
// azure_cloud_resource fact per row plus warning facts for truncation and
// partial scope, and returns the ordered result. The same boundary and provider
// produce byte-identical stable keys and fact IDs, so duplicate delivery of a
// generation converges. Provider read errors abort the scan instead of emitting
// silently incomplete success.
func (c *Collector) Collect(ctx context.Context, boundary Boundary) (ScanResult, error) {
	if err := validateBoundary(boundary); err != nil {
		return ScanResult{}, fmt.Errorf("invalid azure scan boundary: %w", err)
	}
	if c.provider == nil {
		return ScanResult{}, fmt.Errorf("azure collector requires a page provider")
	}
	if boundary.SourceLane == SourceLaneResourceChanges {
		return c.collectResourceChanges(ctx, boundary)
	}

	var result ScanResult
	skipToken := ""
	for {
		if ctx.Err() != nil {
			return ScanResult{}, ctx.Err()
		}
		if result.PageCount >= maxResourceGraphPages {
			return ScanResult{}, fmt.Errorf("azure scan exceeded %d page bound", maxResourceGraphPages)
		}

		page, err := c.provider.NextPage(ctx, skipToken)
		if err != nil {
			c.metrics.RecordAPICall(ctx, boundary, "resources_list", StatusClassError)
			return ScanResult{}, fmt.Errorf("read resource graph page: %w", err)
		}
		c.metrics.RecordAPICall(ctx, boundary, "resources_list", StatusClassSuccess)
		result.PageCount++
		if skipToken != "" {
			result.SkipTokenResumes++
			c.metrics.RecordSkipTokenResume(ctx, boundary)
		}

		if err := c.emitPageResources(boundary, page, &result); err != nil {
			return ScanResult{}, err
		}

		if page.ResultTruncated {
			result.Truncated = true
			if err := c.emitTruncationWarning(boundary, &result); err != nil {
				return ScanResult{}, err
			}
		}

		skipToken = page.SkipToken
		if skipToken == "" {
			break
		}
	}

	if err := c.emitScopeWarning(ctx, boundary, &result); err != nil {
		return ScanResult{}, err
	}

	c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureCloudResourceFactKind, result.ResourceCount)
	c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureCollectionWarningFactKind, result.WarningCount)
	if result.TagObservationCount > 0 {
		c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureTagObservationFactKind, result.TagObservationCount)
	}
	if result.RelationshipCount > 0 {
		c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureCloudRelationshipFactKind, result.RelationshipCount)
	}
	if result.IdentityObservationCount > 0 {
		c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureIdentityObservationFactKind, result.IdentityObservationCount)
	}
	if result.DNSRecordCount > 0 {
		c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureDNSRecordFactKind, result.DNSRecordCount)
	}
	if result.ImageReferenceCount > 0 {
		c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureImageReferenceFactKind, result.ImageReferenceCount)
	}
	return result, nil
}

func (c *Collector) collectResourceChanges(ctx context.Context, boundary Boundary) (ScanResult, error) {
	if c.redactionKey.IsZero() {
		return ScanResult{}, fmt.Errorf("azure resource change collection requires a redaction key")
	}
	provider, ok := c.provider.(ResourceChangesProvider)
	if !ok {
		return ScanResult{}, fmt.Errorf("azure resource change lane requires a resource changes page provider")
	}

	var result ScanResult
	skipToken := ""
	for {
		if ctx.Err() != nil {
			return ScanResult{}, ctx.Err()
		}
		if result.PageCount >= maxResourceGraphPages {
			return ScanResult{}, fmt.Errorf("azure resource change scan exceeded %d page bound", maxResourceGraphPages)
		}
		page, err := provider.NextResourceChangesPage(ctx, skipToken)
		if err != nil {
			c.metrics.RecordAPICall(ctx, boundary, "resource_changes_list", StatusClassError)
			return ScanResult{}, fmt.Errorf("read resource changes page: %w", err)
		}
		c.metrics.RecordAPICall(ctx, boundary, "resource_changes_list", StatusClassSuccess)
		result.PageCount++
		if skipToken != "" {
			result.SkipTokenResumes++
			c.metrics.RecordSkipTokenResume(ctx, boundary)
		}
		if err := c.emitPageResourceChanges(boundary, page, &result); err != nil {
			return ScanResult{}, err
		}
		if page.ResultTruncated {
			result.Truncated = true
			if err := c.emitTruncationWarning(boundary, &result); err != nil {
				return ScanResult{}, err
			}
		}
		skipToken = page.SkipToken
		if skipToken == "" {
			break
		}
	}
	if err := c.emitScopeWarning(ctx, boundary, &result); err != nil {
		return ScanResult{}, err
	}
	c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureResourceChangeFactKind, result.ResourceChangeCount)
	c.metrics.RecordFactsEmitted(ctx, boundary, facts.AzureCollectionWarningFactKind, result.WarningCount)
	return result, nil
}

func (c *Collector) emitPageResourceChanges(
	boundary Boundary,
	page ResourceChangesPage,
	result *ScanResult,
) error {
	for _, row := range page.Changes {
		observation, err := row.toObservation(boundary)
		if err != nil {
			warning, werr := NewWarningEnvelope(WarningObservation{
				Boundary:    boundary,
				WarningKind: WarningUnsupported,
				Outcome:     OutcomeUnsupported,
				Message:     fmt.Sprintf("unparseable resource change: %v", err),
			})
			if werr != nil {
				return fmt.Errorf("build unsupported change warning: %w", werr)
			}
			result.Facts = append(result.Facts, warning)
			result.WarningCount++
			continue
		}
		env, err := NewResourceChangeEnvelope(observation, c.redactionKey)
		if err != nil {
			return fmt.Errorf("build azure_resource_change fact: %w", err)
		}
		result.Facts = append(result.Facts, env)
		result.ResourceChangeCount++
	}
	return nil
}

func (c *Collector) emitPageResources(boundary Boundary, page ResourceGraphPage, result *ScanResult) error {
	for _, row := range page.Rows {
		identity, err := ParseARMIdentity(row.ID)
		if err != nil {
			// A malformed row is unsupported evidence, not a scan failure.
			warning, werr := NewWarningEnvelope(WarningObservation{
				Boundary:    boundary,
				WarningKind: WarningUnsupported,
				Outcome:     OutcomeUnsupported,
				Message:     fmt.Sprintf("unparseable arm id: %v", err),
			})
			if werr != nil {
				return fmt.Errorf("build unsupported warning: %w", werr)
			}
			result.Facts = append(result.Facts, warning)
			result.WarningCount++
			continue
		}
		observation := rowToObservation(boundary, row, identity)
		env, err := NewResourceEnvelope(observation)
		if err != nil {
			return fmt.Errorf("build azure_cloud_resource fact: %w", err)
		}
		result.Facts = append(result.Facts, env)
		result.ResourceCount++

		// Emit a paired azure_tag_observation only when a redaction key is set
		// and the resource carries usable tags. Without a key, tag values are
		// never fingerprinted or carried; the resource fact still emits.
		if !c.redactionKey.IsZero() && hasUsableTag(observation.Tags) {
			tagEnv, err := NewTagObservationEnvelope(observation, c.redactionKey)
			if err != nil {
				return fmt.Errorf("build azure_tag_observation fact: %w", err)
			}
			result.Facts = append(result.Facts, tagEnv)
			result.TagObservationCount++
		}

		// Emit a provenance-only managed_by relationship from the ARM owning
		// resource reference. It needs no redaction key.
		if rel, ok := relationshipFromManagedBy(observation.Boundary, row); ok {
			relEnv, err := NewRelationshipEnvelope(rel)
			if err != nil {
				return fmt.Errorf("build azure_cloud_relationship fact: %w", err)
			}
			result.Facts = append(result.Facts, relEnv)
			result.RelationshipCount++
		}

		// Emit managed-identity observations (system-assigned + user-assigned)
		// when a redaction key is set; principal/client GUIDs are fingerprinted,
		// never carried raw.
		if !c.redactionKey.IsZero() {
			for _, idObs := range identityObservationsFromRow(observation.Boundary, row) {
				idEnv, err := NewIdentityObservationEnvelope(idObs, c.redactionKey)
				if err != nil {
					return fmt.Errorf("build azure_identity_observation fact: %w", err)
				}
				result.Facts = append(result.Facts, idEnv)
				result.IdentityObservationCount++
			}
			if dnsObs, ok := dnsRecordObservationFromRow(observation.Boundary, row); ok {
				dnsEnv, err := NewDNSRecordEnvelope(dnsObs, c.redactionKey)
				if err != nil {
					return fmt.Errorf("build azure_dns_record fact: %w", err)
				}
				result.Facts = append(result.Facts, dnsEnv)
				result.DNSRecordCount++
			}
			for _, imageObs := range imageReferenceObservationsFromRow(observation.Boundary, row) {
				imageEnv, err := NewImageReferenceEnvelope(imageObs, c.redactionKey)
				if err != nil {
					return fmt.Errorf("build azure_image_reference fact: %w", err)
				}
				result.Facts = append(result.Facts, imageEnv)
				result.ImageReferenceCount++
			}
		}
	}
	return nil
}

func (c *Collector) emitTruncationWarning(boundary Boundary, result *ScanResult) error {
	warning, err := NewWarningEnvelope(WarningObservation{
		Boundary:    boundary,
		WarningKind: WarningResultTruncated,
		Outcome:     OutcomePartial,
		Retryable:   true,
		Message:     "resource graph result truncated; narrow the query or page further",
	})
	if err != nil {
		return fmt.Errorf("build truncation warning: %w", err)
	}
	result.Facts = append(result.Facts, warning)
	result.WarningCount++
	return nil
}

func (c *Collector) emitScopeWarning(ctx context.Context, boundary Boundary, result *ScanResult) error {
	access := c.provider.ScopeAccess(ctx)
	if !access.Partial {
		return nil
	}
	reason := access.Reason
	if reason == "" {
		reason = WarningPartialScope
	}
	warning, err := NewWarningEnvelope(WarningObservation{
		Boundary:            boundary,
		WarningKind:         reason,
		Outcome:             OutcomePartial,
		Retryable:           true,
		HiddenResourceCount: access.HiddenResourceCount,
		Message:             access.Message,
	})
	if err != nil {
		return fmt.Errorf("build partial-scope warning: %w", err)
	}
	result.Facts = append(result.Facts, warning)
	result.WarningCount++
	result.Partial = true
	c.metrics.RecordPartialScope(ctx, boundary, reason)
	return nil
}

func rowToObservation(boundary Boundary, row ResourceRow, identity ARMIdentity) ResourceObservation {
	rowBoundary := boundary
	if location := row.Location; location != "" {
		rowBoundary.LocationBucket = location
	}
	return ResourceObservation{
		Boundary:       rowBoundary,
		ARMResourceID:  row.ID,
		Identity:       identity,
		Kind:           row.Kind,
		SKUClass:       row.SKUClass(),
		ManagedBy:      row.ManagedBy,
		APIVersion:     row.APIVersion,
		ProviderTime:   row.ProviderTime(),
		Tags:           row.Tags,
		HasIdentity:    row.HasIdentity(),
		RawExtension:   resourceExtensionFromRow(row),
		SourceRecordID: identity.Normalized,
	}
}
