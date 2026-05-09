// Package javascript extracts JavaScript and TypeScript parser evidence that
// can stay independent from the parent parser dispatch package.
//
// The package currently owns tsconfig.json import resolution and package.json
// root modeling. Resolvers accept JSONC TypeScript config files, keep
// resolution inside the repository root, and return repository-relative source
// paths so the parent parser can attach resolved_source metadata to import
// payloads. Package helpers use the nearest owning package.json before mapping
// compiled main, bin, script, exports, and types targets back to source files.
package javascript
