// Package scannerworker defines scanner-worker claims, output validation,
// failure payloads, analyzer ports, and hosted claim processing.
//
// The package owns the narrow boundary that lets workflow-owned work items
// carry bounded repository, image, or artifact target scope and resource limits
// into isolated CPU and memory heavy security analyzers. Scanner workers emit
// allowlisted source fact families only; reducers remain the truth owners for
// finding admission, prioritization, and graph projection. Target kind
// derivation stays bounded to repository, image, or artifact enums so telemetry
// and failure payloads do not leak raw locators. Hosted workers must either
// commit source evidence or record a bounded retry or dead-letter payload.
package scannerworker
