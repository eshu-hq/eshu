// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// #7008: when the topic read hits its deadline, the handler must answer the
// bounded partial rows with an explicit deadline_truncated marker (distinct
// from candidate_pool_truncated) instead of failing the call.

func TestHandleCodeTopicInvestigationDeadlineReturnsBoundedPartial(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{
		rows: []CodeTopicEvidenceRow{
			{
				SourceKind:   "entity",
				RepoID:       "repo-1",
				RelativePath: "auth.go",
				EntityID:     "entity-auth",
				EntityName:   "resolveAuth",
				EntityType:   "Function",
				Language:     "go",
				StartLine:    44,
				EndLine:      88,
				MatchedTerms: []string{"auth"},
				Score:        1,
			},
			{
				SourceKind:   "file",
				RepoID:       "repo-1",
				RelativePath: "workspace_lock.go",
				Language:     "go",
				MatchedTerms: []string{"lock"},
				Score:        1,
			},
		},
		err: fmt.Errorf("investigate code topic: %w", ErrCodeTopicDeadlineTruncated),
	}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/topics/investigate",
		bytes.NewBufferString(`{"topic":"authentication locking","repo_id":"repo-1"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", envelope.Data)
	}
	if got, want := data["deadline_truncated"], true; got != want {
		t.Fatalf("deadline_truncated = %#v, want %#v (explicit deadline marker)", got, want)
	}
	if got, want := data["candidate_pool_truncated"], false; got != want {
		t.Fatalf("candidate_pool_truncated = %#v, want %#v (deadline marker stays distinct)", got, want)
	}
	if got, want := data["count"], float64(2); got != want {
		t.Fatalf("count = %#v, want %#v bounded partial rows", got, want)
	}
	coverage, ok := data["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage type = %T, want map", data["coverage"])
	}
	if got, want := coverage["deadline_truncated"], true; got != want {
		t.Fatalf("coverage.deadline_truncated = %#v, want %#v", got, want)
	}
	if got, want := coverage["candidate_pool_truncated"], false; got != want {
		t.Fatalf("coverage.candidate_pool_truncated = %#v, want %#v", got, want)
	}
	packet := requireAnswerPacketCompanion(t, data, "code.topic")
	limitations, ok := packet["limitations"].([]any)
	if !ok || len(limitations) == 0 {
		t.Fatalf("answer_packet.limitations = %#v, want a deadline-partial limitation", packet["limitations"])
	}
	// The two gathered rows fit the page (truncated=false), yet the packet
	// must still read partial: the deadline cut the evidence set short.
	if got, want := packet["partial"], true; got != want {
		t.Fatalf("answer_packet.partial = %#v, want %#v for a bounded deadline partial", got, want)
	}
}

// A non-deadline read failure stays fail-closed even when the store reports
// rows alongside the error: only the deadline marker unlocks the partial.
func TestHandleCodeTopicInvestigationFaultStaysErrorOnly(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{
		rows: []CodeTopicEvidenceRow{
			{
				SourceKind:   "entity",
				RepoID:       "repo-1",
				RelativePath: "auth.go",
				EntityID:     "entity-auth",
				Score:        1,
			},
		},
		err: errors.New("connection reset mid result"),
	}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/topics/investigate",
		bytes.NewBufferString(`{"topic":"authentication","repo_id":"repo-1"}`),
	)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

// codeTopicRows is the only unlock for a store-side partial: the deadline
// marker error carries the gathered rows, while every other failure drops
// them so a truncated result is never read as complete.
func TestCodeTopicRowsKeepsPartialOnlyOnDeadline(t *testing.T) {
	t.Parallel()

	rows := []CodeTopicEvidenceRow{
		{
			SourceKind:   "entity",
			RepoID:       "repo-1",
			RelativePath: "auth.go",
			EntityID:     "entity-auth",
			Score:        1,
		},
	}
	cases := []struct {
		name         string
		err          error
		wantRows     int
		wantDeadline bool
	}{
		{
			name:         "deadline",
			err:          fmt.Errorf("investigate code topic: %w", ErrCodeTopicDeadlineTruncated),
			wantRows:     1,
			wantDeadline: true,
		},
		{
			name:         "fault",
			err:          errors.New("connection reset mid result"),
			wantRows:     0,
			wantDeadline: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &topicInvestigationContentStore{rows: rows, err: tc.err}
			handler := &CodeHandler{Content: store}
			got, err := handler.codeTopicRows(context.Background(), CodeTopicInvestigationRequest{
				Topic: "authentication",
				Limit: 25,
				Terms: []string{"authentication"},
			})
			if err == nil {
				t.Fatal("codeTopicRows() error = nil, want the store failure")
			}
			if gotDeadline := errors.Is(err, ErrCodeTopicDeadlineTruncated); gotDeadline != tc.wantDeadline {
				t.Fatalf("deadline marker = %v, want %v (error = %v)", gotDeadline, tc.wantDeadline, err)
			}
			if len(got) != tc.wantRows {
				t.Fatalf("len(rows) = %d, want %d", len(got), tc.wantRows)
			}
		})
	}
}
