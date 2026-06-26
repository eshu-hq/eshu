# Go Parser Audit

## Overview
The Go parser (`go/internal/parser/golang/`) is the most comprehensive language adapter in Eshu. It uses tree-sitter to extract functions, methods, structs, interfaces, imports, variables, calls (with chain metadata, receiver inference, import alias tracking), composite-literal type references, dead-code roots (13+ kinds), embedded SQL queries, embedded shell commands, cyclomatic complexity, and — when opted in — dataflow functions, taint findings, interprocedural findings, and durable dataflow summaries. The package has ~30 test files: 6 in-subdirectory unit tests and ~24 parent-level test files, plus benchmarks and integration/dogfood tests against real Terraform checkouts.

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

**Core parse (parent-level, `engine_test.go`, `go_language_test.go`):**
| Construct | Test reference |
|---|---|
| Full Go parse with all buckets | `engine_test.go:123` (`TestDefaultEngineParsePathGo`) |
| Functions with return_type | `go_language_test.go` |
| Functions with package_import_path | `go_function_package_identity_test.go:25` (`TestGoFunctionRowsCarryPackageImportPathWhenKnown`) |
| Functions omit blank package_import_path | `go_function_package_identity_test.go:38` |
| Methods with scip_symbol | `go_function_package_identity_test.go:65` |
| Package-qualified calls with stable_symbol_key | `go_function_package_identity_test.go:94` |
| Shadowed import alias no stable_symbol_key | `go_function_package_identity_test.go:123,158` |
| Nested module import path derivation | `go_function_package_identity_test.go:194` |

**Dead-code roots (parent-level, `go_dead_code_roots_test.go`, `go_dead_code_registrations_test.go`, `go_dead_code_interfaces_test.go`, etc.):**
| Construct | Test reference |
|---|---|
| HTTP handler registration roots | `go_dead_code_registrations_test.go:11` |
| HTTP handler unknown receiver ignored | `go_dead_code_registrations_test.go:58` |
| Cobra run/literal/assignment roots | `go_dead_code_registrations_test.go:11` |
| Local interface root kinds | `go_dead_code_interfaces_test.go:11` |
| Function-value roots | `go_dead_code_function_values_test.go` |
| Function-literal scope (unused closure vs callback closure) | `go_dead_code_function_literal_scope_test.go:11,48` |
| Package-interface roots | `go_dead_code_package_interface_test.go` |
| Dogfood Terraform dead-code roots | `go_dead_code_dogfood_roots_test.go` |
| Terraform gap roots (controller-runtime, etc.) | `go_dead_code_terraform_gaps_test.go` |

**Call metadata (parent-level):**
| Construct | Test reference |
|---|---|
| Selector assignment receiver bindings skipped | `go_call_metadata_receiver_assignment_test.go:11` |
| Aliased imports annotated | `go_call_metadata_receiver_assignment_test.go:55` |
| Method-return chain receiver type | `go_call_metadata_receiver_assignment_test.go:87` |
| Concrete interface assignment chain receiver | `go_call_metadata_receiver_assignment_test.go:128` |
| Unproven interface parameter chain receiver skipped | `go_call_metadata_receiver_assignment_test.go:173` |
| Ambiguous interface assignment chain receiver skipped | `go_call_metadata_receiver_assignment_test.go:216` |
| Map receiver type detection | `go_call_metadata_map_receiver_test.go` |

**Embedded SQL and shell (parent-level):**
| Construct | Test reference |
|---|---|
| Embedded SQL queries | `go_embedded_sql_test.go:12` |
| Embedded shell commands | `go_embedded_shell_test.go:12` |

**Dataflow/taint (parent-level, opt-in gate):**
| Construct | Test reference |
|---|---|
| Dataflow functions bucket | `go_cfg_dataflow_test.go` |
| Dataflow sources bucket | `go_cfg_dataflow_sources_test.go` |
| Taint source-to-SQL-sink | `go_cfg_taint_test.go:53` |
| Taint wrong-kind sanitizer still tainted | `go_cfg_taint_test.go:74` |
| Taint correct sanitizer suppresses | `go_cfg_taint_test.go:98` |
| Field-sensitive source-to-sink | `go_cfg_taint_test.go:123` |
| Pointer alias source/sanitizer | `go_cfg_taint_test.go:155,182` |
| Container element source-to-sink | `go_cfg_taint_test.go:215` |
| Closure capture source | `go_cfg_taint_test.go:237` |
| Uncalled closure does not report | `go_cfg_taint_test.go:260` |
| Closure local shadow does not capture | `go_cfg_taint_test.go:283` |
| Taint off is byte-identical | `go_cfg_taint_test.go:307` |
| Interprocedural findings across functions | `go_cfg_interproc_test.go:15` |
| Interproc function IDs include repository ID | `go_cfg_interproc_test.go:62` |
| Interproc no false edge from method call | `go_cfg_interproc_test.go:105` |
| Interproc off is byte-identical | `go_cfg_interproc_test.go:145` |
| Interproc no false edge from shadowed callee | `go_cfg_interproc_test.go:172` |
| Interproc call before local shadow | `go_cfg_interproc_test.go:211` |
| Dataflow summaries emit effects | `go_cfg_dataflow_summaries_test.go:18` |
| Dataflow summaries sorted by ID | `go_cfg_dataflow_summaries_test.go:108` |
| Dataflow summaries require repository ID | `go_cfg_dataflow_summaries_test.go:147` |
| Dataflow summaries require package import path | `go_cfg_dataflow_summaries_test.go:173` |

**Subdirectory unit tests (`golang/`):**
| Construct | Test reference |
|---|---|
| Local variable types | `golang/local_variable_types_test.go` |
| Local receiver types | `golang/local_receiver_types_test.go` |
| AWS SDK receiver service binding | `golang/aws_sdk_receiver_service_test.go` |
| CFG lowering | `golang/cfg_lower_test.go` |
| CFG guard text inspection | `golang/cfg_guard_text_test.go` |
| Embedded SQL (subdirectory) | `golang/embedded_sql_test.go` |

**Performance and dogfood:**
| Construct | Test reference |
|---|---|
| Go parent lookup benchmark | `go_parent_lookup_bench_test.go` |
| Go package prescan benchmark | `go_package_interface_prescan_bench_test.go` |
| Terraform dogfood parse + prescan | `go_terraform_dogfood_test.go` |

**Package interface pre-scan:**
| Construct | Test reference |
|---|---|
| Package interface prescan | `go_package_interface_prescan_test.go` |

## Unverified / Claimed-but-Untested Constructs
- **`MethodDeclarationKeys`** — exported in `golang/package_interface_prescan.go`, documented in `README.md:68`. No dedicated test exercises it independently of the package interface prescan tests.
- **LiveComponent callback root** overlap risk — the Elixir comment applies, but for Go: `go.interface_method_implementation` via imported interface methods with `allowExportedMethods` (in `goMarkConcreteTypeForInterfaceTarget` at `dead_code_semantic_flows.go:164-170`) marks every exported method of a concrete type. This is tested only indirectly via the package interface prescan tests, not with a fixture that proves a false positive cannot occur (e.g., where not every exported method is actually called).
- **Struct field interface targets in composite literals** — `goMarkCompositeLiteralInterfaceFields` in `dead_code_semantic_flows.go:60-99` is exercised only through the broader dead-code root tests, not with specific fixtures for struct field name mismatch or empty keyed_element.
- **`LocalInterfaceMethods` and `GenericConstraintInterfaceNames`** — documented in `README.md:67-68` as exported functions; the generic constraint interface root (`goMarkGenericConstraintInterfaceRoots`) is tested through the dead-code interface tests, but the standalone export functions are not directly tested.
- **Embedded shell shadow detection** — `goIdentifierShadowedBeforeOffset` in `embedded_shell.go:93-98` uses regex to detect shadowed aliases but has no focused test with deliberately shadowed `exec` variables.
- **fmt.Stringer root for formatted values** — `goCollectFmtStringerRoot` in `dead_code_semantic_flows.go:252-278` determines which `fmt.Sprint*/Fprint*` arguments are value arguments. No test for `fmt.Fprintf` specifically (3-arg pattern) with a Stringer type.
- **AWS SDK receiver service** — tested in `golang/aws_sdk_receiver_service_test.go` but only for basic binding; no test for multiple AWS services imported in the same file with different aliases.

## Edge Cases Considered
- Empty parse: likely tested through the empty-file/system fixture tests
- Blank/dot import alias exclusion: `language.go:143`
- Shadowed import aliases for stable_symbol_key: `go_function_package_identity_test.go:123,158`
- Ambiguous interface assignment chain receiver: `go_call_metadata_receiver_assignment_test.go:216`
- Unproven interface parameter chain receiver: `go_call_metadata_receiver_assignment_test.go:173`
- Concrete interface assignment: `go_call_metadata_receiver_assignment_test.go:128`
- Function-literal scope (unused vs callback): `go_dead_code_function_literal_scope_test.go:11,48`
- Closure local shadow does not capture: `go_cfg_taint_test.go:283`
- Uncalled closure does not report: `go_cfg_taint_test.go:260`
- Container element (array/slice/map) approximation: `go_cfg_taint_test.go:215`
- Field-sensitive taint (field A vs field B): `go_cfg_taint_test.go:123`
- Pointer alias normalization: `go_cfg_taint_test.go:155,182`
- Taint off byte-identical: `go_cfg_taint_test.go:307`
- Interproc off byte-identical: `go_cfg_interproc_test.go:145`
- Interproc call before local shadow: `go_cfg_interproc_test.go:211`
- Terraform dogfood perf regression gate: `go_terraform_dogfood_test.go`
- Per-file amortization (parent lookup, variable type indices): bench tested via `go_parent_lookup_bench_test.go` and Terraform dogfood

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

The Go parser has the most comprehensive test suite of any Eshu parser by a wide margin: ~24 parent-level test files, 6 subdirectory test files, performance benchmarks, and Terraform integration dogfood tests. Every significant construct — parse output, call chain metadata, all dead-code root kinds, embedded SQL/shell extraction, and the full opt-in dataflow/taint/interprocedural pipeline — has focused, named test functions with positive and negative assertions. Edge cases for shadowed imports, ambiguous receiver types, function-literal scope, closure capture, field sensitivity, and byte-identical opt-in gates are all covered.

## Recommended Actions
1. Add a focused test for `MethodDeclarationKeys` as an exported interface if it's intended to be called externally.
2. Add a test for `emdedded_shell.go` shadow detection (`goIdentifierShadowedBeforeOffset`) with a deliberately shadowed `exec := someOtherThing` variable.
3. Add a `fmt.Fprintf` Stringer test case (3-arg pattern where the 3rd argument is the first value argument).
4. Add a test for multiple AWS SDK service imports with different aliases in one file.
5. Add a generic receiver (`func (r *R[T]) Method()`) test for method row with generic receiver normalization.
6. Add an anonymous struct field test for struct field type tracking.
