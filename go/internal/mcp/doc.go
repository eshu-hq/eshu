// Package mcp implements the Model Context Protocol tool surface for Eshu.
//
// MCP tools dispatch into the same HTTP query handlers that power the public
// HTTP API, so a tool response and the corresponding HTTP query response
// share truth. Helpers in this package normalize tool arguments, including
// shared slice and identifier helpers in dispatch_args.go, build request bodies
// for the underlying handler, and parse canonical response envelopes. Citation
// tools stay in this transport layer and delegate source hydration to the query
// package rather than reading storage directly; the advertised citation schema
// caps input at 500 handles. Security investigation tools also stay transport
// only and delegate redacted finding generation to the query package; their
// tool definition lives in tools_security.go. Any change that alters request or
// response shape must update
// the MCP guide, the HTTP API reference where the route is shared, and the
// handler tests in the same change.
package mcp
