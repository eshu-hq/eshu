# Dockerfile Parser Helpers

## Purpose

This package owns Dockerfile parser helpers that do not need parent parser
payload helpers. It extracts runtime metadata such as stages, exposed ports,
arguments, environment variables, labels, and runtime commands from Dockerfile
source text.

## Ownership boundary

The package is responsible for typed Dockerfile runtime evidence. The parent
parser package still owns file I/O, parser registry dispatch, payload assembly,
and the exported compatibility wrapper used by query and relationship code.

## Exported surface

The godoc contract is in doc.go. Current exports are Metadata, Stage, Arg, Env,
Port, Label, RuntimeMetadata, and Metadata.Map.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

RuntimeMetadata keeps bucket ordering deterministic by sorting rows by name,
matching the old parent parser payload behavior.

Metadata.Map keeps the legacy `map[string]any` payload shape because query and
relationship packages still consume `parser.ExtractDockerfileRuntimeMetadata`.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
