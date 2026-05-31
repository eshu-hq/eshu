// Command scanner-worker runs isolated scanner-worker claims for CPU-heavy or
// memory-heavy security analyzers.
//
// The binary consumes workflow work items with collector_kind=scanner_worker,
// builds scannerworker.ClaimInput values with resource limits, commits source
// facts under the claim fence, and records bounded retry or dead-letter
// payloads. It can run concrete sbom_generation repository-manifest and
// os_package_extraction rootfs analyzers, but it does not emit reducer-owned
// findings.
package main
