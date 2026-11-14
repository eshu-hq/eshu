# Import Dependency Prefix Reads

`investigate_import_dependencies` accepts `target_module_prefix`, which
matches `Module` names by prefix (`Module.name STARTS WITH
$target_module_prefix`), and `external_only`, which excludes relative
(`./x`, `../y`) and absolute (`/abs`) specifiers so the first page holds
bare package names. The prefix counts as a scope filter, is mutually
exclusive with `target_module`, and is rejected when empty. Both apply only
to the module-matching reads (`imports_by_file`, `importers`,
`module_dependencies`, `package_imports`), not to file-import cycles or
cross-module calls.

The prefix seeks the existing `module_name_lookup` RANGE index on
`Module.name` on the direct and package reads: PROFILE on a
30,000-`Module` population shows a `NodeIndexSeekByRange` anchor with ~27
DbHits per 25-row page and no whole-graph scan, for trailing `.` and `/`
prefixes as well as `@scope/` families. The source-module scan path keeps
its pinned Cypher text (no `STARTS WITH`) and applies the prefix/external
filter in Go after the bounded scan, so a broad source scope can still hit
the 25,000-row scan refusal before the Go filter. No new index was added. New
queryplan pins `QP-CODE-IMPORT-PREFIX-ROWS` and
`QP-CODE-IMPORT-PREFIX-PACKAGES` bind the exact production shapes; the old
pins are untouched. Full proof (setup, PROFILE tables, row counts):
`docs/internal/evidence/10657-import-prefix-profile.md`.

Performance Evidence: the direct and package prefix reads are index-seeking
at any corpus size the 25,000-row scan refusal admits; the source-module
scan path stays scan-bounded as pinned and filters in Go. An explicitly
empty prefix is rejected in validation because `STARTS WITH ''` matches
every non-null name.

Observability Evidence: existing `eshu.import_dependencies.*` span
attributes and the response `scope` echo (now carrying
`target_module_prefix` and `external_only`) expose the active filter.

## Related Docs

- [Cypher Performance](cypher-performance.md)
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
