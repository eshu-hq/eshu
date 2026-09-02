# C# Parser Audit

## Overview
The C# parser (`go/internal/parser/csharp/`) extracts declarations (classes, interfaces, structs, enums, records, properties, functions), using directives, calls, inheritance metadata, and 9 bounded dead-code root kinds. It also includes an opt-in value-flow/taint subsystem (`EmitDataflow`) that lowers methods to CFGs, runs intraprocedural taint analysis, derives interprocedural summaries, and emits durable source/sink rows. Sources are ASP.NET Core model-binding parameters ([FromQuery], [FromBody], [FromRoute], [FromForm]) corroborated by a `Microsoft.AspNetCore.Mvc` using. Sinks are ADO.NET `SqlCommand` execution methods corroborated by `System.Data.SqlClient` or `Microsoft.Data.SqlClient` using, plus `Process.Start` corroborated by `System.Diagnostics` using. Engine-level C# coverage lives in `go/internal/parser/csharp/` as external package `csharp_test` (relocated from the parent root by #6062): 19 test functions across 4 files (`csharp_cfg_dataflow_test.go`, `csharp_dead_code_roots_test.go`, `csharp_route_semantics_test.go`, `engine_csharp_taint_test.go`) plus `csharp_test_helpers_test.go`, from `rg --no-filename -o '^func Test' csharp_cfg_dataflow_test.go csharp_dead_code_roots_test.go csharp_route_semantics_test.go engine_csharp_taint_test.go | wc -l`, run from `go/internal/parser/csharp/`; the package also keeps 1 in-package (`package csharp`) guarded test in `equivalence_dump_test.go`. The parent parser suite retains the cross-language C# cases: 2 tests in `engine_managed_oo_test.go` and the `csharp_*` cases in `engine_cyclomatic_complexity_test.go` and `engine_cyclomatic_complexity_arms_test.go`, which are parameterised over every language and were not relocated.

## Claimed Constructs
| Construct | Source Reference |
|---|---|
| `classes` | `language.go:39` (`class_declaration`) |
| `interfaces` | `language.go:41` (`interface_declaration`) |
| `structs` | `language.go:43` (`struct_declaration`) |
| `enums` | `language.go:45` (`enum_declaration`) |
| `records` | `language.go:47` (`record_declaration`) |
| `properties` | `language.go:49` (`property_declaration`) |
| `functions` (methods, constructors, local functions) | `language.go:51-62` (`appendFunctionWithContext`), `dataflow_summary.go:86-93` (`csharpIsCallableDeclaration`) |
| `imports` (using directives) | `language.go:63-72` |
| `function_calls` (invocation, object creation) | `language.go:73-78` |
| `bases` (base list) | `dead_code_roots.go:78-121` (`csharpBaseNames`) |
| `class_context` | `language.go:209-212` (`nearestNamedAncestorWithQualifiedKind`) |
| `decorators` (attributes) | `language.go:205` (`csharpAttributeNames` at `dead_code_syntax.go:45`) |
| `cyclomatic_complexity` | `complexity.go:34-36` |
| PreScan names | `language.go:94-101` |
| `dataflow_functions` (CFG rows) | `dataflow_emit.go:29,55-83` |
| `taint_findings` (intraprocedural) | `dataflow_emit.go:36-38,77-79` |
| `interproc_findings` | `dataflow_emit.go:39-41`, `dataflow_summary.go:75-80` |
| `dataflow_summaries` | `dataflow_emit.go:43-44`, `dataflow_summary.go:58-63` |
| `dataflow_sources` | `dataflow_emit.go:46-47`, `dataflow_summary.go:65-69` |
| `dataflow_catalog_versions` | `dataflow_emit.go:29-31` |

Dead-code root kinds claimed: `csharp.main_method`, `csharp.constructor`, `csharp.interface_method`, `csharp.interface_implementation_method`, `csharp.override_method`, `csharp.aspnet_controller_action`, `csharp.hosted_service_entrypoint`, `csharp.test_method`, `csharp.serialization_callback`.

Taint sources claimed: `[FromQuery]`, `[FromBody]`, `[FromRoute]`, `[FromForm]` (all require `Microsoft.AspNetCore.Mvc` using). Taint sinks claimed: `SqlCommand.ExecuteReader`/`ExecuteNonQuery`/`ExecuteScalar` (require `System.Data.SqlClient` or `Microsoft.Data.SqlClient` using), `Process.Start` (requires `System.Diagnostics` using).

## Verified-by-Test Constructs
| Construct | Test Reference |
|---|---|
| `classes`, `interfaces`, `records`, `properties`, `functions`, `imports`, `function_calls`, `bases` | `engine_managed_oo_test.go:130-181` (`TestDefaultEngineParsePathCSharp`) |
| `structs`, `enums`, local functions as `functions` | `engine_managed_oo_test.go:183-229` (`TestDefaultEngineParsePathCSharpLocalTypes`) |
| `csharp.main_method` (void, Task, Task<int>, fully-qualified, string[] args) | `csharp/csharp_dead_code_roots_test.go:152-154` |
| `csharp.main_method` negative: string return, wrong params, non-static | `csharp/csharp_dead_code_roots_test.go:169-173` |
| `csharp.constructor` | `csharp/csharp_dead_code_roots_test.go:144` |
| `csharp.interface_method` | `csharp/csharp_dead_code_roots_test.go:143` |
| `csharp.interface_implementation_method` (arity-disambiguated) | `csharp/csharp_dead_code_roots_test.go:145` |
| `csharp.override_method` | `csharp/csharp_dead_code_roots_test.go:146` |
| `csharp.aspnet_controller_action` (ControllerBase suffix, base type) | `csharp/csharp_dead_code_roots_test.go:147` |
| `csharp.aspnet_controller_action` negative: `[NonAction]`, private, not public | `csharp/csharp_dead_code_roots_test.go:168,160` |
| `csharp.hosted_service_entrypoint` (BackgroundService, IHostedService) | `csharp/csharp_dead_code_roots_test.go:148,155` |
| `csharp.hosted_service_entrypoint` in namespace | `csharp/csharp_dead_code_roots_test.go:115-121` |
| `csharp.hosted_service_entrypoint` negative: plain namespace, non-hosted | `csharp/csharp_dead_code_roots_test.go:123-129,175-176` |
| `csharp.test_method` (Fact, multiple attributes) | `csharp/csharp_dead_code_roots_test.go:149-150` |
| `csharp.serialization_callback` (OnDeserialized) | `csharp/csharp_dead_code_roots_test.go:151` |
| Negative: private helper, non-action method, text-only mentions NOT rooted | `csharp/csharp_dead_code_roots_test.go:156-161,167-170` |
| Negative: generic interface impl NOT matched by simple name | `csharp/csharp_dead_code_roots_test.go:169` (`Processor : IHandler<Order>`) |
| Dead-code fixture expected roots (separate fixture file) | `csharp/csharp_dead_code_roots_test.go:187-216` |
| Dataflow off: buckets absent, remainder byte-identical | `csharp/csharp_cfg_dataflow_test.go:30-74` |
| Intraprocedural taint: `[FromQuery]` → `SqlCommand.ExecuteReader` | `csharp/csharp_cfg_dataflow_test.go:76-88` |
| Same-named local source/sink WITHOUT using → NO findings | `csharp/csharp_cfg_dataflow_test.go:90-111` |
| Source WITHOUT `AspNetCore.Mvc` using → NO findings | `csharp/csharp_cfg_dataflow_test.go:113-132` |
| Interprocedural summaries (param→sink) | `csharp/csharp_cfg_dataflow_test.go:153-158` |
| Durable source rows | `csharp/csharp_cfg_dataflow_test.go:161-166` |
| Interproc cross-method findings | `csharp/csharp_cfg_dataflow_test.go:169-175` |
| `[FromBody]` taint source | `csharp/engine_csharp_taint_test.go:14-38` |
| `[FromRoute]` taint source | `csharp/engine_csharp_taint_test.go:40-64` |
| `[FromForm]` taint source | `csharp/engine_csharp_taint_test.go:66-90` |
| `SqlCommand.ExecuteNonQuery` sink | `csharp/engine_csharp_taint_test.go:92-116` |
| `SqlCommand.ExecuteScalar` sink | `csharp/engine_csharp_taint_test.go:118-142` |
| `Process.Start` sink (command_injection) | `csharp/engine_csharp_taint_test.go:144-169` |
| `Microsoft.Data.SqlClient` using for sink | `csharp/engine_csharp_taint_test.go:171-195` |
| `var`/implicit-typed local sink rejection (honesty contract) | `csharp/engine_csharp_taint_test.go:197-216` |
| `Fact`, `Theory`, `Test`, `TestMethod`, `SetUp`, `TearDown`, `OneTimeSetUp`, `OneTimeTearDown` test attributes | `csharp/engine_csharp_taint_test.go:218-276` |
| `OnSerializing`, `OnSerialized`, `OnDeserializing`, `OnDeserialized` serialization callbacks | `csharp/engine_csharp_taint_test.go:278-318` |
| ASP.NET attribute route entries (literal, `[NonAction]`, dynamic, non-controller excluded) | `csharp/csharp_route_semantics_test.go:14-77` |
| ASP.NET minimal API route entries (literal `Map*`, `MapMethods`, dynamic and lambda excluded) | `csharp/csharp_route_semantics_test.go:79-123` |
| `cyclomatic_complexity` | `engine_cyclomatic_complexity_test.go:103-110` (C# straight-line and branchy fixtures) |
| `cyclomatic_complexity` catch and default arms | `engine_cyclomatic_complexity_arms_test.go:52-118` (csharp_catch, csharp_case_default, csharp_only_default) |

## Unverified / Claimed-but-Untested Constructs
| Construct | Gap |
|---|---|
| Taint source WITH `AspNetCore.Mvc` using but WITHOUT the specific attribute import in `using` set | Not tested for boundary conditions on import matching. |
| Multiple sources in one method | Only single-source cases tested. |
| Multiple sinks in one method | Only single-sink cases tested. |
| CFG lowering of `try`/`catch`/`finally` in dataflow | Only tested structurally via `dataflow_lower.go`, not with a taint test. |
| `decorators` field content | Present on functions (line 206), but no explicit test asserts attribute names in decorators. |
| PreScan names for C# | Not explicitly tested in isolation. |

## Edge Cases Considered
| Edge Case | Test Reference |
|---|---|
| Dataflow gate off preserves byte-identical non-dataflow output | `csharp/csharp_cfg_dataflow_test.go:30-74` |
| Same-named local class/attribute without framework using NOT a source/sink | `csharp/csharp_cfg_dataflow_test.go:90-111` |
| Model-binding attribute without AspNetCore using NOT a source | `csharp/csharp_cfg_dataflow_test.go:113-132` |
| Interprocedural resolution across same-file calls | `csharp/csharp_cfg_dataflow_test.go:134-175` |
| Main with `Task`/`Task<int>`/`void`/`int` return types | `csharp/csharp_dead_code_roots_test.go:152-154` |
| Main with string return type excluded | `csharp/csharp_dead_code_roots_test.go:172` |
| Main with wrong parameter excluded | `csharp/csharp_dead_code_roots_test.go:173` |
| Non-static Main excluded | `csharp/csharp_dead_code_roots_test.go:170-171` (`TextOnlyRoots.Main`) |
| Local function named Main NOT rooted | `dead_code_roots.go:312` (checked, covered by TextOnlyRoots test) |
| Fully qualified return type on Main | `csharp/csharp_dead_code_roots_test.go:154` |
| `[NonAction]` attribute exclusion | `csharp/csharp_dead_code_roots_test.go:167-168` |
| Private method exclusion for controller actions | `csharp/csharp_dead_code_roots_test.go:160` |
| Controller base types (Controller, ControllerBase) and name suffix | `dead_code_roots.go:18-27` (tested via OrdersController : ControllerBase) |
| Interface method arity disambiguation (Run() vs Run(int)) | `csharp/csharp_dead_code_roots_test.go:171,174` |
| Multiple test attributes on one method | `csharp/csharp_dead_code_roots_test.go:150` |
| Body-text mentions of attributes NOT fooling detection | `csharp/csharp_dead_code_roots_test.go:169-170` (`TextOnlyRoots.MentionsFact`, `TextOnlyRoots.MentionsOverride`) |
| Generic interface NOT matched as implementation | `csharp/csharp_dead_code_roots_test.go:169` (`Processor : IHandler<Order>`) |
| Duplicate simple-named interfaces (count > 1) NOT matched | `dead_code_roots.go:182` (`interfaceSimpleNameCounts != 1` guard, exercised by fixture) |
| Hosted service in namespace | `csharp/csharp_dead_code_roots_test.go:115-121` |
| Plain namespace with same method name NOT rooted as hosted | `csharp/csharp_dead_code_roots_test.go:123-129,175-176` |
| Qualified type names for disambiguation | `dead_code_roots.go:158-159` (`types[qualifiedName]` + `typeSimpleNameCounts` guard) |
| `override` modifier detection from AST | `dead_code_syntax.go:135-141` (`hasModifier`) |
| Switch default arm excluded from complexity | `engine_cyclomatic_complexity_arms_test.go:111-118` |
| Catch clause counted as decision point | `engine_cyclomatic_complexity_arms_test.go:52` |
| `var`-typed local receiver NOT matched as sink | `csharp/engine_csharp_taint_test.go:197-216` |

## Edge Cases NOT Considered
No test covers: static classes, partial classes/methods, record structs, top-level statements (C# 9+), file-scoped namespaces in isolation, global using directives, nullable reference types, primary constructors (C# 12), required/init properties, pattern matching in CFG, async/await data flow, `yield return` in dataflow, LINQ expressions, extension methods, indexer declarations, event declarations, operator declarations, explicit interface implementations, nested types (qualified name built but not tested), delegate declarations, expression-bodied members, lock statements in CFG, `using` statements/declarations in CFG, tainted parameter through intermediate local variable, empty source files, invalid C# syntax, or very large files.

## Verdict
**Moderate** — The dead-code root classification is deep: all 9 root kinds are tested with positive and negative cases including arity disambiguation, qualified name guards, and body-text false-positive protection. Primary symbol extraction (classes, interfaces, structs, enums, records, properties, functions, imports, calls, bases) is verified. The intraprocedural taint analysis has a strong honesty test (same-name-no-using rejection). The taint catalog is now covered per entry: all 4 model-binding sources, all 3 SQL sink methods, the `Process.Start` sink, both SqlClient usings, the `var`-typed local rejection (honesty contract), all 8 test-method attributes, and all 4 serialization callbacks each have a dedicated test in `csharp/engine_csharp_taint_test.go`. Interprocedural summaries are tested with one scenario. The CFG lowering of try/catch/finally is structural-only (no taint test through exception handlers).

## Recommended Actions
1. Add taint test where tainted value flows through an intermediate local variable before reaching sink.
2. Add taint test with `try`/`catch`/`finally` to verify CFG lowering handles exception control flow.
3. Add explicit PreScan test for C#.
