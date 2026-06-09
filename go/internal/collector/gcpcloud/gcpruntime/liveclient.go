package gcpruntime

import (
	"context"
	"errors"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

// ErrLiveClientNotImplemented reports that the live Cloud Asset Inventory client
// seam has no transport wired yet. The live gRPC/REST adapter, credential
// loading, retry, throttle, and backoff land in a later slice; this scaffolding
// slice is fixture-tested only and never performs a live Google Cloud call.
var ErrLiveClientNotImplemented = errors.New("gcp live cloud asset inventory client is not implemented in this slice")

// LiveClient is the documented seam for the live Cloud Asset Inventory
// transport. It implements PageProvider so a future slice can drop in the real
// gRPC/REST client and read-only credential resolution without changing the
// Source.
//
// LiveClient is intentionally not wired as a default and is never exercised by
// tests. FetchPage returns ErrLiveClientNotImplemented so any accidental wiring
// fails loudly instead of silently attempting a live call. The collector binary
// only constructs a PageProvider from an offline fixture in this slice.
type LiveClient struct {
	// CredentialRef names the read-only credential a future implementation will
	// resolve out of band. It is a name only; no secret material is stored here.
	CredentialRef string
}

// FetchPage is the unimplemented live page fetch. It always returns
// ErrLiveClientNotImplemented in this slice so the live path cannot make a
// network call by accident.
func (c LiveClient) FetchPage(context.Context, PageRequest) (gcpcloud.AssetsListPage, error) {
	return gcpcloud.AssetsListPage{}, ErrLiveClientNotImplemented
}

var _ PageProvider = LiveClient{}
