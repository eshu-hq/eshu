// Package skillgen owns the source-of-truth skill fragment loader, the
// byte-citation preservation contract, the per-host adapter registry, and
// the deterministic render pipeline that produces the roundtrip baseline
// committed at the repo root under expected/.
//
// The package reads skill-fragments/*.md, parses each fragment's YAML
// frontmatter, formats the byte_citation into a stable top-of-file comment
// block, and renders one skill file per registered host (Claude Code,
// Cursor, Codex). v1 has no LLM dependency; the per-collector-matrix
// fragment is the only fragment that consumes per-deployment capability
// overrides read from skill-fragments/capabilities.local.yaml (gitignored).
//
// The package is the implementation of S2 of the skillgen epic. The
// byte-citation comment block is preserved here for S3 to verify against
// the merge tree (not ambient main). Adding a new host means appending a
// constant to the Host block, a constructor to hostRegistry, and an
// adapter file; the render pipeline is host-agnostic.
package skillgen
