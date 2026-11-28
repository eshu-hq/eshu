// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/apilib"
)

// Mounts in this file register only proof-read routes. They do no
// authentication and take no credential parameters: the caller MUST wrap the
// mux in its auth chain before serving. There is deliberately no full-mux
// mount: the host keeps its own mux and picks exactly the families it
// serves, so omitting a family is a registration choice, not a flag inside
// a shared mount.

// MountTuningReport mounts the static shared-projection tuning report
// (GET /api/v0/admin/shared-projection/tuning-report).
func MountTuningReport(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v0/admin/shared-projection/tuning-report", WriteTuningReport)
}

// MountWorkItemsQuery mounts the work-items inspection read
// (POST /api/v0/admin/work-items/query) over store. onStoreError is the
// optional reader-verdict hook (nil skips it); see ServeWorkItemsQuery.
func MountWorkItemsQuery(mux *http.ServeMux, store WorkItemStore, onStoreError StoreErrorHandler) {
	mux.HandleFunc("POST /api/v0/admin/work-items/query", func(w http.ResponseWriter, r *http.Request) {
		ServeWorkItemsQuery(w, r, store, onStoreError)
	})
}

// MountProofReads mounts every proof-read family: the tuning report and the
// work-items query. The full ReadStore satisfies the work-items port.
func MountProofReads(mux *http.ServeMux, store ReadStore, onStoreError StoreErrorHandler) {
	MountTuningReport(mux)
	MountWorkItemsQuery(mux, store, onStoreError)
}

// MountReadiness mounts the backend-gated /readyz probe for service,
// answering from the postgres and graph probes. It is the admin-package
// alias of apilib.MountReadiness for hosts that mount everything from here.
func MountReadiness(mux *http.ServeMux, service string, postgres, graph apilib.ReadinessProbe) {
	apilib.MountReadiness(mux, service, postgres, graph)
}
