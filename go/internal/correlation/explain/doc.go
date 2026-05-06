// Package explain renders one engine result as a stable, line-oriented text
// block for the explain API and operator-facing diagnostics.
//
// `Render` produces a deterministic header line, sorted match-count lines,
// sorted rejection-reason lines, and sorted evidence lines with stable
// confidence formatting. Output ordering is part of the contract: the explain
// API and golden tests rely on it. This package does not evaluate rules or
// apply admission; it only formats what the engine has already decided.
package explain
