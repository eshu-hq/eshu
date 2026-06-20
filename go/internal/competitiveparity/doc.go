// Package competitiveparity validates that shipped Eshu capability surfaces
// remain reachable and documented against a peer-inspired parity checklist.
//
// The gate is intentionally offline. Callers supply CLI command paths, generated
// API/MCP/console surface names, public documentation text, and local exercise
// results. Validate scores that inventory against the default #3265 expectations
// for first-run reports, operator digests, investigation evidence packets, and
// the capability catalog, then returns a deterministic report renderable as JSON
// or Markdown.
package competitiveparity
