# Dart Parser

## Purpose

This package owns the line-oriented Dart parser adapter used by the parent
parser engine. It extracts import/export names, class-style declarations,
top-level function rows, variables, and simple call evidence.

## Ownership boundary

The package is responsible for Dart source scanning and payload bucket
population. The parent parser package still owns registry dispatch, engine
orchestration, repo path handling, and parse telemetry.

## Exported surface

The godoc contract is in doc.go. Current exports are Parse and PreScan.

## Dependencies

This package imports the Go standard library and internal/parser/shared. It
must not import the parent internal/parser package.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

Bucket ordering must stay deterministic because parser payloads become fact
inputs. PreScan returns function and class names using the same payload path as
Parse, then sorts those names before returning them to the parent engine.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
