// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrActionsDeliveryConflict marks a verified retry whose delivery ID already
// belongs to a different immutable Actions identity.
var ErrActionsDeliveryConflict = errors.New("conflicting delivery identity")

// ActionsTrigger is a verified GitHub workflow delivery used only to wake
// authoritative, bounded CI/CD run collection. It is not graph evidence.
type ActionsTrigger struct {
	DeliveryID   string
	EventKind    string
	Action       string
	RepositoryID string
	Repository   string
	RunID        string
	JobID        string
}

// StoredActionsTrigger is one durable delivery with its fenced handoff lease.
type StoredActionsTrigger struct {
	ActionsTrigger
	// HandoffKey is assigned once by the first successful claim and stays stable
	// across release, lease expiry, and reclaim.
	HandoffKey        string
	Status            TriggerStatus
	ReceivedAt        time.Time
	UpdatedAt         time.Time
	ClaimExpiresAt    time.Time
	ClaimFencingToken int64
	ClaimAttempts     int
}

// ActionsReapResult counts expired delivery leases returned to the queue or
// failed after exhausting their claim budget. The counts reflect rows returned
// by a successful reap query without exposing delivery identities.
type ActionsReapResult struct {
	Requeued  int
	Exhausted int
}

// NormalizeGitHubActions extracts a bounded wake-up identity after its caller
// has verified GitHub's SHA-256 signature. Unsupported events fail closed.
func NormalizeGitHubActions(event, deliveryID string, payload []byte) (ActionsTrigger, error) {
	event = strings.TrimSpace(event)
	if event != "workflow_run" && event != "workflow_job" {
		return ActionsTrigger{}, fmt.Errorf("unsupported github actions event %q", event)
	}
	deliveryID = strings.TrimSpace(deliveryID)
	if deliveryID == "" || len(deliveryID) > 256 {
		return ActionsTrigger{}, errors.New("github actions delivery_id must contain 1..256 bytes")
	}
	var decoded struct {
		Action     string `json:"action"`
		Repository struct {
			ID       json.RawMessage `json:"id"`
			FullName string          `json:"full_name"`
		} `json:"repository"`
		WorkflowRun struct {
			ID json.RawMessage `json:"id"`
		} `json:"workflow_run"`
		WorkflowJob struct {
			ID    json.RawMessage `json:"id"`
			RunID json.RawMessage `json:"run_id"`
		} `json:"workflow_job"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return ActionsTrigger{}, fmt.Errorf("decode github actions event: %w", err)
	}
	repository := strings.ToLower(strings.Trim(strings.TrimSpace(decoded.Repository.FullName), "/"))
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(repository) > 255 {
		return ActionsTrigger{}, errors.New("github actions repository must be owner/name")
	}
	repoID, err := actionsNumericID("repository.id", decoded.Repository.ID)
	if err != nil {
		return ActionsTrigger{}, err
	}
	action := strings.TrimSpace(decoded.Action)
	if action == "" || len(action) > 64 {
		return ActionsTrigger{}, errors.New("github actions action must contain 1..64 bytes")
	}
	trigger := ActionsTrigger{DeliveryID: deliveryID, EventKind: event, Action: action, RepositoryID: repoID, Repository: repository}
	if event == "workflow_run" {
		trigger.RunID, err = actionsNumericID("workflow_run.id", decoded.WorkflowRun.ID)
	} else {
		trigger.RunID, err = actionsNumericID("workflow_job.run_id", decoded.WorkflowJob.RunID)
		if err == nil {
			trigger.JobID, err = actionsNumericID("workflow_job.id", decoded.WorkflowJob.ID)
		}
	}
	if err != nil {
		return ActionsTrigger{}, err
	}
	return trigger, nil
}

func actionsNumericID(name string, raw json.RawMessage) (string, error) {
	if len(raw) == 0 || raw[0] < '0' || raw[0] > '9' {
		return "", fmt.Errorf("github actions %s must be a positive integer", name)
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("github actions %s must be a positive integer", name)
	}
	return strconv.FormatInt(id, 10), nil
}
