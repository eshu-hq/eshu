// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ClaimPlaneFencedCommitter is implemented by a committer that can report
// whether it fences claimed commits against the extracted claim plane
// (#9376). postgres.IngestionStore implements it; a wrapping committer
// forwards it with CommitterFencesClaimPlane.
type ClaimPlaneFencedCommitter interface {
	ClaimPlaneFenced() bool
}

// CommitterFencesClaimPlane reports whether committer fences claimed commits
// against the claim plane. A committer that does not implement
// ClaimPlaneFencedCommitter uses the in-tree fence.
func CommitterFencesClaimPlane(committer Committer) bool {
	fenced, ok := committer.(ClaimPlaneFencedCommitter)
	return ok && fenced.ClaimPlaneFenced()
}

// validateClaimPlaneFence refuses a control store and committer that fence
// different claim stores (#9376 D8). A claim the claim plane holds has no
// core claim row, so an in-tree committer would fail every commit's core
// heartbeat; a committer that pins on the plane would have every core-held
// claim refused as not_found.
func validateClaimPlaneFence(controlStore ClaimControlStore, committer Committer) error {
	_, planeStore := controlStore.(*workflow.ClaimPlaneClient)
	fenced := CommitterFencesClaimPlane(committer)
	switch {
	case planeStore && !fenced:
		return errors.New("claim-plane control store requires a committer with the claim-plane commit fence " +
			"(postgres.IngestionStore.ClaimPlane with the claim-plane client as Pinner)")
	case !planeStore && fenced:
		return errors.New("committer pins claims on the claim plane but the control store is the core Postgres control store")
	}
	return nil
}
