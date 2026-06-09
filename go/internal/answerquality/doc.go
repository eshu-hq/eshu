// Package answerquality scores captured API, MCP, CLI, and hosted answer runs
// against the publish-safe dogfood criteria for representative Eshu answer
// families.
//
// The package is intentionally offline: callers capture redacted answers from
// real surfaces, then pass the evidence to Score. This keeps private endpoints,
// repository paths, credentials, and source excerpts out of committed scorecard
// artifacts while preserving a rerunnable pass/fail contract.
package answerquality
