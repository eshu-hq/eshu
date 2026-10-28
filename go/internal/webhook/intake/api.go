// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// Sentinel errors classify intake rejections for status mapping. The API
// never exposes store internals: validation failures are 400, store
// failures are 500, and only the delivery-conflict identity case is 409.
var (
	errUnsupportedAPIVersion = errors.New("unsupported api_version")
	errUnsupportedProvider   = errors.New("unsupported provider for this intake route")
	errInvalidTrigger        = errors.New("invalid trigger")
)

// Error codes in machine-readable rejection bodies.
const (
	codeUnsupportedAPIVersion = "unsupported_api_version"
	codeUnsupportedProvider   = "unsupported_provider"
	codeInvalidTrigger        = "invalid_trigger"
	codeDeliveryConflict      = "delivery_conflict"
	codeStoreFailed           = "store_failed"
)

// defaultMaxIntakeBodyBytes caps one intake request body at 1 MiB, matching
// the provider-facing webhook listener cap.
const defaultMaxIntakeBodyBytes = int64(1 << 20)

// RepositoryStore persists normalized repository triggers. Core owns this
// storage; the intake API is its only writer on the extracted path.
type RepositoryStore interface {
	StoreTrigger(context.Context, webhook.Trigger, time.Time) (webhook.StoredTrigger, error)
}

// IncidentStore persists normalized incident freshness triggers.
type IncidentStore interface {
	StoreIncidentFreshnessTrigger(context.Context, webhook.IncidentFreshnessTrigger, time.Time) (webhook.StoredIncidentFreshnessTrigger, error)
}

// AWSStore persists normalized AWS freshness triggers.
type AWSStore interface {
	StoreTrigger(context.Context, awsfreshness.Trigger, time.Time) (awsfreshness.StoredTrigger, error)
}

// GCPStore persists normalized GCP freshness triggers.
type GCPStore interface {
	StoreTrigger(context.Context, gcpfreshness.Trigger, time.Time) (gcpfreshness.StoredTrigger, error)
}

// ActionsStore persists verified GitHub Actions wake-up identities.
type ActionsStore interface {
	Store(context.Context, webhook.ActionsTrigger, time.Time) (webhook.StoredActionsTrigger, error)
}

// Stores bundles the core durable stores behind the intake API.
type Stores struct {
	Repository RepositoryStore
	Incident   IncidentStore
	AWS        AWSStore
	GCP        GCPStore
	Actions    ActionsStore
}

// ServerConfig configures the intake API server.
type ServerConfig struct {
	// Token is the shared bearer secret the extracted listener presents.
	// Empty fails closed: the server refuses to construct.
	Token string
	// MaxBodyBytes caps one intake request body. Zero uses the default.
	MaxBodyBytes int64
	// Clock returns the server-stamped received time. Nil uses UTC now.
	Clock func() time.Time
	// Logger receives structured intake outcomes. Nil disables logging.
	Logger *slog.Logger
}

// Server is core's authenticated, versioned webhook intake API.
type Server struct {
	config ServerConfig
	stores Stores
}

// NewServer builds an intake server with default body bounds. Prefer
// NewServerChecked, which fails closed on an empty token.
func NewServer(config ServerConfig, stores Stores) *Server {
	return &Server{config: withDefaults(config), stores: stores}
}

// NewServerChecked builds an intake server, refusing an empty bearer token:
// an intake API without authentication must never serve.
func NewServerChecked(config ServerConfig, stores Stores) (*Server, error) {
	if strings.TrimSpace(config.Token) == "" {
		return nil, errors.New("webhook intake bearer token is required")
	}
	return NewServer(config, stores), nil
}

func withDefaults(config ServerConfig) ServerConfig {
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = defaultMaxIntakeBodyBytes
	}
	return config
}

// Mux returns the intake route set. Every intake route requires the bearer
// token; /healthz stays unauthenticated so load balancers and the
// extracted listener's readiness probe can reach it without credentials.
func (s *Server) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/webhook/intake/repository", s.handleRepository)
	mux.HandleFunc("/api/v1/webhook/intake/incident", s.handleIncident)
	mux.HandleFunc("/api/v1/webhook/intake/aws", s.handleAWS)
	mux.HandleFunc("/api/v1/webhook/intake/gcp", s.handleGCP)
	mux.HandleFunc("/api/v1/webhook/intake/actions", s.handleActions)
	mux.HandleFunc("/healthz", s.handleHealth)
	return mux
}

func (s *Server) now() time.Time {
	if s.config.Clock != nil {
		return s.config.Clock()
	}
	return time.Now().UTC()
}

// authorize rejects requests without the configured bearer token using a
// constant-time comparison. The token value never appears in logs.
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "Bearer "
	presented := r.Header.Get("Authorization")
	if !strings.HasPrefix(presented, prefix) {
		s.logOutcome(r, "intake request rejected", slog.String("reason", "missing_bearer"))
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "authorization required", http.StatusUnauthorized)
		return false
	}
	got := strings.TrimSpace(strings.TrimPrefix(presented, prefix))
	want := strings.TrimSpace(s.config.Token)
	if want == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		s.logOutcome(r, "intake request rejected", slog.String("reason", "invalid_bearer"))
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "authorization failed", http.StatusUnauthorized)
		return false
	}
	return true
}

// readBody enforces the intake body bound: 413 only on cap exceed, other
// read failures 400, mirroring the provider-facing listener contract.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return nil, false
	}
	limited := http.MaxBytesReader(w, r.Body, s.config.MaxBodyBytes)
	defer func() { _ = limited.Close() }()
	payload, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return nil, false
		}
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return nil, false
	}
	if len(payload) == 0 {
		http.Error(w, "empty request body", http.StatusBadRequest)
		return nil, false
	}
	return payload, true
}

// decodeRequest runs the shared intake prologue — bearer auth, body
// bound, and JSON decode — so each route handler starts from a decoded
// wire request.
func (s *Server) decodeRequest(w http.ResponseWriter, r *http.Request, request any) bool {
	if !s.authorize(w, r) {
		return false
	}
	payload, ok := s.readBody(w, r)
	if !ok {
		return false
	}
	return s.decode(w, r, payload, request)
}

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

// decode unmarshals one intake body. Unknown fields are ignored so a newer
// client still submits to this server (forward compatibility); a body that
// is not JSON at all is a 400.
func (s *Server) decode(w http.ResponseWriter, r *http.Request, payload []byte, request any) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(request); err != nil {
		s.logOutcome(r, "intake request rejected", slog.String("reason", "invalid_json"))
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return false
	}
	return true
}

// writeTriggerError maps a wire validation failure to its status: version
// and provider-family mismatches carry their own codes so the listener can
// distinguish a contract problem from a bad trigger.
func (s *Server) writeTriggerError(w http.ResponseWriter, r *http.Request, route string, err error) {
	code := codeInvalidTrigger
	status := http.StatusBadRequest
	switch {
	case errors.Is(err, errUnsupportedAPIVersion):
		code = codeUnsupportedAPIVersion
	case errors.Is(err, errUnsupportedProvider):
		code = codeUnsupportedProvider
	}
	s.logOutcome(r, "intake request rejected",
		slog.String("route", route),
		slog.String("code", code),
	)
	writeIntakeJSON(w, status, map[string]any{
		"api_version": APIVersionV1,
		"code":        code,
		"error":       firstLine(err.Error()),
	})
}

// writeStoreError reports a route with no store configured as a 503: core
// has that trigger family disabled, so the listener must keep the delivery
// for redelivery rather than drop it.
func (s *Server) writeStoreError(w http.ResponseWriter, r *http.Request, route string) {
	s.logOutcome(r, "intake store unavailable", slog.String("route", route))
	writeIntakeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"api_version": APIVersionV1,
		"code":        codeStoreFailed,
		"error":       fmt.Sprintf("%s intake store is not configured", route),
	})
}

// writeStoreFailure reports a durable-store failure as a 500 without store
// internals so the provider redelivers.
func (s *Server) writeStoreFailure(w http.ResponseWriter, r *http.Request, route, provider string, err error) {
	s.logOutcome(r, "intake store failed",
		slog.String("route", route),
		slog.String("provider", provider),
		slog.String("error", firstLine(err.Error())),
	)
	writeIntakeJSON(w, http.StatusInternalServerError, map[string]any{
		"api_version": APIVersionV1,
		"code":        codeStoreFailed,
		"error":       fmt.Sprintf("store %s trigger", route),
	})
}

// logOutcome records one structured intake outcome. Provider secrets,
// bearer tokens, and raw trigger identities never appear here: only the
// route, provider family, status, and machine-readable reason.
func (s *Server) logOutcome(r *http.Request, message string, attrs ...slog.Attr) {
	if s.config.Logger == nil {
		return
	}
	all := make([]slog.Attr, 0, len(attrs)+2)
	all = append(all, slog.String("method", r.Method), slog.String("path", r.URL.Path))
	all = append(all, attrs...)
	args := make([]any, 0, len(all))
	for _, attr := range all {
		args = append(args, attr)
	}
	s.config.Logger.Log(r.Context(), slog.LevelInfo, message, args...)
}

func writeIntakeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// firstLine keeps one line of an error for logs and bodies so a wrapped
// store error cannot smuggle multi-line internals into a response.
func firstLine(message string) string {
	if i := strings.IndexByte(message, '\n'); i >= 0 {
		return message[:i]
	}
	return message
}
