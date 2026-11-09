// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
)

// replayAdvisoryGroupWinners is the fixed winners read model the
// advisory-group replay case reconciles. The watermark equals the replay
// intent's enqueue time so the handler's watermark gate passes without
// depending on the wall clock.
type replayAdvisoryGroupWinners struct{}

func (replayAdvisoryGroupWinners) QualifyingAdvisoryGroupWinners(_ context.Context, groupID string) ([]advisorygroup.WinnerRow, error) {
	if groupID != "CVE-2026-0001" {
		return nil, nil
	}
	return []advisorygroup.WinnerRow{
		{
			RepositoryID: "repo-a", PackageID: "pkg:npm/lib@1", Ecosystem: "npm",
			PURL: "pkg:npm/lib@1.0.0", CVEID: "CVE-2026-0001", AdvisoryID: "GHSA-aaaa",
			Members: []string{"GHSA-aaaa", "DEBIAN-CVE-2026-0001"},
		},
		{
			RepositoryID: "repo-b", PackageID: "pkg:deb/lib", Ecosystem: "deb",
			PURL: "pkg:deb/debian/lib@2", AdvisoryID: "DEBIAN-CVE-2026-0001",
		},
	}, nil
}

func (replayAdvisoryGroupWinners) AdvisoryGroupWinnersWatermark(_ context.Context) (time.Time, error) {
	return time.Date(2026, time.June, 28, 12, 0, 0, 0, time.UTC), nil
}

// replayRecordingAdvisoryGroupWriter records every desired group state
// the handler reconciles so the replay case can compare the node
// membership and edge set across runs.
type replayRecordingAdvisoryGroupWriter struct {
	groups []advisorygroup.Group
}

func (w *replayRecordingAdvisoryGroupWriter) ReconcileAdvisoryGroup(_ context.Context, group advisorygroup.Group) (advisorygroup.WriteStats, error) {
	w.groups = append(w.groups, group)
	return advisorygroup.WriteStats{}, nil
}

// advisoryGroupReplayCase proves replaying the same advisory-group intent
// against the same winners read model reconciles byte-identical desired
// state: the same AdvisoryGroup membership lists and the same
// AFFECTED_BY edge set under the same (repository, package) keys
// (#10411). The writer's reconcile is a replace-set MERGE, so identical
// desired state is what makes a replayed claim converge instead of
// drifting.
func advisoryGroupReplayCase() idempotencyReplayCase {
	return idempotencyReplayCase{
		domain: DomainAdvisoryGroupProjection,
		run: func(t *testing.T) []idempotencyRow {
			t.Helper()
			writer := &replayRecordingAdvisoryGroupWriter{}
			handler := advisorygroup.Handler{
				Winners: replayAdvisoryGroupWinners{},
				Writer:  writer,
			}
			intent := replayIntent(DomainAdvisoryGroupProjection, "scope-vuln", []string{advisorygroup.EntityKey("CVE-2026-0001")})
			if _, err := handler.Handle(drainContext(), intent); err != nil {
				t.Fatalf("advisory group Handle: %v", err)
			}
			var out []idempotencyRow
			for _, group := range writer.groups {
				out = append(out, idempotencyRow{
					identity: "advisory_group:" + group.ID,
					contents: fmt.Sprintf("cves=%s advisories=%s",
						strings.Join(group.CVEIDs, ","), strings.Join(group.AdvisoryIDs, ",")),
				})
				for _, edge := range group.Edges {
					out = append(out, idempotencyRow{
						identity: fmt.Sprintf("%s|%s|%s", group.ID, edge.RepositoryID, edge.PackageID),
						contents: fmt.Sprintf("ecosystem=%s purl=%s", edge.Ecosystem, edge.PURL),
					})
				}
			}
			return out
		},
	}
}
