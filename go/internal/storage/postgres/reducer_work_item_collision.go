// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
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
// a pure function of the intent, so a retry collides again. Detection compares
// the stored scope, generation and domain columns, and the stored entity key only
// when it reproduces the row's own id (heldIsDifferentIntent).
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
// that a queued row already holds. A primary-key probe; it reads no payload but
// the entity key. Go, not SQL, decides which held rows are a different intent
// (heldIsDifferentIntent), so the comparison trims exactly as reducerWorkItemID
// does.
const heldWorkItemsQuery = `
SELECT w.work_item_id, w.scope_id, w.generation_id, w.domain, COALESCE(w.payload ->> 'entity_key', '')
FROM fact_work_items AS w
WHERE w.work_item_id = ANY($1::text[])`

// heldIsDifferentIntent reports whether a held row belongs to an intent other
// than incoming. The scope, generation and domain columns are always stored, so
// any difference there is a different intent. The entity key lives only in the
// payload, and the payload is not a trustworthy identity on its own: enqueue
// writes the intent's key, but other writers of reducer rows (refinalize, the
// freshness reproject, seeds), older rows and test fixtures store no key or an
// unrelated one. A stored key therefore counts only when it reproduces the row's
// own work item id; then it is the key the id was built from, and a different
// incoming key under that id is the alias #11215 describes. Anything else says
// nothing about the intent and is never a difference, because refusing a
// legitimate duplicate would dead-letter a healthy item. Every part is compared
// trimmed, the way reducerWorkItemID builds the id, so a spelling that differs
// only by surrounding whitespace is a plain duplicate, not a collision.
func heldIsDifferentIntent(stored ReducerWorkItemCollisionSample, incoming runtime.ReducerIntent) bool {
	if strings.TrimSpace(stored.StoredScopeID) != strings.TrimSpace(incoming.ScopeID) ||
		strings.TrimSpace(stored.StoredGenerationID) != strings.TrimSpace(incoming.GenerationID) ||
		strings.TrimSpace(stored.StoredDomain) != strings.TrimSpace(string(incoming.Domain)) {
		return true
	}
	storedKey := strings.TrimSpace(stored.StoredEntityKey)
	if storedKey == "" || storedKey == strings.TrimSpace(incoming.EntityKey) {
		return false
	}
	reproduces := reducerWorkItemID(runtime.ReducerIntent{
		ScopeID:      stored.StoredScopeID,
		GenerationID: stored.StoredGenerationID,
		Domain:       reducercontract.Domain(stored.StoredDomain),
		EntityKey:    stored.StoredEntityKey,
	}) == stored.WorkItemID
	return reproduces
}

// firstDifferingIntent returns the first incoming intent of a held row's id that
// is a different intent from the held row (heldIsDifferentIntent).
func firstDifferingIntent(stored ReducerWorkItemCollisionSample, incoming []runtime.ReducerIntent) (runtime.ReducerIntent, bool) {
	for _, intent := range incoming {
		if heldIsDifferentIntent(stored, intent) {
			return intent, true
		}
	}
	return runtime.ReducerIntent{}, false
}

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
	// A batch can hold several intents with one id (a duplicate, or an alias of
	// another intent), so every incoming intent per id is kept: a map of one
	// intent per id would compare the held row with only the last of them.
	byID := make(map[string][]runtime.ReducerIntent, len(batch))
	for i, intent := range batch {
		ids[i] = reducerWorkItemID(intent)
		byID[ids[i]] = append(byID[ids[i]], intent)
	}
	rows, err := database.QueryContext(ctx, heldWorkItemsQuery, array.StringArray(ids))
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
		// One held row is one collision, however many incoming intents share its
		// id; the sample names the first incoming intent that differs from it.
		incoming, differs := firstDifferingIntent(sample, byID[sample.WorkItemID])
		if !differs {
			continue
		}
		collision.Count++
		if len(collision.Samples) < reducerWorkItemCollisionSampleCap {
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
