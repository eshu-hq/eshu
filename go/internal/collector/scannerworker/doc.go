// Package scannerworker defines scanner-worker claims, output validation,
// failure payloads, analyzer ports, and hosted claim processing.
//
// The package owns the narrow boundary that lets workflow-owned work items
// carry bounded target scope and resource limits into isolated CPU and memory
// heavy security analyzers. Scanner workers emit source facts only; reducers
// remain the truth owners for finding admission, prioritization, and graph
// projection. Hosted workers must either commit source evidence or record a
// bounded retry or dead-letter payload.
package scannerworker
