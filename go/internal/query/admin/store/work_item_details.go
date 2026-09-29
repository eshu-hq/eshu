// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// workItemDetails is the part of failure_details the admin listing reads: the
// operator note an admin dead-letter or skip stored (#7388) and the prior_failure
// a supersede or an operator note kept (#7320). The prior failure's own details
// text is deliberately not decoded.
type workItemDetails struct {
	PriorFailure *struct {
		Status         string `json:"status"`
		FailureClass   string `json:"failure_class"`
		FailureMessage string `json:"failure_message"`
		UpdatedAt      string `json:"updated_at"`
	} `json:"prior_failure"`
}

// workItemNote is decoded on its own so a malformed prior_failure cannot discard
// a valid operator_note, and a malformed operator_note cannot discard a valid
// prior_failure.
type workItemNote struct {
	OperatorNote *string `json:"operator_note"`
}

// applyWorkItemDetails fills item.OperatorNote and item.PriorFailure from a
// row's failure_details. failure_details is free text or JSON, so anything that
// is not a JSON object leaves both nil and is not an error: a non-JSON row must
// never fail the listing (#7385).
func applyWorkItemDetails(item *admin.WorkItem, details sql.NullString) {
	text := strings.TrimSpace(details.String)
	if !details.Valid || text == "" || text[0] != '{' {
		return
	}
	var note workItemNote
	if err := json.Unmarshal([]byte(text), &note); err == nil {
		item.OperatorNote = note.OperatorNote
	}
	var parsed workItemDetails
	if err := json.Unmarshal([]byte(text), &parsed); err != nil || parsed.PriorFailure == nil {
		return
	}
	prior := admin.PriorFailure{
		Status:         strings.TrimSpace(parsed.PriorFailure.Status),
		FailureClass:   strings.TrimSpace(parsed.PriorFailure.FailureClass),
		FailureMessage: strings.TrimSpace(parsed.PriorFailure.FailureMessage),
		UpdatedAt:      strings.TrimSpace(parsed.PriorFailure.UpdatedAt),
	}
	if at, err := time.Parse(time.RFC3339Nano, prior.UpdatedAt); err == nil {
		prior.UpdatedAt = at.UTC().Format(time.RFC3339)
	}
	item.PriorFailure = &prior
}
