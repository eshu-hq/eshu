// Package sbomgenerator implements the bounded scanner-worker analyzer that
// produces CycloneDX-compatible SBOM source facts from a repository, image, or
// artifact target.
//
// The analyzer is a scanner-worker boundary citizen: it consumes one
// claim-bounded Inventory from a runtime-owned Source, enforces
// file-count/input-byte/fact-count limits, derives a deterministic subject
// digest, and emits source facts (sbom.document, sbom.component, sbom.warning)
// that reducers can later admit through the existing sbom_attestation_attachment
// truth path. It does not replace the sbom-attestation collector, which fetches
// already-published SBOM and attestation documents.
//
// The runtime owns filesystem and archive boundaries; this package owns the
// source-fact contract, the limit-check failure vocabulary, and the
// privacy-safe analyzer-failure shape consumed by the scanner-worker claim
// loop.
package sbomgenerator
