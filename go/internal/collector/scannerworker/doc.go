// Package scannerworker defines scanner-worker claim, output, failure, and
// analyzer-lane contracts.
//
// The package intentionally does not implement analyzers. It defines the
// narrow boundary that lets workflow-owned work items carry bounded target
// scope and resource limits into isolated CPU and memory heavy security
// analyzers. Scanner workers emit source facts only; reducers remain the truth
// owners for finding admission, prioritization, and graph projection.
package scannerworker
