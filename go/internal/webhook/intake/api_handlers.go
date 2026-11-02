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
	obs := s.observe(r, valueUnknown)
	defer obs.finish()
	r = obs.bind(r)
	var request RepositoryIntakeRequest
	if !s.decodeRequest(w, r, obs, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, obs, "repository", err)
		return
	}
	obs.provider = string(trigger.Provider)
	d := triggerDecision{
		provider:  obs.provider,
		eventKind: string(trigger.EventKind),
		decision:  string(trigger.Decision),
		reason:    boundedDecisionReason(trigger.Reason),
	}
	if s.stores.Repository == nil {
		obs.storeUnavailable(d)
		s.writeStoreError(w, r, "repository")
		return
	}
	storeCtx, storeStarted := obs.beginStore(d)
	stored, err := s.stores.Repository.StoreTrigger(storeCtx, trigger, s.now())
	if err != nil {
		obs.storeFailed(d, storeStarted)
		s.writeStoreFailure(w, r, "repository", string(trigger.Provider), err)
		return
	}
	obs.stored(d, storeStarted, string(stored.Status))
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
	obs := s.observe(r, valueUnknown)
	defer obs.finish()
	r = obs.bind(r)
	var request IncidentIntakeRequest
	if !s.decodeRequest(w, r, obs, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, obs, "incident", err)
		return
	}
	obs.provider = string(trigger.Provider)
	d := triggerDecision{
		provider:  obs.provider,
		eventKind: boundedIncidentEventKind(trigger.EventKind),
		decision:  string(webhook.DecisionAccepted),
		reason:    reasonNone,
	}
	if s.stores.Incident == nil {
		obs.storeUnavailable(d)
		s.writeStoreError(w, r, "incident")
		return
	}
	storeCtx, storeStarted := obs.beginStore(d)
	stored, err := s.stores.Incident.StoreIncidentFreshnessTrigger(storeCtx, trigger, s.now())
	if err != nil {
		obs.storeFailed(d, storeStarted)
		s.writeStoreFailure(w, r, "incident", string(trigger.Provider), err)
		return
	}
	obs.stored(d, storeStarted, string(stored.Status))
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
	obs := s.observe(r, providerAWSFreshness)
	defer obs.finish()
	r = obs.bind(r)
	var request AWSIntakeRequest
	if !s.decodeRequest(w, r, obs, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, obs, "aws", err)
		return
	}
	d := triggerDecision{
		provider:  providerAWSFreshness,
		eventKind: string(trigger.Kind),
		decision:  string(webhook.DecisionAccepted),
		reason:    reasonNone,
	}
	if s.stores.AWS == nil {
		obs.storeUnavailable(d)
		s.writeStoreError(w, r, "aws")
		return
	}
	storeCtx, storeStarted := obs.beginStore(d)
	stored, err := s.stores.AWS.StoreTrigger(storeCtx, trigger, s.now())
	if err != nil {
		obs.storeFailed(d, storeStarted)
		s.writeStoreFailure(w, r, "aws", "aws", err)
		return
	}
	obs.stored(d, storeStarted, string(stored.Status))
	obs.freshnessStored(d, stored.DuplicateCount)
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
	obs := s.observe(r, providerGCPFreshness)
	defer obs.finish()
	r = obs.bind(r)
	var request GCPIntakeRequest
	if !s.decodeRequest(w, r, obs, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, obs, "gcp", err)
		return
	}
	d := triggerDecision{
		provider:  providerGCPFreshness,
		eventKind: string(trigger.Kind),
		decision:  string(webhook.DecisionAccepted),
		reason:    reasonNone,
	}
	if s.stores.GCP == nil {
		obs.storeUnavailable(d)
		s.writeStoreError(w, r, "gcp")
		return
	}
	storeCtx, storeStarted := obs.beginStore(d)
	stored, err := s.stores.GCP.StoreTrigger(storeCtx, trigger, s.now())
	if err != nil {
		obs.storeFailed(d, storeStarted)
		s.writeStoreFailure(w, r, "gcp", "gcp", err)
		return
	}
	obs.stored(d, storeStarted, string(stored.Status))
	obs.freshnessStored(d, stored.DuplicateCount)
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
	obs := s.observe(r, string(webhook.ProviderGitHub))
	defer obs.finish()
	r = obs.bind(r)
	var request ActionsIntakeRequest
	if !s.decodeRequest(w, r, obs, &request) {
		return
	}
	trigger, err := request.toTrigger()
	if err != nil {
		s.writeTriggerError(w, r, obs, "actions", err)
		return
	}
	d := triggerDecision{
		provider:  string(webhook.ProviderGitHub),
		eventKind: trigger.EventKind,
		decision:  string(webhook.DecisionAccepted),
		reason:    reasonNone,
	}
	if s.stores.Actions == nil {
		obs.storeUnavailable(d)
		s.writeStoreError(w, r, "actions")
		return
	}
	storeCtx, storeStarted := obs.beginStore(d)
	stored, err := s.stores.Actions.Store(storeCtx, trigger, s.now())
	if err != nil {
		if errors.Is(err, webhook.ErrActionsDeliveryConflict) {
			obs.storeRejected(d.provider, storeStarted)
			s.logOutcome(r, "intake delivery conflict", slog.String("route", "actions"))
			writeIntakeJSON(w, http.StatusConflict, map[string]any{
				"api_version": APIVersionV1,
				"code":        codeDeliveryConflict,
				"error":       "conflicting actions delivery",
			})
			return
		}
		obs.storeFailed(d, storeStarted)
		s.writeStoreFailure(w, r, "actions", "github", err)
		return
	}
	obs.stored(d, storeStarted, string(stored.Status))
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
