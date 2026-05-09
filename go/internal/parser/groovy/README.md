# Groovy Parser Helpers

## Purpose

This package owns Jenkins/Groovy parser helpers that do not need parent parser
payload helpers. It extracts shared libraries, pipeline calls, shell commands,
Ansible playbook hints, entry points, and configd/pre-deploy flags from source
text.

## Ownership boundary

The package is responsible for typed Groovy delivery evidence. The parent
parser package still owns file I/O, parser registry dispatch, payload assembly,
pre-scan wiring, and the exported compatibility wrapper used by query code.

## Exported surface

The godoc contract is in doc.go. Current exports are Metadata,
AnsiblePlaybookHint, PipelineMetadata, and Metadata.Map. Metadata carries
SharedLibraries, PipelineCalls, ShellCommands, AnsiblePlaybookHints,
EntryPoints, UseConfigd, and HasPreDeploy.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

PipelineMetadata normalizes shared library versions such as `pipelines@v2` down
to `pipelines`, matching existing parser payload behavior.

Metadata.Map keeps the legacy `map[string]any` payload shape because query and
relationship packages still consume `parser.ExtractGroovyPipelineMetadata`.

## Related docs

- docs/plans/2026-05-09-parser-language-layout.md
