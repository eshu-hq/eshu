# C# Parser Audit

## Overview
The C# parser (`go/internal/parser/csharp/`) extracts declarations (classes, interfaces, structs, enums, records, properties, functions), using directives, calls, inheritance metadata, and 9 bounded dead-code root kinds. It also includes an opt-in value-flow/taint subsystem (`EmitDataflow`) that lowers methods to CFGs, runs intraprocedural taint analysis, derives interprocedural summaries, and emits durable source/sink rows. Sources are ASP.NET Core model-binding parameters ([FromQuery], [FromBody], [FromRoute], [FromForm]) corroborated by a `Microsoft.AspNetCore.Mvc` using. Sinks are ADO.NET `SqlCommand` execution methods corroborated by `System.Data.SqlClient` or `Microsoft.Data.SqlClient` using, plus `Process.Start` corroborated by `System.Diagnostics` using. The parser has 2 dedicated parent-level test files (`csharp_dead_code_roots_test.go`, `csharp_cfg_dataflow_test.go`) plus 2 tests in `engine_managed_oo_test.go`, totaling 12 C#-specific subtests.

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
| `decorators` (attributes) | `language.go:206` (`csharpAttributeNames`) |
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
| `csharp.main_method` (void, Task, Task<int>, fully-qualified, string[] args) | `csharp_dead_code_roots_test.go:149-151` |
| `csharp.main_method` negative: string return, wrong params, non-static | `csharp_dead_code_roots_test.go:166-170` |
| `csharp.constructor` | `csharp_dead_code_roots_test.go:141` |
| `csharp.interface_method` | `csharp_dead_code_roots_test.go:140` |
| `csharp.interface_implementation_method` (arity-disambiguated) | `csharp_dead_code_roots_test.go:142` |
| `csharp.override_method` | `csharp_dead_code_roots_test.go:143` |
| `csharp.aspnet_controller_action` (ControllerBase suffix, base type) | `csharp_dead_code_roots_test.go:144` |
| `csharp.aspnet_controller_action` negative: `[NonAction]`, private, not public | `csharp_dead_code_roots_test.go:165,157` |
| `csharp.hosted_service_entrypoint` (BackgroundService, IHostedService) | `csharp_dead_code_roots_test.go:145,152` |
| `csharp.hosted_service_entrypoint` in namespace | `csharp_dead_code_roots_test.go:112-118` |
| `csharp.hosted_service_entrypoint` negative: plain namespace, non-hosted | `csharp_dead_code_roots_test.go:120-126,172-173` |
| `csharp.test_method` (Fact, multiple attributes) | `csharp_dead_code_roots_test.go:146-147` |
| `csharp.serialization_callback` (OnDeserialized) | `csharp_dead_code_roots_test.go:148` |
| Negative: private helper, non-action method, text-only mentions NOT rooted | `csharp_dead_code_roots_test.go:153-158,164-167` |
| Negative: generic interface impl NOT matched by simple name | `csharp_dead_code_roots_test.go:166` (`Processor : IHandler<Order>`) |
| Dead-code fixture expected roots (separate fixture file) | `csharp_dead_code_roots_test.go:184-213` |
| Dataflow off: buckets absent, remainder byte-identical | `csharp_cfg_dataflow_test.go:27-71` |
| Intraprocedural taint: `[FromQuery]` → `SqlCommand.ExecuteReader` | `csharp_cfg_dataflow_test.go:73-85` |
| Same-named local source/sink WITHOUT using → NO findings | `csharp_cfg_dataflow_test.go:87-108` |
| Source WITHOUT `AspNetCore.Mvc` using → NO findings | `csharp_cfg_dataflow_test.go:110-129` |
| Interprocedural summaries (param→sink) | `csharp_cfg_dataflow_test.go:150-155` |
| Durable source rows | `csharp_cfg_dataflow_test.go:158-163` |
| Interproc cross-method findings | `csharp_cfg_dataflow_test.go:166-172` |
| `cyclomatic_complexity` | `engine_cyclomatic_complexity_test.go:103-110` (C# straight-line and branchy fixtures) |
| `cyclomatic_complexity` catch and default arms | `engine_cyclomatic_complexity_arms_test.go:52-118` (csharp_catch, csharp_case_default, csharp_only_default) |

## Unverified / Claimed-but-Untested Constructs
| Construct | Gap |
|---|---|
| `Process.Start` sink (command_injection) | Never tested. Only `SqlCommand.ExecuteReader` is tested as a sink. |
| `SqlCommand.ExecuteNonQuery` sink | Never tested individually. Only `ExecuteReader` is tested. |
| `SqlCommand.ExecuteScalar` sink | Never tested individually. |
| `[FromBody]` taint source | Only `[FromQuery]` is tested. |
| `[FromRoute]` taint source | Never tested. |
| `[FromForm]` taint source | Never tested. |
| `Test`, `TestMethod`, `SetUp`, `TearDown`, `OneTimeSetUp`, `OneTimeTearDown` test attributes | Only `Fact` is verified as a test-method attribute. |
| `OnSerializing`, `OnSerialized`, `OnDeserializing` serialization callbacks | Only `OnDeserialized` is verified. |
| `Microsoft.Data.SqlClient` using for sink | Only `System.Data.SqlClient` is tested. |
| `var`/implicit-typed local sink rejection | The honesty contract states `var` locals are not matched, but no test proves this behavior. |
| Taint source WITH `AspNetCore.Mvc` using but WITHOUT the specific attribute import in `using` set | Not tested for boundary conditions on import matching. |
| Multiple sources in one method | Only single-source cases tested. |
| Multiple sinks in one method | Only single-sink cases tested. |
| CFG lowering of `try`/`catch`/`finally` in dataflow | Only tested structurally via `dataflow_lower.go`, not with a taint test. |
| `decorators` field content | Present on functions (line 206), but no explicit test asserts attribute names in decorators. |
| PreScan names for C# | Not explicitly tested in isolation. |

## Edge Cases Considered
| Edge Case | Test Reference |
|---|---|
| Dataflow gate off preserves byte-identical non-dataflow output | `csharp_cfg_dataflow_test.go:27-71` |
| Same-named local class/attribute without framework using NOT a source/sink | `csharp_cfg_dataflow_test.go:87-108` |
| Model-binding attribute without AspNetCore using NOT a source | `csharp_cfg_dataflow_test.go:110-129` |
| Interprocedural resolution across same-file calls | `csharp_cfg_dataflow_test.go:131-172` |
| Main with `Task`/`Task<int>`/`void`/`int` return types | `csharp_dead_code_roots_test.go:149-151` |
| Main with string return type excluded | `csharp_dead_code_roots_test.go:169` |
| Main with wrong parameter excluded | `csharp_dead_code_roots_test.go:170` |
| Non-static Main excluded | `csharp_dead_code_roots_test.go:167-168` (`TextOnlyRoots.Main`) |
| Local function named Main NOT rooted | `dead_code_roots.go:312` (checked, covered by TextOnlyRoots test) |
| Fully qualified return type on Main | `csharp_dead_code_roots_test.go:151` |
| `[NonAction]` attribute exclusion | `csharp_dead_code_roots_test.go:164-165` |
| Private method exclusion for controller actions | `csharp_dead_code_roots_test.go:157` |
| Controller base types (Controller, ControllerBase) and name suffix | `dead_code_roots.go:18-27` (tested via OrdersController : ControllerBase) |
| Interface method arity disambiguation (Run() vs Run(int)) | `csharp_dead_code_roots_test.go:168,171` |
| Multiple test attributes on one method | `csharp_dead_code_roots_test.go:147` |
| Body-text mentions of attributes NOT fooling detection | `csharp_dead_code_roots_test.go:166-167` (`TextOnlyRoots.MentionsFact`, `TextOnlyRoots.MentionsOverride`) |
| Generic interface NOT matched as implementation | `csharp_dead_code_roots_test.go:166` (`Processor : IHandler<Order>`) |
| Duplicate simple-named interfaces (count > 1) NOT matched | `dead_code_roots.go:182` (`interfaceSimpleNameCounts != 1` guard, exercised by fixture) |
| Hosted service in namespace | `csharp_dead_code_roots_test.go:112-118` |
| Plain namespace with same method name NOT rooted as hosted | `csharp_dead_code_roots_test.go:120-126,172-173` |
| Qualified type names for disambiguation | `dead_code_roots.go:158-159` (`types[qualifiedName]` + `typeSimpleNameCounts` guard) |
| `override` modifier detection from AST | `dead_code_syntax.go:135-141` (`hasModifier`) |
| Switch default arm excluded from complexity | `engine_cyclomatic_complexity_arms_test.go:111-118` |
| Catch clause counted as decision point | `engine_cyclomatic_complexity_arms_test.go:52` |

## Edge Cases NOT Considered
No test covers: static classes, partial classes/methods, record structs, top-level statements (C# 9+), file-scoped namespaces in isolation, global using directives, nullable reference types, primary constructors (C# 12), required/init properties, pattern matching in CFG, async/await data flow, `yield return` in dataflow, LINQ expressions, extension methods, indexer declarations, event declarations, operator declarations, explicit interface implementations, nested types (qualified name built but not tested), delegate declarations, expression-bodied members, lock statements in CFG, `using` statements/declarations in CFG, tainted parameter through intermediate local variable, `var`-typed local rejection (honesty contract claims it but not tested), `Microsoft.Data.SqlClient` as using evidence, `Process.Start` as a sink, all 4 binding attributes tested individually, empty source files, invalid C# syntax, or very large files.

## Verdict
**Moderate** — The dead-code root classification is deep: all 9 root kinds are tested with positive and negative cases including arity disambiguation, qualified name guards, and body-text false-positive protection. Primary symbol extraction (classes, interfaces, structs, enums, records, properties, functions, imports, calls, bases) is verified. The intraprocedural taint analysis has a strong honesty test (same-name-no-using rejection). However, the taint catalog has significant coverage gaps: only 1 of 4 sources is tested, only 1 of 3 SQL sink methods is tested, the `Process.Start` sink is completely untested, only 1 of 8 test-method attributes is verified, and only 1 of 4 serialization callback attributes is verified. The `var`-typed local rejection (honesty contract) has no test. Interprocedural summaries are tested with one scenario. The CFG lowering of try/catch/finally is structural-only (no taint test through exception handlers).

## Recommended Actions
1. Add taint tests for `[FromBody]`, `[FromRoute]`, `[FromForm]` sources individually.
2. Add taint test for `SqlCommand.ExecuteNonQuery` and `SqlCommand.ExecuteScalar` sinks.
3. Add taint test for `Process.Start` as a `command_injection` sink with `System.Diagnostics` using.
4. Add test proving `var`-typed locals do NOT match sink receiver types (honesty contract).
5. Add test for `Microsoft.Data.SqlClient` as alternative using evidence for sinks.
6. Add test for each test-method attribute (`Test`, `TestMethod`, `SetUp`, `TearDown`, `OneTimeSetUp`, `OneTimeTearDown`).
7. Add test for each serialization callback attribute (`OnSerializing`, `OnSerialized`, `OnDeserializing`).
8. Add taint test where tainted value flows through an intermediate local variable before reaching sink.
9. Add taint test with `try`/`catch`/`finally` to verify CFG lowering handles exception control flow.
10. Add explicit PreScan test for C#.
