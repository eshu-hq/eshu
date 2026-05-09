// Package confluence collects read-only Confluence documentation evidence.
//
// The package supports bounded collection by Confluence space or root page
// tree, normalizes visible pages into source-neutral documentation facts, and
// preserves source provenance, freshness, labels, links, ownership hints, and
// partial-sync evidence without mutating Confluence. Callers may attach a
// doctruth.Extractor plus claim hints to emit non-authoritative mention and
// claim-candidate facts from the same page evidence.
package confluence
