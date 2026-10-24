// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

const (
	// LanguageName is the lang tag on every payload and row, the same tag the
	// build-script parser uses so both feed one Gradle ecosystem.
	LanguageName = "gradle"
	// PackageManager is the ecosystem identifier on every dependency row. The
	// supply-chain reducer normalizes it to Maven identity.
	PackageManager = "gradle"

	// MaxSourceBytes bounds the file the parser will read. Real catalogs are a
	// few kilobytes; a larger file is reported as StateOversize and never parsed.
	MaxSourceBytes = 2 << 20
	// MaxRows bounds the library and plugin rows one file emits, and the
	// version and bundle aliases it tracks. The excess is counted in
	// Counts.Dropped.
	MaxRows = 5000
	// MaxFieldBytes bounds an alias, a coordinate part, a plugin id and a
	// version text. A longer value makes its entry unsupported; it is never
	// truncated.
	MaxFieldBytes = 256
	// MaxNesting bounds array and inline-table depth. See nestingExceeds.
	MaxNesting = 16
	// MaxKeySegments bounds the dotted segments of one key. A Gradle catalog
	// key needs at most three (alias.version.ref); a longer key is reported
	// as StateTooDeep, because the streaming parse and the key tree allocate
	// per segment.
	MaxKeySegments = 64
	// MaxFileKeySegments bounds the dotted segments of every key and header
	// in one file, inline-table keys included. A catalog of MaxRows entries
	// in each table uses about 40,000 and a 2 MiB catalog of the same shape
	// about 101,000; a file over the bound is reported as StateTooDeep.
	MaxFileKeySegments = 1 << 18
)

// File states reported in Result.State and the payload's gradle_catalog_state.
const (
	// StateParsed means the file was read as TOML and the grammar was applied.
	StateParsed = "parsed"
	// StateMalformed means the file is not valid TOML: a syntax error, or a
	// key or table defined twice in any form (see keyTree). No rows are
	// emitted.
	StateMalformed = "malformed"
	// StateOversize means the file exceeds MaxSourceBytes. No rows are emitted.
	StateOversize = "oversize"
	// StateTooDeep means the file nests deeper than MaxNesting, or has a key
	// over MaxKeySegments or more than MaxFileKeySegments key segments. No
	// rows are emitted.
	StateTooDeep = "too_deep"
)

// Counts tallies what a scan saw, so a reader can tell an empty catalog from
// one the parser could not read. Every construct that yields no row or a
// weaker row lands in exactly one of Unsupported, Unresolved, Partial, Dropped.
type Counts struct {
	// Versions counts [versions] aliases, including rich ones.
	Versions int
	// Libraries counts [libraries] entries that matched the grammar.
	Libraries int
	// Plugins counts [plugins] entries that matched the grammar.
	Plugins int
	// Bundles counts [bundles] aliases. Bundles emit no rows: they group
	// library aliases and declare no dependency of their own.
	Bundles int
	// Resolved counts rows whose version text is known verbatim.
	Resolved int
	// Partial counts rows that declare no version.
	Partial int
	// Unresolved counts rows whose version the grammar cannot read verbatim.
	Unresolved int
	// Unsupported counts constructs the grammar does not cover. None yields a
	// row.
	Unsupported int
	// Dropped counts entries skipped because MaxRows was reached.
	Dropped int
	// Collided counts aliases, in any table, that share a normalized spelling
	// or an accessor name with another alias in the same table. Gradle treats
	// '-', '_' and '.' in an alias as one separator, so "a-b" and "a_b" are
	// one alias to it, and it fails the build when two aliases map to one
	// accessor name, such as "fooBar" and "foo-bar". An alias written as a
	// dotted key or a sub-table counts too, though it emits no row.
	// Colliding library and plugin rows are also counted in Unresolved; this
	// count is a diagnostic that overlaps the others.
	Collided int
}

// Result is the outcome of scanning one catalog.
type Result struct {
	// State is one of the State constants.
	State string
	// ParseError describes why State is not StateParsed. It holds a reason or
	// a TOML syntax message, never file content beyond that message.
	ParseError string
	// Rows holds the content_entity-shaped variable rows in document order.
	Rows []map[string]any
	// Counts tallies the scan.
	Counts Counts
}
