// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/claim/fence"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimPlaneBinding shares one dialed claim-plane client between the
// commit handler control store and the ingestion fence pinner. Both
// faces must hold the same instance: the fence pins the claim the
// control store fenced first, so splitting them across two clients
// would pin under one epoch and commit under another.
type ClaimPlaneBinding struct {
	ControlStore collector.ClaimControlStore
	Pinner       fence.Pinner
}

// DialClaimPlane builds the claim-plane client backing both faces of a
// ClaimPlaneBinding. A nil httpClient keeps the client default. The
// constructor lives here, next to the commit handler wiring, so API
// commands stay free of workflow-state references.
func DialClaimPlane(baseURL string, httpClient *http.Client) ClaimPlaneBinding {
	plane := workflow.NewClaimPlaneClient(baseURL, httpClient)
	return ClaimPlaneBinding{ControlStore: plane, Pinner: plane}
}
