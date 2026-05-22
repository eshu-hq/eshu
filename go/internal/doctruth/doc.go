// Package doctruth extracts conservative documentation truth evidence,
// verification findings, and drift findings from bounded documentation
// sections.
//
// The package emits mention and claim-candidate evidence without treating prose
// as operational truth. Verifier compares explicit documentation claims such as
// CLI commands, HTTP endpoints, environment variables, and explicit local repo
// paths or container image refs with caller-supplied truth sources, then emits
// documentation_finding and documentation_evidence_packet facts.
// DeploymentDriftAnalyzer compares service_deployment claim candidates with
// caller-supplied Eshu truth and returns read-only findings that preserve match,
// conflict, ambiguous, unsupported, stale, and building states.
package doctruth
