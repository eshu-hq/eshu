// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

// Client is the subset of the CLI's HTTP API client that this package calls.
// go/cmd/eshu's *APIClient satisfies it.
//
// The interface is declared here, at the point of use, rather than shared
// from go/cmd/eshu: that package is `package main` and cannot be imported,
// and its *APIClient resolves the service URL and API key from cobra flags,
// the process environment, and the on-disk config file — process state this
// package must not reach for. The wrapper resolves all of it and passes the
// built client in.
//
// Both methods JSON-decode the response body into result. Passing a nil
// result discards the body.
type Client interface {
	// Get issues a GET to path, relative to the client's base URL.
	Get(path string, result any) error
	// Post issues a POST to path with body marshaled as the JSON request
	// body.
	Post(path string, body, result any) error
}
