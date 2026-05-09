// Package doctruth extracts conservative documentation truth evidence and drift
// findings from bounded documentation sections.
//
// The package emits mention and claim-candidate evidence without treating prose
// as operational truth. DeploymentDriftAnalyzer compares service_deployment
// claim candidates with caller-supplied Eshu truth and returns read-only
// findings that preserve match, conflict, ambiguous, unsupported, stale, and
// building states.
package doctruth
