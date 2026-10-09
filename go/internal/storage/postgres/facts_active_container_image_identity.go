// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// identityFactFilterSQL is the shared 6-arm filter used by both the load query
// and the epoch probe. Both queries MUST embed the identical filter text;
// TestIdentityEpochProbeFilterDrift locks this invariant.
const identityFactFilterSQL = `(
    (
      fact.fact_kind IN ('oci_registry.image_tag_observation', 'oci_registry.image_manifest', 'oci_registry.image_index')
      AND fact.source_system = 'oci_registry'
    )
    OR (
      fact.fact_kind = 'aws_image_reference'
      AND fact.source_system = 'aws'
    )
    OR (
      fact.fact_kind = 'azure_image_reference'
      AND fact.source_system = 'azure'
    )
    OR (
      fact.fact_kind = 'gcp_image_reference'
      AND fact.source_system = 'gcp'
    )
    OR (
      fact.fact_kind = 'aws_relationship'
      AND fact.source_system = 'aws'
      AND fact.payload->>'target_type' = 'container_image'
    )
    OR (
      fact.fact_kind = 'content_entity'
      AND fact.source_system = 'git'
      AND (
        fact.payload->'entity_metadata' ? 'container_images'
        OR fact.payload->'metadata' ? 'container_images'
      )
    )
    OR (
      fact.fact_kind = 'file'
      AND fact.source_system = 'git'
      AND fact.payload->'parsed_file_data' ? 'dockerfile_stages'
    )
  )`

// listActiveContainerImageIdentityFactsQuery pages the identity fact set in
// (observed_at, fact_id) keyset order, restricted to each scope's active
// generation.
//
// The active-generation restriction is a hashed SubPlan filter, deliberately
// not a JOIN. As a JOIN against ingestion_scopes and scope_generations the
// planner drives the query from the roughly 1,400 active scopes, reads and
// filters the whole active identity set, and top-N sorts it for every
// 500-row page, so a full load was quadratic (about 0.35 to 0.63 s per page on
// a 547k-row active set, #7805). The "OR FALSE" is load-bearing: it keeps the
// planner from pulling the IN subquery up into a semi-join, so the filter
// rides on the ordered scan of fact_records_identity_epoch_idx_v2 and the
// LIMIT stops that scan after about one page of rows. PostgreSQL folds the
// constant away during planning, so it costs nothing at run time.
// TestIdentityPageQueryPlanRidesOrderedIndexLive pins the plan, first page and
// mid-load page, on a real server in the postgres_ci lane (verified on
// PostgreSQL 18); the text-shape asserts in
// TestFactStoreListActiveContainerImageIdentityFactsUsesActiveIdentityGenerations
// only keep the "OR FALSE" from being deleted.
//
// The (scope_id, active_generation_id) pairs are exactly the pairs the former
// JOIN matched: a scope's active generation, when that generation row is
// itself status 'active'.
const listActiveContainerImageIdentityFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    COALESCE(fact.source_uri, ''),
    COALESCE(fact.source_record_id, ''),
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM fact_records AS fact
WHERE ` + identityFactFilterSQL + `
  AND fact.is_tombstone = FALSE
  AND (
    (fact.scope_id, fact.generation_id) IN (
      SELECT scope.scope_id, scope.active_generation_id
      FROM ingestion_scopes AS scope
      JOIN scope_generations AS generation
        ON generation.scope_id = scope.scope_id
       AND generation.generation_id = scope.active_generation_id
      WHERE generation.status = 'active'
    )
    OR FALSE
  )
  AND (
    $1::timestamptz IS NULL
    OR (fact.observed_at, fact.fact_id) > ($1::timestamptz, $2::text)
  )
ORDER BY fact.observed_at ASC, fact.fact_id ASC
LIMIT $3
`

// probeIdentityEpochQuery returns (count, COALESCE(max(observed_at), '-infinity'),
// active_fingerprint). The count and max are taken over identity facts of each
// scope's ACTIVE generation only, the same set the page query serves, so
// retention and supersession deletes of old-generation rows do not move the
// epoch (#7805: the former all-generations count moved every few minutes, which
// discarded nearly every load). An insert or delete on an active generation
// still moves it. The active restriction is the same hashed SubPlan filter the
// page query uses, "OR FALSE" included, for the same planner reason (see
// listActiveContainerImageIdentityFactsQuery). It reads the heap for scope and
// generation, so it costs about twice the former index-only probe (about
// 120 ms versus 60 ms on a 1.0M-identity-fact shim, docs/internal/evidence/
// 7805-identity-epoch-flight.md) and needs no new index.
//
// The fingerprint is a collision-resistant SHA-256 digest of the active
// generation mapping from ingestion_scopes (every scope's
// "scope_id:active_generation_id" pair, ORDER BY scope_id, joined with '|').
// It detects supersession (active_generation_id flip) so the cache misses when
// a new generation becomes active even when the active count and max
// observed_at are unchanged. Unlike a summed hash, the ordered digest has no
// collision mode where two different active mappings (a 32-bit hashtext
// collision, or offsetting deltas that cancel in a sum) produce the same
// fingerprint.
const probeIdentityEpochQuery = `
SELECT
    f.cnt,
    COALESCE(f.max_obs, '-infinity'::timestamptz),
    COALESCE(s.fingerprint, '')
FROM (
    SELECT count(*) AS cnt, max(fact.observed_at) AS max_obs
    FROM fact_records AS fact
    WHERE ` + identityFactFilterSQL + `
      AND fact.is_tombstone = FALSE
      AND (
        (fact.scope_id, fact.generation_id) IN (
          SELECT scope.scope_id, scope.active_generation_id
          FROM ingestion_scopes AS scope
          JOIN scope_generations AS generation
            ON generation.scope_id = scope.scope_id
           AND generation.generation_id = scope.active_generation_id
          WHERE generation.status = 'active'
        )
        OR FALSE
      )
) f
CROSS JOIN (
    SELECT encode(sha256(convert_to(COALESCE(string_agg(scope_id::text || ':' || active_generation_id::text, '|' ORDER BY scope_id), ''), 'UTF8')), 'hex') AS fingerprint
    FROM ingestion_scopes
) s
`

// ListActiveContainerImageIdentityFacts loads active OCI registry facts and
// active Git/AWS/Azure/GCP image-reference facts for cross-scope identity joins.
// When an identity cache is wired, the result set is served from the cache on
// epoch match and reloaded via singleflight on miss.
func (s *FactStore) ListActiveContainerImageIdentityFacts(ctx context.Context) ([]facts.Envelope, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	if s.identityCache != nil {
		return s.identityCache.get(ctx, s)
	}

	return s.loadIdentityFactsUncached(ctx)
}

// loadIdentityFactsUncached performs the paginated load without caching.
func (s *FactStore) loadIdentityFactsUncached(ctx context.Context) ([]facts.Envelope, error) {
	var loaded []facts.Envelope
	var cursorObservedAt *time.Time
	var cursorFactID string
	for {
		page, err := s.listActiveContainerImageIdentityFactsPage(ctx, cursorObservedAt, cursorFactID)
		if err != nil {
			return nil, err
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}

		last := page[len(page)-1]
		observedAt := last.ObservedAt.UTC()
		cursorObservedAt = &observedAt
		cursorFactID = last.FactID
	}
}

// probeIdentityEpoch returns the epoch probe for the identity fact set.
func (s *FactStore) probeIdentityEpoch(ctx context.Context) (identityEpoch, error) {
	rows, err := s.database.QueryContext(ctx, probeIdentityEpochQuery)
	if err != nil {
		return identityEpoch{}, fmt.Errorf("probe identity epoch: %w", err)
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return identityEpoch{}, fmt.Errorf("probe identity epoch: no rows returned")
	}
	var count int64
	var maxObservedAt time.Time
	var fingerprint string
	if err := rows.Scan(&count, &maxObservedAt, &fingerprint); err != nil {
		return identityEpoch{}, fmt.Errorf("probe identity epoch scan: %w", err)
	}
	if err := rows.Err(); err != nil {
		return identityEpoch{}, fmt.Errorf("probe identity epoch rows: %w", err)
	}

	return identityEpoch{count: int(count), maxObservedAt: maxObservedAt, activeFingerprint: fingerprint}, nil
}

func (s *FactStore) listActiveContainerImageIdentityFactsPage(
	ctx context.Context,
	cursorObservedAt *time.Time,
	cursorFactID string,
) ([]facts.Envelope, error) {
	var cursor any
	if cursorObservedAt != nil {
		cursor = cursorObservedAt.UTC()
	}

	rows, err := s.database.QueryContext(
		ctx,
		listActiveContainerImageIdentityFactsQuery,
		cursor,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list active container image identity facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list active container image identity facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active container image identity facts: %w", err)
	}

	return loaded, nil
}

// IdentityEpochUnstableFailureClass is the durable failure_class of
// identityLoadUnstableError. It labels the reducer retry and failure metrics and
// the queue's failure_class column. It is deliberately NOT one of the
// non-counting retry classes: each retry consumes a claim attempt, so a
// persistently moving epoch ends in the normal dead-letter policy instead of an
// unbounded loop (#7805).
const IdentityEpochUnstableFailureClass = "identity_epoch_unstable"

// identityLoadUnstableError reports that the identity fact set kept changing
// (or could not be re-validated) while it was being paged, so no consistent set
// exists to decide on. It is retryable and carries a failure class: the reducer
// queue re-runs the item, which re-enters the cache with a fresh epoch probe.
type identityLoadUnstableError struct {
	reason string
}

func newIdentityLoadUnstableError(reason string) error {
	return identityLoadUnstableError{reason: reason}
}

// Error implements error.
func (e identityLoadUnstableError) Error() string {
	return "identity fact set did not stabilize (" + e.reason + "); the item will be retried"
}

// Retryable marks the failure as one the durable queue should retry.
func (identityLoadUnstableError) Retryable() bool { return true }

// FailureClass names the failure for queue status and operator triage.
func (identityLoadUnstableError) FailureClass() string { return IdentityEpochUnstableFailureClass }

// estimateEnvelopesByteSize returns a conservative byte estimate for a set of
// fact envelopes, used for the cache cap check. It returns an error if any
// envelope's Payload cannot be sized via json.Marshal (e.g. a NaN/Inf float or
// another value json.Marshal rejects). Callers MUST treat a non-nil error as
// "unsizable, do not cache" rather than substituting a 0 or estimated size:
// silently under-counting an unsizable payload could let it slip under the
// cache's maxBytes cap.
func estimateEnvelopesByteSize(loaded []facts.Envelope) (int64, error) {
	var total int64
	for _, env := range loaded {
		total += int64(len(env.FactID))
		total += int64(len(env.ScopeID))
		total += int64(len(env.GenerationID))
		total += int64(len(env.FactKind))
		total += int64(len(env.StableFactKey))
		total += int64(len(env.SchemaVersion))
		total += int64(len(env.CollectorKind))
		total += int64(len(env.SourceConfidence))
		total += int64(len(env.SourceRef.SourceSystem))
		total += int64(len(env.SourceRef.FactKey))
		total += int64(len(env.SourceRef.SourceURI))
		total += int64(len(env.SourceRef.SourceRecordID))
		// Estimate payload as its JSON serialization size.
		if env.Payload != nil {
			b, err := json.Marshal(env.Payload)
			if err != nil {
				return 0, fmt.Errorf("estimate envelope %s payload size: %w", env.FactID, err)
			}
			total += int64(len(b))
		}
		// Fixed overhead: each time.Time, int64, bool ~ 40 bytes.
		total += 40
	}
	return total, nil
}

// defensiveCopyEnvelopes returns a fresh slice with a one-level copy of each
// envelope's Payload map, so callers cannot mutate the shared cache through
// top-level payload keys. Nested payload values (e.g. entity_metadata maps)
// are shared by reference; identity-load callers are read-only by audit, and
// new callers must not mutate nested payload values.
func defensiveCopyEnvelopes(src []facts.Envelope) []facts.Envelope {
	dst := make([]facts.Envelope, len(src))
	for i, env := range src {
		dst[i] = env
		if env.Payload != nil {
			dst[i].Payload = make(map[string]any, len(env.Payload))
			for k, v := range env.Payload {
				dst[i].Payload[k] = v
			}
		}
	}
	return dst
}

// maxIdentityWaiterFlights is how many flights one caller may wait out without
// being served before it gives up. Without the bound a caller whose probe never
// matches a stable flight re-probes and re-joins forever while only the leaders
// of those flights consume claim attempts. Each such caller holds one worker of
// the reducer pool for as long as the churn lasts, so under sustained churn the
// pool stalls (#7805). The leader is bounded by its two load attempts instead.
const maxIdentityWaiterFlights = 3

// Reasons carried in the error text of a caller that gave up, and the closed
// waiter outcomes the metric records for it: gave_up_flights dominant means
// sustained churn, gave_up_wall dominant means a slow but stable flight.
const (
	identityGaveUpFlights   = "waited out the maximum number of flights"
	identityGaveUpWallClock = "waited longer than one heartbeat interval"

	identityWaiterGaveUpFlights = "gave_up_flights"
	identityWaiterGaveUpWall    = "gave_up_wall"
)

// errIdentityWaitExpired is the internal signal that a caller's wall-clock wait
// budget ran out while it was parked on a flight. get turns it into a final
// probe and, failing that, the retryable identity_epoch_unstable error.
var errIdentityWaitExpired = errors.New("identity cache wait budget expired")

// IdentityEpochCacheOption tunes an IdentityEpochCache at construction.
type IdentityEpochCacheOption func(*IdentityEpochCache)

// WithHeartbeatInterval bounds the total time one caller may spend waiting on
// other callers' flights to one reducer heartbeat interval. Pass the interval
// the reducer service already derives from its claim lease (LeaseDuration / 2),
// so the cache adds no knob of its own. A non-positive value disables the
// wall-clock half of the bound; the flight-count half always applies.
func WithHeartbeatInterval(interval time.Duration) IdentityEpochCacheOption {
	return func(c *IdentityEpochCache) {
		c.heartbeatInterval = interval
	}
}

// waitBudgetUsedUp reports whether a caller has spent its whole wall-clock wait
// budget (one heartbeat interval in total). The flight-count half of the budget
// is checked by the caller's loop.
func (c *IdentityEpochCache) waitBudgetUsedUp(waitedFor time.Duration) bool {
	return c.heartbeatInterval > 0 && waitedFor >= c.heartbeatInterval
}

// waitForFlight blocks until flight finishes, the caller's context ends, or the
// caller has spent a full heartbeat interval waiting on flights in total
// (waitedFor accumulates across calls). It returns nil when the flight finished,
// the context error when the caller's own context ended, and
// errIdentityWaitExpired when the wall-clock bound tripped.
func (c *IdentityEpochCache) waitForFlight(
	ctx context.Context,
	flight *identityFlight,
	waitedFor *time.Duration,
) error {
	var expired <-chan time.Time
	stop := func() bool { return true }
	if c.heartbeatInterval > 0 {
		remaining := c.heartbeatInterval - *waitedFor
		if remaining <= 0 {
			return errIdentityWaitExpired
		}
		if c.newTimer != nil {
			expired, stop = c.newTimer(remaining)
		} else {
			timer := time.NewTimer(remaining)
			expired, stop = timer.C, timer.Stop
		}
	}
	started := c.clockNow()
	defer func() {
		stop()
		*waitedFor += c.clockNow().Sub(started)
	}()
	select {
	case <-flight.done:
		return nil
	case <-expired:
		return errIdentityWaitExpired
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *IdentityEpochCache) clockNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// giveUp ends a caller that used up its wait budget. It makes ONE final probe
// and never starts a load: when the cache now holds a set whose epoch matches
// the probe (a consistent flight filled it while this caller waited), the caller
// is served that set as a plain hit; otherwise its item fails with the
// retryable identity_epoch_unstable error and the given closed outcome.
func (c *IdentityEpochCache) giveUp(
	ctx context.Context,
	store *FactStore,
	outcome string,
	reason string,
) ([]facts.Envelope, error) {
	probe, err := store.probeIdentityEpoch(context.WithoutCancel(ctx))
	if err == nil {
		c.mu.Lock()
		if c.facts != nil && c.epoch == probe {
			result := defensiveCopyEnvelopes(c.facts)
			c.inst.IdentityCacheHitTotal.Add(ctx, 1)
			c.mu.Unlock()
			return result, nil
		}
		c.mu.Unlock()
	}
	return nil, c.gaveUp(ctx, outcome, reason)
}

// gaveUp records that a caller hit its patience bound and returns the
// retryable error that fails its item.
func (c *IdentityEpochCache) gaveUp(ctx context.Context, outcome, reason string) error {
	c.inst.IdentityCacheFlightWaiterTotal.Add(context.WithoutCancel(ctx), 1,
		metric.WithAttributes(telemetry.AttrOutcome(outcome)))
	return newIdentityLoadUnstableError(reason)
}
