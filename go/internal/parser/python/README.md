# Python Parser

## Purpose

This package owns the Python language adapter used by the parent parser engine.
It turns Python source and notebook code cells into parser payload buckets for
functions, classes, modules, variables, imports, calls, annotations, framework
metadata, ORM table mappings, shell-exec call-site evidence, and dead-code root
evidence.

## Python parse flow

```mermaid
flowchart LR
    Parent["parent parser Engine"]
    Source[".py file or notebook code cells"]
    Config["SAM/serverless config scan"]
    Parse["python.Parse"]
    Payload["functions, classes, imports, calls, routes, tables, shell exec, roots"]
    Collector["collector materialization"]

    Parent --> Parse
    Source --> Parse
    Config --> Parse
    Parse --> Payload
    Payload --> Collector
```

Notebook extraction and config scans produce parser evidence only. Runtime
ownership, fact storage, and graph writes remain outside this package.

## Ownership boundary

The package is responsible for Python-specific parsing and evidence shaping.
That includes .py input, .ipynb code-cell extraction, import source metadata,
FastAPI and Flask route summaries, SQLAlchemy and Django table hints, Lambda
handler roots from SAM and serverless config, generator flags, metaclass data,
public API roots, and Python call receiver inference.

The parent parser package still owns registry dispatch, absolute path
resolution, content metadata, and Engine method signatures. The child package
must not import the parent parser package; shared payload and tree helpers come
from the shared parser package.

## Exported surface

The godoc contract is in doc.go.

- Parse reads one Python or notebook path with a caller-owned tree-sitter parser
  and returns the parser payload. When `Options.EmitDataflow` is set it also emits
  the opt-in `dataflow_functions`, `taint_findings`, and `interproc_findings`
  buckets (see `cfg_emit.go`); the gate is off by default and the payload is
  byte-identical when off.
- PreScan reuses Parse for collector import-map discovery.
- NotebookSource extracts executable Python code cells from notebook JSON.
- The `embedded_shell_commands` bucket records import-backed `subprocess` and
  `os.system` call sites with function, line, API, and language metadata only.
  It does not retain command strings, arguments, or environment values.

## Dependencies

The package imports the shared parser helper package for Options, BasePayload,
ReadSource, WalkNamed, node text helpers, and bucket helpers. The value-flow
emission (`cfg_emit.go`) imports the `python/pydataflow` lowering, the shared
`internal/parser/dataflowemit` renderer, and the `internal/parser/cfg`/`taint`
engines. It imports the YAML parser child package only to decode SAM and
serverless config candidates when marking Python Lambda handlers.

It does not import the parent parser package, collector packages, storage
packages, graph query code, or reducer code.

## Telemetry

This package emits no telemetry directly. Parser timing and parse failure
context remain owned by the ingester and collector runtime paths that call the
parent Engine.

## Gotchas / invariants

NotebookSource returns an empty string for notebooks without code cells. Invalid
JSON returns an error so a caller fails the file instead of indexing partial
source.

Parse accepts a caller-owned tree-sitter parser. The caller opens and closes the
parser so the parent Engine can preserve its runtime lifecycle.

Lambda handler detection scans template.yaml, template.yml, serverless.yaml, and
serverless.yml from the source directory up to the repository root. It only
marks handlers when the runtime is Python.

Script-main guard detection walks parsed if statements and accepts both
`__name__ == "__main__"` and `"__main__" == __name__` forms. Only calls inside
the guard statement become `python.script_main_guard` roots.

Property root detection covers `@property`, `@cached_property`, and
`@functools.cached_property`, including decorators with inline type-checker
comments. Dunder protocol roots cover recognized class protocol methods, module
`__getattr__` and `__dir__` hooks, and nested dunder functions only when source
assignment evidence in the same enclosing function or module installs them onto
a protocol attribute.

The adapter keeps module-scope variables by default. Set the shared
VariableScope option to all when a caller needs local assignment payloads too.

## Related docs

- docs/public/architecture.md
- docs/public/reference/local-testing.md
- docs/public/languages/support-maturity.md
