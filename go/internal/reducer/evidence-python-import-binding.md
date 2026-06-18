# Python Import-Binding Evidence

Issue #3004 deepens Python call resolution by letting unqualified imported call
targets use parser import rows before the weak repository-wide fallback.

No-Regression Evidence: `go test ./internal/reducer -run TestExtractCodeCallRowsPrefersPythonAliasedImportBeforeRepoFallback -count=1` failed before aliased Python imports were considered before weak repository fallback: `from lib.factory import create_app as make_app; make_app()` resolved to an unrelated repository-unique `make_app` decoy with `repo_unique_name`. It passes after Python unqualified import targets use the existing parser import rows and repository `imports_map` before weak fallback, returning the imported `create_app` with `import_binding`. `go test ./internal/resolutionparity -run 'TestGoldenCallGraphCorrectnessHarness/python_import_binding|TestResolutionTierGoldens' -count=1` proves the source-derived Python alias fixture targets the imported function and the existing tier distribution remains stable.

No-Observability-Change: Python aliased import resolution reorders an existing in-memory resolver branch over parsed import rows, repository prescan import maps, and the existing entity index. It adds no graph query, graph write shape, queue table, worker, lease, batch setting, runtime knob, metric instrument, metric label, span, route, status field, or log key. Operators still diagnose code-call extraction through the existing `code call materialization completed` log fields and reducer execution spans/counters.
