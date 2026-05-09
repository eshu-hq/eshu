# Java Parser Helpers

## Purpose

This package owns Java parser helpers that do not need the parent parser
dispatch code. The first helper extracts class references from ServiceLoader
and Spring metadata files so Java dead-code reachability can use explicit
metadata evidence without scanning source files again.

## Ownership boundary

The package is responsible for Java-specific evidence extraction that can be
expressed with typed return values. The parent parser package still owns file
I/O, payload assembly, bucket sorting, registry dispatch, tree-sitter parsing,
and final map keys.

## Exported surface

The godoc contract is in doc.go. Current exports are ClassReference and
MetadataClassReferences.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

MetadataClassReferences accepts repository-relative or absolute paths and
normalizes separators before matching metadata locations.

ServiceLoader files are line-oriented. Spring factories may continue a value
across backslash-terminated lines, and the extractor keeps the starting line
number for every class in the continued value.

Invalid or duplicate class names are ignored. That keeps metadata extraction
bounded to names that look like Java fully qualified class names.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
