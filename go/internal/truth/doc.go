// Package truth defines the layered truth contract used by reducer-owned
// canonical materialization.
//
// Layer enumerates the four bounded source layers: source_declaration,
// applied_declaration, observed_resource, and canonical_asset. Contract binds
// a canonical kind to the set of source layers a reducer accepts as evidence
// for that kind. Validate enforces non-empty source layers, rejects
// canonical_asset as a source, and rejects duplicates.
package truth
