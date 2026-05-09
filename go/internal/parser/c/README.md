# C Parser Adapter

## Purpose

This package owns C-specific tree-sitter payload extraction for functions,
types, includes, macros, typedef aliases, variables, and calls.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns C syntax walking and payload assembly, while the parent package
keeps registry dispatch, runtime parser construction, and compatibility method
signatures.

## Exported Surface

The package exposes Parse for full payload extraction and PreScan for dependency
symbol discovery.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package.

## Operational Notes

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.
