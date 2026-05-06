// Package mcp implements the Model Context Protocol tool surface for Eshu.
//
// MCP tools dispatch into the same HTTP query handlers that power the public
// HTTP API, so a tool response and the corresponding HTTP query response
// share truth. Helpers in this package normalize tool arguments, build
// request bodies for the underlying handler, and parse canonical response
// envelopes. Any change that alters request or response shape must update
// the MCP guide, the HTTP API reference where the route is shared, and the
// handler tests in the same change.
package mcp
