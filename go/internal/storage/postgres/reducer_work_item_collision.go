// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/array"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// reducerWorkItemCollisionFailureClass is the closed failure class of an enqueue
// that found its work item id held by a different intent (#11215).
const reducerWorkItemCollisionFailureClass = "reducer_work_item_id_collision"

// reducerWorkItemCollisionSampleCap bounds the samples an error names.
const reducerWorkItemCollisionSampleCap = 10

// ReducerWorkItemCollisionSample is one incoming intent whose work item id a
// different intent already holds. Identifiers only, never a payload.
type ReducerWorkItemCollisionSample struct {
	WorkItemID           string
	IncomingScopeID      string
	IncomingGenerationID string
	IncomingDomain       string
	IncomingEntityKey    string
	StoredScopeID        string
	StoredGenerationID   string
	StoredDomain         string
	StoredEntityKey      string
}

// ReducerWorkItemCollisionError reports that enqueue found a work item id held
// by a different intent: reducerWorkItemID is not injective, so two intents with
// different scope, generation, domain or entity key can map to one id, and the
// ON CONFLICT (work_item_id) DO NOTHING coalesce would have dropped the second
// with no error (#11215). The enqueue is refused. It is not retryable: the id is
// a pure function of the intent, so a retry collides again.
type ReducerWorkItemCollisionError struct {
	Count   int
	Samples []ReducerWorkItemCollisionSample
}

// Error names the first collision; no payload.
func (e *ReducerWorkItemCollisionError) Error() string {
	first := ""
	if len(e.Samples) > 0 {
		s := e.Samples[0]
		first = fmt.Sprintf("; first id %q is held by scope %q generation %q domain %q, incoming scope %q generation %q domain %q",
			s.WorkItemID, s.StoredScopeID, s.StoredGenerationID, s.StoredDomain, s.IncomingScopeID, s.IncomingGenerationID, s.IncomingDomain)
	}
	return fmt.Sprintf("%s: %d reducer intent(s) share a work item id with a different queued intent%s",
		reducerWorkItemCollisionFailureClass, e.Count, first)
}

// Retryable is false: the collision repeats on every retry.
func (*ReducerWorkItemCollisionError) Retryable() bool { return false }

// FailureClass is the closed class the failure path records.
func (*ReducerWorkItemCollisionError) FailureClass() string {
	return reducerWorkItemCollisionFailureClass
}

// heldWorkItemsQuery returns the stored identity of every incoming work item id
// whose queued row differs from the incoming intent in scope, generation, domain
// or entity key. A primary-key probe; it reads no payload but the entity key.
const heldWorkItemsQuery = `
SELECT t.work_item_id, w.scope_id, w.generation_id, w.domain, COALESCE(w.payload ->> 'entity_key', '')
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])
    AS t(work_item_id, scope_id, generation_id, domain, entity_key)
JOIN fact_work_items AS w ON w.work_item_id = t.work_item_id
WHERE w.scope_id IS DISTINCT FROM t.scope_id
   OR w.generation_id IS DISTINCT FROM t.generation_id
   OR w.domain IS DISTINCT FROM t.domain
   OR COALESCE(w.payload ->> 'entity_key', '') IS DISTINCT FROM t.entity_key`

// resolveEnqueueShortfall returns the rows an enqueue inserted, after refusing a
// collision a shortfall hides: fewer rows than attempted is normally a benign
// duplicate, but reducerWorkItemID is not injective (#11215), so a held id can
// belong to a different intent, and coalescing that away would drop it silently.
// A full insert runs no probe.
func resolveEnqueueShortfall(ctx context.Context, database db.Queryer, batch []runtime.ReducerIntent, inserted int64) (int64, error) {
	if inserted >= int64(len(batch)) {
		return inserted, nil
	}
	if err := refuseWorkItemCollisions(ctx, database, batch); err != nil {
		return 0, err
	}
	return inserted, nil
}

// refuseWorkItemCollisions resolves an enqueue shortfall (fewer rows inserted
// than attempted). A short batch is normally a benign duplicate, the same intent
// already queued; only a held row whose identity differs from the incoming intent
// is a collision. It runs one primary-key probe, and only on a shortfall.
func refuseWorkItemCollisions(ctx context.Context, database db.Queryer, batch []runtime.ReducerIntent) error {
	ids := make([]string, len(batch))
	scopes := make([]string, len(batch))
	generations := make([]string, len(batch))
	domains := make([]string, len(batch))
	entityKeys := make([]string, len(batch))
	byID := make(map[string]runtime.ReducerIntent, len(batch))
	for i, intent := range batch {
		ids[i] = reducerWorkItemID(intent)
		scopes[i], generations[i], domains[i], entityKeys[i] = intent.ScopeID, intent.GenerationID, string(intent.Domain), intent.EntityKey
		byID[ids[i]] = intent
	}
	rows, err := database.QueryContext(ctx, heldWorkItemsQuery,
		array.StringArray(ids), array.StringArray(scopes), array.StringArray(generations),
		array.StringArray(domains), array.StringArray(entityKeys))
	if err != nil {
		return fmt.Errorf("probe held reducer work items after a short enqueue: %w", err)
	}
	defer func() { _ = rows.Close() }()

	collision := &ReducerWorkItemCollisionError{}
	for rows.Next() {
		var sample ReducerWorkItemCollisionSample
		if err := rows.Scan(&sample.WorkItemID, &sample.StoredScopeID, &sample.StoredGenerationID, &sample.StoredDomain, &sample.StoredEntityKey); err != nil {
			return fmt.Errorf("scan held reducer work item: %w", err)
		}
		collision.Count++
		if len(collision.Samples) < reducerWorkItemCollisionSampleCap {
			incoming := byID[sample.WorkItemID]
			sample.IncomingScopeID, sample.IncomingGenerationID = incoming.ScopeID, incoming.GenerationID
			sample.IncomingDomain, sample.IncomingEntityKey = string(incoming.Domain), incoming.EntityKey
			collision.Samples = append(collision.Samples, sample)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read held reducer work items: %w", err)
	}
	if collision.Count == 0 {
		return nil
	}
	return collision
}
