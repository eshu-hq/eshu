// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"strings"
	"testing"
)

func TestNormalizeGitHubActions(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, event, payload, runID, jobID, wantErr string }{
		{name: "workflow run", event: "workflow_run", payload: `{"action":"completed","repository":{"id":42,"full_name":"Example/Repo"},"workflow_run":{"id":123}}`, runID: "123"},
		{name: "workflow job", event: "workflow_job", payload: `{"action":"queued","repository":{"id":42,"full_name":"Example/Repo"},"workflow_job":{"id":88,"run_id":123}}`, runID: "123", jobID: "88"},
		{name: "missing run", event: "workflow_job", payload: `{"action":"queued","repository":{"id":42,"full_name":"Example/Repo"},"workflow_job":{"id":88}}`, wantErr: "workflow_job.run_id"},
		{name: "zero repository id", event: "workflow_run", payload: `{"action":"completed","repository":{"id":0,"full_name":"Example/Repo"},"workflow_run":{"id":123}}`, wantErr: "repository.id must be a positive integer"},
		{name: "zero workflow run id", event: "workflow_run", payload: `{"action":"completed","repository":{"id":42,"full_name":"Example/Repo"},"workflow_run":{"id":0}}`, wantErr: "workflow_run.id must be a positive integer"},
		{name: "zero workflow job run id", event: "workflow_job", payload: `{"action":"queued","repository":{"id":42,"full_name":"Example/Repo"},"workflow_job":{"id":88,"run_id":0}}`, wantErr: "workflow_job.run_id must be a positive integer"},
		{name: "zero workflow job id", event: "workflow_job", payload: `{"action":"queued","repository":{"id":42,"full_name":"Example/Repo"},"workflow_job":{"id":0,"run_id":123}}`, wantErr: "workflow_job.id must be a positive integer"},
		{name: "invalid repository", event: "workflow_run", payload: `{"action":"completed","repository":{"id":42,"full_name":"elsewhere"},"workflow_run":{"id":123}}`, wantErr: "repository"},
		{name: "floating run", event: "workflow_run", payload: `{"action":"completed","repository":{"id":42,"full_name":"Example/Repo"},"workflow_run":{"id":1.5}}`, wantErr: "workflow_run.id"},
		{name: "unsupported event", event: "push", payload: `{}`, wantErr: "unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizeGitHubActions(tt.event, "delivery-1", []byte(tt.payload))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error=%v,want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Repository != "example/repo" || got.RunID != tt.runID || got.JobID != tt.jobID {
				t.Fatalf("trigger=%+v", got)
			}
		})
	}
}
