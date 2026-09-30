// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"net/http"
	"sync/atomic"
)

// deferredHandler is an http.Handler whose delegate is set once, after
// construction. It exists to break a wiring ordering cycle: the Ask Eshu
// in-process MCP runner must dispatch inner tool calls through the SAME
// scoped-auth-middleware-wrapped handler that external callers hit, so that
// every inner read re-runs the scoped-route gate under the caller's token. But
// that wrapped handler is built only after the Ask handler is mounted on the
// inner mux. The runner is therefore wired to a deferredHandler now, and the
// wrapped handler is installed via Set once it exists.
//
// Until Set is called, ServeHTTP responds 503. In practice Set runs during
// synchronous wiring, before the server accepts any request, so callers never
// observe the unset state. The delegate is stored atomically so the wiring-time
// write is safely visible to request-time reads.
type deferredHandler struct {
	delegate atomic.Pointer[http.Handler]
}

// Set installs the delegate handler. It is expected to be called exactly once,
// during wiring, before the server serves traffic.
func (d *deferredHandler) Set(h http.Handler) {
	d.delegate.Store(&h)
}

// ServeHTTP dispatches to the installed delegate, or returns 503 if Set has not
// been called yet.
func (d *deferredHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hp := d.delegate.Load()
	if hp == nil || *hp == nil {
		http.Error(w, "ask in-process handler not ready", http.StatusServiceUnavailable)
		return
	}
	(*hp).ServeHTTP(w, r)
}
