# Groovy Parser Helpers

## Purpose

This package owns Jenkins/Groovy parser extraction that does not need parent
parser internals. It builds the Groovy payload, pre-scan names, and delivery
metadata for shared libraries, pipeline calls, shell commands, Ansible
playbook hints, entry points, and configd/pre-deploy flags.

## Ownership boundary

The package is responsible for Groovy parse and pre-scan behavior plus typed
delivery evidence. The parent parser package still owns parser registry
dispatch, content metadata enrichment, and the exported compatibility wrapper
used by query and relationship code.

## Exported surface

The godoc contract is in doc.go. Current exports are Metadata,
AnsiblePlaybookHint, Parse, PreScan, PipelineMetadata, and Metadata.Map.
Metadata carries SharedLibraries, PipelineCalls, ShellCommands,
AnsiblePlaybookHints, EntryPoints, UseConfigd, and HasPreDeploy.

## Dependencies

This package imports `internal/parser/shared` and the Go standard library. It
must not import the parent parser package, collector packages, graph storage,
or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

PipelineMetadata normalizes shared library versions such as `pipelines@v2`
down to `pipelines`, matching existing parser payload behavior.

Parse and PreScan use shared payload helpers so bucket shape and pre-scan
ordering stay aligned with other language-owned parser packages. Metadata.Map
keeps the legacy `map[string]any` payload shape because query and relationship
packages still consume `parser.ExtractGroovyPipelineMetadata`.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
