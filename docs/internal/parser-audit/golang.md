# Go Parser Audit

## Overview
The Go parser (`go/internal/parser/golang/`) is Eshu's broadest language adapter. It uses tree-sitter to extract functions, methods, structs, interfaces, imports, variables, calls (with chain metadata, receiver inference, import alias tracking), composite-literal type references, dead-code roots (13+ kinds), embedded SQL queries, embedded shell commands, cyclomatic complexity, and — when opted in — dataflow functions, taint findings, interprocedural findings, and durable dataflow summaries. The child directory holds 39 test files: 13 same-package `package golang` unit tests (42 test functions) and 26 external `package golang_test` files (the `go_*_test.go` family plus `engine_go_rich_semantics_test.go` and `engine_data_carriage_return_test.go`: 94 test functions and 2 benchmarks, including Terraform dogfood coverage; 24 of them relocated from the parent by #6062, alongside `go_embedded_shell_test.go` and the `go_test_helpers_test.go` fixture writer). `engine_data_carriage_return_test.go` is the last of the two single-language relocations closing out #6062: it exercises only the Go raw-string carriage-return case (issue #6306) and calls `parsertest.MustParsePath`/`parsertest.WriteFile`, the same helpers `engine_go_rich_semantics_test.go` uses, since it needs only the root package's exported `DefaultEngine`/`ParsePath`/`Options` surface and no root-package-internal symbol. One Go-named test file stays in the parent because it exercises parent-owned code: `go_package_interface_prescan_test.go` (white-box tests of the parent's `effectivePackagePrescanWorkers` and `packagePrescanPassWorkerCount`). Counts derived with `rg -o '^func Test' | wc -l` and `rg -l '^package golang(_test)?$' *_test.go | wc -l`.

## Claimed Constructs
From `doc.go:7-73`, `README.md:35-104`, and function docstrings:

**Core parse output:**
| Construct | Source reference |
|---|---|
| Functions (with package_import_path, scip_symbol, return_type) | `language.go:62-99` |
| Methods (with class_context/receiver normalization) | `language.go:62-99` |
| Structs | `language.go:100-121` |
| Interfaces | `language.go:122-126` |
| Imports (with alias tracking, blank/dot exclusion) | `language.go:128-147` |
| Variables (package/module scope controlled) | `language.go:184-197` |
| Function calls (name, full_name, receiver metadata) | `language.go:148-171` |
| Composite-literal type references | `language.go:172-183` |
| Cyclomatic complexity | `helpers.go:43-68` (`goComplexitySet`) |
| Docstring extraction | `language.go:77-79` |
| Parameter count | `language.go:74` |

**Call metadata:**
| Construct | Source reference |
|---|---|
| Receiver identifier (selector base) | `language.go:347-375` (`goCallReceiverIdentifier`) |
| Import alias detection on receivers | `language.go:377-390` (`goIdentifierMatchesImportAlias`) |
| Stable symbol key on imported calls | `language.go:322-327` |
| Enclosing method receiver context | `language.go:392-400` (`goEnclosingMethodReceiver`) |
| Inferred obj_type (local receiver type) | `language.go:330-332` |
| AWS SDK service binding | `language.go:333-335` |
| Call chain metadata (x.Method1().Method2) | `call_chain_metadata.go:12-29` (`goAnnotateCallChainMetadata`) |
| Chained method return receiver proof | `call_chain_metadata.go:32-50` (`goMethodReturnChainReceiver`) |

**Dead-code root kinds (>13):**
| Root kind | Source reference |
|---|---|
| `go.net_http_handler_signature` | `dead_code_roots.go:142` |
| `go.cobra_run_signature` | `dead_code_roots.go:145` |
| `go.controller_runtime_reconcile_signature` | `dead_code_roots.go:148` |
| `go.net_http_handler_registration` | `dead_code_registrations.go:150` |
| `go.cobra_run_registration` | `dead_code_registrations.go:209,238` |
| `go.function_value_reference` | `dead_code_semantic_helpers.go:58` |
| `go.method_value_reference` | `dead_code_semantic_helpers.go:76` |
| `go.function_literal_reachable_call` | `dead_code_semantic_helpers.go:146` |
| `go.direct_method_call` | `dead_code_semantic_flows.go:210,217,223` |
| `go.interface_type_reference` | `dead_code_semantic_roots.go:101,117,137,141,225` |
| `go.interface_method_implementation` | `dead_code_semantic_roots.go:196` |
| `go.interface_implementation_type` | `dead_code_semantic_roots.go:197` |
| `go.generic_constraint_method` | `dead_code_semantic_roots.go:232` |
| `go.fmt_stringer_method` | `dead_code_semantic_flows.go:275` |
| `go.dependency_injection_callback` | `dead_code_semantic_roots.go:292,302` |
| `go.type_reference` | `dead_code_semantic_roots.go:113` |

**Additional payload buckets:**
| Construct | Source reference |
|---|---|
| Embedded SQL queries (database/sql, sqlx) | `embedded_sql.go:71-109` (`EmbeddedSQLQueries`) |
| Embedded shell commands (os/exec) | `embedded_shell.go:31-68` (`EmbeddedShellCommands`) |
| Dataflow functions (opt-in) | `language.go:213-217`, `cfg_emit.go`, `cfg_lower.go` |
| Taint findings (opt-in) | `language.go:218-220`, `cfg_taint_facts.go` |
| Interprocedural findings (opt-in) | `language.go:221-224`, `cfg_effects.go`, `cfg_interproc.go` |
| Dataflow summaries (opt-in, durable) | `language.go:225-228`, `cfg_emit.go` |
| Dataflow sources (opt-in) | `language.go:228-230` |

**Pre-scan and package interface:**
| Construct | Source reference |
|---|---|
| PreScan (functions, methods, structs, interfaces) | `prescan.go:21-56` |
| ImportedInterfaceParamMethods | `dead_code_semantic_helpers.go:347-368` |
| ExportedInterfaceParamMethods | `README.md:57-60` |
| ImportedDirectMethodCallRoots | `README.md:63-66` |
| LocalInterfaceImportedMethodReturns | `README.md:67-68` |
| GenericConstraintInterfaceNames | `README.md:68` |
| MethodDeclarationKeys | `README.md:68` |

## Verified-by-Test Constructs

**Core parse (`engine_test.go` in the parent; `golang/go_language_test.go` and the rows below in the child `golang_test` package):**
| Construct | Test reference |
|---|---|
| Full Go parse with all buckets | `engine_test.go:123` (`TestDefaultEngineParsePathGo`) |
| Functions with return_type | `golang/go_language_test.go` |
| Functions with package_import_path | `golang/go_function_package_identity_test.go:14` (`TestGoFunctionRowsCarryPackageImportPathWhenKnown`) |
| Functions omit blank package_import_path | `golang/go_function_package_identity_test.go:41` |
| Methods with scip_symbol | `golang/go_function_package_identity_test.go:68` |
| Package-qualified calls with stable_symbol_key | `golang/go_function_package_identity_test.go:97` |
| Shadowed import alias no stable_symbol_key | `golang/go_function_package_identity_test.go:126,161` |
| Nested module import path derivation | `golang/go_function_package_identity_test.go:197` |

**Dead-code roots (child `golang_test` package, `golang/go_dead_code_roots_test.go`, `golang/go_dead_code_registrations_test.go`, `golang/go_dead_code_interfaces_test.go`, etc.):**
| Construct | Test reference |
|---|---|
| HTTP handler registration roots | `golang/go_dead_code_registrations_test.go:14` |
| HTTP handler unknown receiver ignored | `golang/go_dead_code_registrations_test.go:72` |
| Cobra run/literal/assignment roots | `golang/go_dead_code_registrations_test.go:14` |
| Local interface root kinds | `golang/go_dead_code_interfaces_test.go:14` |
| Function-value roots | `golang/go_dead_code_function_values_test.go` |
| Function-literal scope (unused closure vs callback closure) | `golang/go_dead_code_function_literal_scope_test.go:14,51` |
| Package-interface roots | `golang/go_dead_code_package_interface_test.go` |
| Dogfood Terraform dead-code roots | `golang/go_dead_code_dogfood_roots_test.go` |
| Terraform gap roots (controller-runtime, etc.) | `golang/go_dead_code_terraform_gaps_test.go` |

**Call metadata (child `golang_test` package):**
| Construct | Test reference |
|---|---|
| Selector assignment receiver bindings skipped | `golang/go_call_metadata_receiver_assignment_test.go:14` |
| Aliased imports annotated | `golang/go_call_metadata_receiver_assignment_test.go:58` |
| Method-return chain receiver type | `golang/go_call_metadata_receiver_assignment_test.go:90` |
| Concrete interface assignment chain receiver | `golang/go_call_metadata_receiver_assignment_test.go:131` |
| Unproven interface parameter chain receiver skipped | `golang/go_call_metadata_receiver_assignment_test.go:176` |
| Ambiguous interface assignment chain receiver skipped | `golang/go_call_metadata_receiver_assignment_test.go:219` |
| Map receiver type detection | `golang/go_call_metadata_map_receiver_test.go` |

**Embedded SQL and shell (engine-level):**
| Construct | Test reference |
|---|---|
| Embedded SQL queries | `golang/go_embedded_sql_test.go:15` |
| Embedded shell commands | `golang/go_embedded_shell_test.go:15` |

**Dataflow/taint (child `golang_test` package, opt-in gate):**
| Construct | Test reference |
|---|---|
| Dataflow functions bucket | `golang/go_cfg_dataflow_test.go` |
| Dataflow sources bucket | `golang/go_cfg_dataflow_sources_test.go` |
| Taint source-to-SQL-sink | `golang/go_cfg_taint_test.go:56` |
| Taint wrong-kind sanitizer still tainted | `golang/go_cfg_taint_test.go:77` |
| Taint correct sanitizer suppresses | `golang/go_cfg_taint_test.go:100` |
| Field-sensitive source-to-sink | `golang/go_cfg_taint_test.go:126` |
| Pointer alias source/sanitizer | `golang/go_cfg_taint_test.go:158,185` |
| Container element source-to-sink | `golang/go_cfg_taint_test.go:218` |
| Closure capture source | `golang/go_cfg_taint_test.go:240` |
| Uncalled closure does not report | `golang/go_cfg_taint_test.go:263` |
| Closure local shadow does not capture | `golang/go_cfg_taint_test.go:286` |
| Taint off is byte-identical | `golang/go_cfg_taint_test.go:310` |
| Interprocedural findings across functions | `golang/go_cfg_interproc_test.go:18` |
| Interproc function IDs include repository ID | `golang/go_cfg_interproc_test.go:65` |
| Interproc no false edge from method call | `golang/go_cfg_interproc_test.go:108` |
| Interproc off is byte-identical | `golang/go_cfg_interproc_test.go:148` |
| Interproc no false edge from shadowed callee | `golang/go_cfg_interproc_test.go:175` |
| Interproc call before local shadow | `golang/go_cfg_interproc_test.go:214` |
| Dataflow summaries emit effects | `golang/go_cfg_dataflow_summaries_test.go:21` |
| Dataflow summaries sorted by ID | `golang/go_cfg_dataflow_summaries_test.go:111` |
| Dataflow summaries require repository ID | `golang/go_cfg_dataflow_summaries_test.go:150` |
| Dataflow summaries require package import path | `golang/go_cfg_dataflow_summaries_test.go:176` |

**Same-package unit tests (`package golang`):**
| Construct | Test reference |
|---|---|
| Local variable types | `golang/local_variable_types_test.go` |
| Local receiver types | `golang/local_receiver_types_test.go` |
| AWS SDK receiver service binding | `golang/aws_sdk_receiver_service_test.go` |
| CFG lowering | `golang/cfg_lower_test.go` |
| CFG guard text inspection | `golang/cfg_guard_text_test.go` |
| Embedded SQL (subdirectory) | `golang/embedded_sql_test.go` |
| Embedded shell alias shadowing | `golang/embedded_shell_test.go` |

**Performance and dogfood (child `golang_test` package):**
| Construct | Test reference |
|---|---|
| Go parent lookup benchmark | `golang/go_parent_lookup_bench_test.go` |
| Go package prescan benchmark | `golang/go_package_interface_prescan_bench_test.go` |
| Terraform dogfood parse + prescan | `golang/go_terraform_dogfood_test.go` |

**Package interface pre-scan (stays in the parent: white-box tests of parent-owned worker sizing):**
| Construct | Test reference |
|---|---|
| Package interface prescan | `go_package_interface_prescan_test.go` |

## Unverified / Claimed-but-Untested Constructs
- **`MethodDeclarationKeys`** — exported in `golang/package_interface_prescan.go`, documented in `README.md:68`. No dedicated test exercises it independently of the package interface prescan tests.
- **LiveComponent callback root** overlap risk — the Elixir comment applies, but for Go: `go.interface_method_implementation` via imported interface methods with `allowExportedMethods` (in `goMarkConcreteTypeForInterfaceTarget` at `dead_code_semantic_flows.go:164-170`) marks every exported method of a concrete type. This is tested only indirectly via the package interface prescan tests, not with a fixture that proves a false positive cannot occur (e.g., where not every exported method is actually called).
- **Struct field interface targets in composite literals** — `goMarkCompositeLiteralInterfaceFields` in `dead_code_semantic_flows.go:60-99` is exercised only through the broader dead-code root tests, not with specific fixtures for struct field name mismatch or empty keyed_element.
- **`LocalInterfaceMethods` and `GenericConstraintInterfaceNames`** — documented in `README.md:67-68` as exported functions; the generic constraint interface root (`goMarkGenericConstraintInterfaceRoots`) is tested through the dead-code interface tests, but the standalone export functions are not directly tested.
- **fmt.Stringer root for formatted values** — `goCollectFmtStringerRoot` in `dead_code_semantic_flows.go:252-278` determines which `fmt.Sprint*/Fprint*` arguments are value arguments. No test for `fmt.Fprintf` specifically (3-arg pattern) with a Stringer type.
- **AWS SDK receiver service** — tested in `golang/aws_sdk_receiver_service_test.go` but only for basic binding; no test for multiple AWS services imported in the same file with different aliases.

## Edge Cases Considered
- Empty parse: likely tested through the empty-file/system fixture tests
- Blank/dot import alias exclusion: `language.go:143`
- Shadowed import aliases for stable_symbol_key: `golang/go_function_package_identity_test.go:126,161`
- Ambiguous interface assignment chain receiver: `golang/go_call_metadata_receiver_assignment_test.go:219`
- Unproven interface parameter chain receiver: `golang/go_call_metadata_receiver_assignment_test.go:176`
- Concrete interface assignment: `golang/go_call_metadata_receiver_assignment_test.go:131`
- Function-literal scope (unused vs callback): `golang/go_dead_code_function_literal_scope_test.go:14,51`
- Closure local shadow does not capture: `golang/go_cfg_taint_test.go:286`
- Uncalled closure does not report: `golang/go_cfg_taint_test.go:263`
- Container element (array/slice/map) approximation: `golang/go_cfg_taint_test.go:218`
- Field-sensitive taint (field A vs field B): `golang/go_cfg_taint_test.go:126`
- Pointer alias normalization: `golang/go_cfg_taint_test.go:158,185`
- Taint off byte-identical: `golang/go_cfg_taint_test.go:310`
- Interproc off byte-identical: `golang/go_cfg_interproc_test.go:148`
- Interproc call before local shadow: `golang/go_cfg_interproc_test.go:214`
- Terraform dogfood perf regression gate: `golang/go_terraform_dogfood_test.go`
- Per-file amortization (parent lookup, variable type indices): bench tested via `golang/go_parent_lookup_bench_test.go` and Terraform dogfood

## Edge Cases NOT Considered
- Go file with only package declaration and no other declarations
- Go file with deeply nested type aliases in generic packages
- Import path collision (two imports with same base name, different paths)
- Method receivers that are type parameters (generic receivers)
- Channel direction types (`<-chan`, `chan<-`) in receiver contexts
- Generic type instantiation with complex type arguments (`Generic[map[string][]int]`)
- Embedded struct field promotion for interface method resolution
- `//go:build` constraint directives
- Build-tagged files with conditional compilation
- cgo preamble blocks
- Inline struct definitions (anonymous struct fields)
- Multiple AWS SDK services imported with same alias from different versions

## Verdict
**deep**

The Go parser has Eshu's broadest parser test suite. After the #6062 relocation it lives almost entirely in the child directory: 39 test files under `go/internal/parser/golang/` (13 same-package, 26 external `golang_test`), with one Go-named test file left at parser root because it exercises the cross-language prescan path rather than Go extraction alone. It includes 2 performance benchmarks and Terraform integration dogfood coverage.

Counts are reproducible: `ls go/internal/parser/golang/*_test.go | wc -l` for the file total, `rg -l '^package golang$' go/internal/parser/golang/*_test.go | wc -l` for the same-package share, and `rg --no-filename -o '^func Benchmark' go/internal/parser/golang/*_test.go | wc -l` for the benchmarks. Named tests cover parse output, call chain metadata, dead-code root kinds, embedded SQL and shell extraction, and the opt-in dataflow, taint, and interprocedural pipeline. The suite also covers shadowed imports, ambiguous receiver types, function-literal scope, closure capture, field sensitivity, and byte-identical opt-in gates.

## Recommended Actions
1. Add a focused test for `MethodDeclarationKeys` as an exported interface if it's intended to be called externally.
2. Add a `fmt.Fprintf` Stringer test case (3-arg pattern where the 3rd argument is the first value argument).
3. Add a test for multiple AWS SDK service imports with different aliases in one file.
4. Add a generic receiver (`func (r *R[T]) Method()`) test for method row with generic receiver normalization.
5. Add an anonymous struct field test for struct field type tracking.
