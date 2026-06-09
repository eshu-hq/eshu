package azurecloud

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// maxResourceGraphPages bounds one Azure scan so a malformed provider response
// that never clears its $skipToken cannot loop forever. A bounded shard is
// expected to finish well within this limit.
const maxResourceGraphPages = 1000

// PageProvider yields Resource Graph pages for one bounded scope and reports
// partial-scope access. Fixtures and a future live ARM client both satisfy this
// interface, so the collector logic is identical under test and in production.
//
// Implementations must not mutate Azure state; they perform read-only
// inventory queries only.
type PageProvider interface {
	// NextPage returns the Resource Graph page for the given $skipToken. The
	// empty token requests the first page. An empty SkipToken on the returned
	// page ends pagination.
	NextPage(ctx context.Context, skipToken string) (ResourceGraphPage, error)
	// ScopeAccess reports whether the configured scope was only partially
	// readable for this claim. A zero ScopeAccess means full access.
	ScopeAccess(ctx context.Context) ScopeAccess
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
}

// Collector turns fixture or live Resource Graph pages into ordered Azure
// source facts for one claim. It owns pagination, normalization, redaction,
// fact emission, and bounded telemetry. It does not commit facts, schedule
// claims, choose credentials, or write graph truth.
type Collector struct {
	provider PageProvider
	metrics  Metrics
}

// NewCollector builds a Collector over a PageProvider. A nil Metrics is
// tolerated; telemetry recording becomes a no-op.
func NewCollector(provider PageProvider, metrics Metrics) *Collector {
	if metrics == nil {
		metrics = NopMetrics{}
	}
	return &Collector{provider: provider, metrics: metrics}
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
	return result, nil
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
		env, err := NewResourceEnvelope(rowToObservation(boundary, row, identity))
		if err != nil {
			return fmt.Errorf("build azure_cloud_resource fact: %w", err)
		}
		result.Facts = append(result.Facts, env)
		result.ResourceCount++
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
		RawExtension:   row.Properties,
		SourceRecordID: identity.Normalized,
	}
}
