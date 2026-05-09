// Package kotlin owns Kotlin parser extraction for the parent parser engine.
//
// Parse reads one Kotlin source file and returns the payload buckets consumed by
// collector and projector code: declarations, imports, variables, function
// calls, and inferred receiver metadata. PreScan uses the same extraction path
// so import-map discovery sees the same function, class, and interface names as
// normal parsing.
package kotlin
