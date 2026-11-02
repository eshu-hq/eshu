// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fencedStubCommitter is a claimed committer that reports whether it fences
// claimed commits against the extracted claim plane.
type fencedStubCommitter struct {
	stubClaimedCommitter
	fenced bool
}

func (c *fencedStubCommitter) ClaimPlaneFenced() bool { return c.fenced }

// TestClaimedServiceRefusesAMismatchedClaimPlaneFence is the #9376 D8
// construction check: a claim-plane control store needs a committer that
// pins on the plane (otherwise every commit heartbeats a core claim row that
// does not exist), and a committer that pins on the plane needs a claim-plane
// control store (otherwise every in-tree claim is refused as not_found).
func TestClaimedServiceRefusesAMismatchedClaimPlaneFence(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.November, 2, 12, 0, 0, 0, time.UTC)
	plane := workflow.NewClaimPlaneClient("http://eshu-workflow-claim-plane:8080", nil)
	for _, tc := range []struct {
		name      string
		plane     bool
		committer Committer
		want      string
	}{
		{"plane store, unfenced committer", true, &stubClaimedCommitter{}, "claim-plane control store requires"},
		{"plane store, fence without pinner", true, &fencedStubCommitter{fenced: false}, "claim-plane control store requires"},
		{"plane store, fenced committer", true, &fencedStubCommitter{fenced: true}, ""},
		{"core store, fenced committer", false, &fencedStubCommitter{fenced: true}, "core Postgres control store"},
		{"core store, unfenced committer", false, &fencedStubCommitter{fenced: false}, ""},
		{"core store, plain committer", false, &stubClaimedCommitter{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service := testClaimedService(now, workflow.Claim{ClaimID: "claim-1"}, scope.CollectorGit, &stubClaimStore{}, nil, tc.committer)
			service.Source = &itemRecordingClaimedSource{}
			if tc.plane {
				service.ControlStore = plane
			}
			err := service.validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("validate() error = %v, want one containing %q", err, tc.want)
			}
			if runErr := service.Run(context.Background()); runErr == nil || !strings.Contains(runErr.Error(), tc.want) {
				t.Fatalf("Run() error = %v, want the same refusal before any claim", runErr)
			}
		})
	}
}
