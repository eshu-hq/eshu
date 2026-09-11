# python

The Python call resolver and metaclass edge extractor (issue #6061). The
resolver is wired into `code/call/languages.go` under the `"python"` key;
`ExtractMetaclassRows`/`ExtractMetaclassRowsWithIndex` are called directly
by `code/call/extract.go`. Not imported outside `code/call`.

## Ownership

Declared-base-class method resolution (direct and inherited, walking the
class hierarchy through `shared.EntityIndex.PythonClassBasesByRepo`) for
qualified `Class.method` calls, and `USES_METACLASS` edge extraction from
Python class metadata.

## Files

| File | Covers |
| --- | --- |
| `resolver.go` | `Resolvers`, direct/inherited class-method resolution |
| `metaclass.go` | `ExtractMetaclassRows`, `ExtractMetaclassRowsWithIndex` (renamed from the exported `ExtractPythonMetaclassRows`/`extractPythonMetaclassRowsWithIndex`) |
| `doc.go` | Package contract |

## Dependency rule

Imports `code/call/shared` only. Never `code/call` or a sibling language
leaf.
