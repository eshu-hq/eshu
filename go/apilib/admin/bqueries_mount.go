// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"
)

// B-query mounts register one proof-read family each. They do no
// authentication and take no credential parameters: the caller MUST wrap
// the mux in its auth chain before serving. Each family keeps its own
// Mount* so a host picks exactly the families it serves; they stay out of
// MountProofReads until the core OmitProofReads surface covers them, so one
// mux never answers a route twice.

// MountDeadLettersQuery mounts the dead-letter inspection read
// (POST /api/v0/admin/dead-letters/query) over store. onStoreError is the
// optional reader-verdict hook (nil skips it); see ServeDeadLettersQuery.
func MountDeadLettersQuery(mux *http.ServeMux, store DeadLetterStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("POST /api/v0/admin/dead-letters/query", func(w http.ResponseWriter, r *http.Request) {
		ServeDeadLettersQuery(w, r, store, onStoreError)
	})
}

// MountDecisionsQuery mounts the projection-decision inspection read
// (POST /api/v0/admin/decisions/query) over store. onStoreError is the
// optional reader-verdict hook (nil skips it); see ServeDecisionsQuery.
func MountDecisionsQuery(mux *http.ServeMux, store DecisionStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("POST /api/v0/admin/decisions/query", func(w http.ResponseWriter, r *http.Request) {
		ServeDecisionsQuery(w, r, store, onStoreError)
	})
}

// MountInputInvalidFactsQuery mounts the reducer_input_invalid_facts
// inspection read (POST /api/v0/admin/input-invalid-facts/query) over
// store. onStoreError is the optional reader-verdict hook (nil skips it);
// see ServeInputInvalidFactsQuery.
func MountInputInvalidFactsQuery(mux *http.ServeMux, store InputInvalidStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("POST /api/v0/admin/input-invalid-facts/query", func(w http.ResponseWriter, r *http.Request) {
		ServeInputInvalidFactsQuery(w, r, store, onStoreError)
	})
}

// MountPoisonedLinksQuery mounts the changed-since poisoned/retrying link
// inspection read (POST /api/v0/admin/changed-since/poisoned-links/query)
// over store. onStoreError is the optional reader-verdict hook (nil skips
// it); see ServePoisonedLinksQuery.
func MountPoisonedLinksQuery(mux *http.ServeMux, store PoisonedLinkStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("POST /api/v0/admin/changed-since/poisoned-links/query", func(w http.ResponseWriter, r *http.Request) {
		ServePoisonedLinksQuery(w, r, store, onStoreError)
	})
}

// MountReplayEventsQuery mounts the replay-event audit read
// (POST /api/v0/admin/replay-events/query) over store. onStoreError is the
// optional reader-verdict hook (nil skips it); see ServeReplayEventsQuery.
func MountReplayEventsQuery(mux *http.ServeMux, store ReplayEventStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("POST /api/v0/admin/replay-events/query", func(w http.ResponseWriter, r *http.Request) {
		ServeReplayEventsQuery(w, r, store, onStoreError)
	})
}

// MountReindexProgress mounts the fleet reindex-progress read
// (GET /api/v0/admin/reindex/progress) over store. onStoreError is the
// optional reader-verdict hook (nil skips it); see ServeReindexProgress.
// It stays out of MountProofReads until the core OmitProofReads surface
// covers it, so one mux never answers a route twice.
func MountReindexProgress(mux *http.ServeMux, store MaintenanceStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("GET /api/v0/admin/reindex/progress", func(w http.ResponseWriter, r *http.Request) {
		ServeReindexProgress(w, r, store, onStoreError)
	})
}

// MountDeltaUnionCompleteness mounts the read-only delta-union
// completeness check (GET /api/v0/admin/delta-union/completeness) over
// store. onStoreError is the optional reader-verdict hook (nil skips it);
// see ServeDeltaUnionCompleteness. It stays out of MountProofReads until
// the core OmitProofReads surface covers it, so one mux never answers a
// route twice.
func MountDeltaUnionCompleteness(mux *http.ServeMux, store DeltaUnionStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("GET /api/v0/admin/delta-union/completeness", func(w http.ResponseWriter, r *http.Request) {
		ServeDeltaUnionCompleteness(w, r, store, onStoreError)
	})
}
