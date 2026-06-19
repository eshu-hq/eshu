// Package csharp owns C# parser extraction without depending on the parent
// parser dispatcher. Parse emits declarations, calls, inheritance metadata, and
// bounded dead-code root hints for directly visible C# runtime and framework
// entrypoints. Root hints are syntax-scoped to declarations, attributes,
// modifiers, base lists, qualified enclosing types, and same-file interface
// contracts with method arity; cross-project reflection, dependency injection,
// and generated code remain query-layer exactness blockers.
//
// When Options.EmitDataflow is set, Parse also attaches the opt-in value-flow
// buckets (dataflow_functions, taint_findings, interproc_findings, and, when a
// repository identity and namespace are present, the durable dataflow_summaries
// and dataflow_sources). The taint catalog is attribute/using-evidence verified:
// sources are ASP.NET Core model-binding parameters ([FromQuery], [FromBody],
// [FromRoute], [FromForm]) corroborated by a Microsoft.AspNetCore.Mvc using;
// sinks are ADO.NET SqlCommand execution (ExecuteReader, ExecuteNonQuery,
// ExecuteScalar) corroborated by a System.Data.SqlClient or
// Microsoft.Data.SqlClient using, plus Process.Start corroborated by a
// System.Diagnostics using. Receiver-type inference for sinks is intraprocedural
// and explicit-type only (parameters and explicitly-typed locals); a sink whose
// receiver is a var/implicit-typed local with no declared type is conservatively
// not matched, preferring honesty over a same-name false positive. The gate is
// off by default and a non-dataflow parse is byte-identical to a build without
// the feature. No sanitizers are catalogued in v1.
package csharp
