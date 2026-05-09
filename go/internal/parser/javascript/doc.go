// Package javascript parses JavaScript, TypeScript, and TSX source into the
// parser payload consumed by the parent dispatcher.
//
// The package owns tree-sitter extraction, import and re-export rows, component
// evidence, TypeScript declarations, call metadata, dead-code root evidence,
// tsconfig.json import resolution, and package.json public surface modeling.
// JavaScript and TypeScript dead-code helpers live in this package so root
// modeling stays close to import, export, Hapi, framework-route, TypeScript
// public-surface, and nearest-package evidence. Shared helper aliases are kept
// local to the helpers that still need them. Callers provide a ParserFactory so
// runtime grammar caching stays in
// the parent package while this child package stays independent from
// internal/parser. Resolvers accept JSONC TypeScript config files, keep
// resolution inside the repository root, and return repository-relative source
// paths for resolved_source metadata.
package javascript
