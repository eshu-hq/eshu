// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/webhook"
)

func (s *Server) handleRepository(w http.ResponseWriter, r *http.Request) {
	var request RepositoryIntakeRequest
	if !s.decodeRequest(w, r, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, "repository", err)
		return
	}
	if s.stores.Repository == nil {
		s.writeStoreError(w, r, "repository")
		return
	}
	stored, err := s.stores.Repository.StoreTrigger(r.Context(), trigger, s.now())
	if err != nil {
		s.writeStoreFailure(w, r, "repository", string(trigger.Provider), err)
		return
	}
	s.logOutcome(r, "intake trigger stored",
		slog.String("route", "repository"),
		slog.String("provider", string(trigger.Provider)),
		slog.String("status", string(stored.Status)),
	)
	writeIntakeJSON(w, http.StatusAccepted, map[string]any{
		"api_version":     APIVersionV1,
		"trigger_id":      stored.TriggerID,
		"status":          stored.Status,
		"decision":        stored.Decision,
		"reason":          stored.Reason,
		"duplicate":       stored.DuplicateCount > 0,
		"duplicate_count": stored.DuplicateCount,
	})
}

func (s *Server) handleIncident(w http.ResponseWriter, r *http.Request) {
	var request IncidentIntakeRequest
	if !s.decodeRequest(w, r, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, "incident", err)
		return
	}
	if s.stores.Incident == nil {
		s.writeStoreError(w, r, "incident")
		return
	}
	stored, err := s.stores.Incident.StoreIncidentFreshnessTrigger(r.Context(), trigger, s.now())
	if err != nil {
		s.writeStoreFailure(w, r, "incident", string(trigger.Provider), err)
		return
	}
	s.logOutcome(r, "intake trigger stored",
		slog.String("route", "incident"),
		slog.String("provider", string(trigger.Provider)),
		slog.String("status", string(stored.Status)),
	)
	writeIntakeJSON(w, http.StatusAccepted, map[string]any{
		"api_version":     APIVersionV1,
		"trigger_id":      stored.TriggerID,
		"status":          stored.Status,
		"duplicate":       stored.DuplicateCount > 0,
		"duplicate_count": stored.DuplicateCount,
	})
}

func (s *Server) handleAWS(w http.ResponseWriter, r *http.Request) {
	var request AWSIntakeRequest
	if !s.decodeRequest(w, r, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, "aws", err)
		return
	}
	if s.stores.AWS == nil {
		s.writeStoreError(w, r, "aws")
		return
	}
	stored, err := s.stores.AWS.StoreTrigger(r.Context(), trigger, s.now())
	if err != nil {
		s.writeStoreFailure(w, r, "aws", "aws", err)
		return
	}
	s.logOutcome(r, "intake trigger stored",
		slog.String("route", "aws"),
		slog.String("status", string(stored.Status)),
	)
	writeIntakeJSON(w, http.StatusAccepted, map[string]any{
		"api_version":     APIVersionV1,
		"trigger_id":      stored.TriggerID,
		"status":          stored.Status,
		"kind":            stored.Kind,
		"duplicate":       stored.DuplicateCount > 0,
		"duplicate_count": stored.DuplicateCount,
	})
}

func (s *Server) handleGCP(w http.ResponseWriter, r *http.Request) {
	var request GCPIntakeRequest
	if !s.decodeRequest(w, r, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, "gcp", err)
		return
	}
	if s.stores.GCP == nil {
		s.writeStoreError(w, r, "gcp")
		return
	}
	stored, err := s.stores.GCP.StoreTrigger(r.Context(), trigger, s.now())
	if err != nil {
		s.writeStoreFailure(w, r, "gcp", "gcp", err)
		return
	}
	s.logOutcome(r, "intake trigger stored",
		slog.String("route", "gcp"),
		slog.String("status", string(stored.Status)),
	)
	writeIntakeJSON(w, http.StatusAccepted, map[string]any{
		"api_version":     APIVersionV1,
		"trigger_id":      stored.TriggerID,
		"status":          stored.Status,
		"kind":            stored.Kind,
		"duplicate":       stored.DuplicateCount > 0,
		"duplicate_count": stored.DuplicateCount,
	})
}

func (s *Server) handleActions(w http.ResponseWriter, r *http.Request) {
	var request ActionsIntakeRequest
	if !s.decodeRequest(w, r, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, "actions", err)
		return
	}
	if s.stores.Actions == nil {
		s.writeStoreError(w, r, "actions")
		return
	}
	stored, err := s.stores.Actions.Store(r.Context(), trigger, s.now())
	if err != nil {
		if errors.Is(err, webhook.ErrActionsDeliveryConflict) {
			s.logOutcome(r, "intake delivery conflict", slog.String("route", "actions"))
			writeIntakeJSON(w, http.StatusConflict, map[string]any{
				"api_version": APIVersionV1,
				"code":        codeDeliveryConflict,
				"error":       "conflicting actions delivery",
			})
			return
		}
		s.writeStoreFailure(w, r, "actions", "github", err)
		return
	}
	s.logOutcome(r, "intake trigger stored",
		slog.String("route", "actions"),
		slog.String("status", string(stored.Status)),
	)
	writeIntakeJSON(w, http.StatusAccepted, map[string]any{
		"api_version": APIVersionV1,
		"status":      stored.Status,
		"decision":    webhook.DecisionAccepted,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeIntakeJSON(w, http.StatusOK, map[string]any{
		"api_version": APIVersionV1,
		"status":      "ok",
	})
}
