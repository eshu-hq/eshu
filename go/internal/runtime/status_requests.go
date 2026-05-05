package runtime

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RequestState represents the lifecycle state of a scan or reindex request.
type RequestState string

const (
	// RequestStateIdle means no scan or reindex request is currently active.
	RequestStateIdle RequestState = "idle"
	// RequestStatePending means the request has been stored but not claimed.
	RequestStatePending RequestState = "pending"
	// RequestStateRunning means a runtime has claimed and started the request.
	RequestStateRunning RequestState = "running"
	// RequestStateCompleted means the claimed request finished successfully.
	RequestStateCompleted RequestState = "completed"
	// RequestStateFailed means the claimed request ended with an error.
	RequestStateFailed RequestState = "failed"
)

// Validate returns an error if the state is not a known value.
func (s RequestState) Validate() error {
	switch s {
	case RequestStateIdle, RequestStatePending, RequestStateRunning, RequestStateCompleted, RequestStateFailed:
		return nil
	default:
		return fmt.Errorf("unknown request state %q", s)
	}
}

// ScanRequest captures the current state of a scan lifecycle for one ingester.
type ScanRequest struct {
	Ingester    string
	State       RequestState
	RequestedAt time.Time
	ClaimedAt   time.Time
	CompletedAt time.Time
	Error       string
}

// ReindexRequest captures the current state of a reindex lifecycle for one ingester.
type ReindexRequest struct {
	Ingester    string
	State       RequestState
	RequestedAt time.Time
	ClaimedAt   time.Time
	CompletedAt time.Time
	Error       string
}

// StatusRequestStore provides the durable scan/reindex request lifecycle
// operations ported from the Python status_store_db.
type StatusRequestStore interface {
	// RequestScan transitions a scan request from idle to pending.
	RequestScan(ctx context.Context, ingester string, now time.Time) error

	// ClaimScanRequest transitions a pending scan to running.
	ClaimScanRequest(ctx context.Context, ingester string, now time.Time) (ScanRequest, error)

	// CompleteScanRequest transitions a running scan to completed or failed.
	CompleteScanRequest(ctx context.Context, ingester string, now time.Time, scanErr string) error

	// RequestReindex transitions a reindex request from idle to pending.
	RequestReindex(ctx context.Context, ingester string, now time.Time) error

	// ClaimReindexRequest transitions a pending reindex to running.
	ClaimReindexRequest(ctx context.Context, ingester string, now time.Time) (ReindexRequest, error)

	// CompleteReindexRequest transitions a running reindex to completed or failed.
	CompleteReindexRequest(ctx context.Context, ingester string, now time.Time, reindexErr string) error

	// GetScanState returns the current scan request state for one ingester.
	GetScanState(ctx context.Context, ingester string) (ScanRequest, error)

	// GetReindexState returns the current reindex request state for one ingester.
	GetReindexState(ctx context.Context, ingester string) (ReindexRequest, error)
}

// StatusRequestHandler manages scan/reindex lifecycle transitions.
type StatusRequestHandler struct {
	store StatusRequestStore
}

// NewStatusRequestHandler constructs a handler with the given store.
func NewStatusRequestHandler(store StatusRequestStore) (*StatusRequestHandler, error) {
	if store == nil {
		return nil, errors.New("status request store is required")
	}
	return &StatusRequestHandler{store: store}, nil
}

// RequestScan initiates a scan request for the given ingester.
func (h *StatusRequestHandler) RequestScan(ctx context.Context, ingester string) error {
	if ingester == "" {
		return errors.New("ingester name is required")
	}
	return h.store.RequestScan(ctx, ingester, time.Now().UTC())
}

// ClaimScan claims a pending scan request for the given ingester.
func (h *StatusRequestHandler) ClaimScan(ctx context.Context, ingester string) (ScanRequest, error) {
	if ingester == "" {
		return ScanRequest{}, errors.New("ingester name is required")
	}
	return h.store.ClaimScanRequest(ctx, ingester, time.Now().UTC())
}

// CompleteScan marks a running scan as completed or failed.
func (h *StatusRequestHandler) CompleteScan(ctx context.Context, ingester string, scanErr string) error {
	if ingester == "" {
		return errors.New("ingester name is required")
	}
	return h.store.CompleteScanRequest(ctx, ingester, time.Now().UTC(), scanErr)
}

// RequestReindex initiates a reindex request for the given ingester.
func (h *StatusRequestHandler) RequestReindex(ctx context.Context, ingester string) error {
	if ingester == "" {
		return errors.New("ingester name is required")
	}
	return h.store.RequestReindex(ctx, ingester, time.Now().UTC())
}

// ClaimReindex claims a pending reindex request for the given ingester.
func (h *StatusRequestHandler) ClaimReindex(ctx context.Context, ingester string) (ReindexRequest, error) {
	if ingester == "" {
		return ReindexRequest{}, errors.New("ingester name is required")
	}
	return h.store.ClaimReindexRequest(ctx, ingester, time.Now().UTC())
}

// CompleteReindex marks a running reindex as completed or failed.
func (h *StatusRequestHandler) CompleteReindex(ctx context.Context, ingester string, reindexErr string) error {
	if ingester == "" {
		return errors.New("ingester name is required")
	}
	return h.store.CompleteReindexRequest(ctx, ingester, time.Now().UTC(), reindexErr)
}
