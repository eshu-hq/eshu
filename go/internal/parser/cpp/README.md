# C++ Parser Adapter

## Purpose

This package owns C++-specific tree-sitter payload extraction for functions,
classes, structs, enums, unions, includes, macros, typedef aliases, calls, and
dead-code root metadata.

## Ownership Boundary

The package receives a caller-owned tree-sitter parser from the parent parser
engine. It owns C++ syntax walking and payload assembly, while the parent package
keeps registry dispatch, runtime parser construction, and compatibility method
signatures.

## Exported Surface

The package exposes `Parse` for full payload extraction, `PreScan` for
dependency symbol discovery, and `AnnotatePublicHeaderRoots` for bounded
same-source local header root annotation after imports have been extracted.

## Dependencies

This package imports the shared parser helper package and tree-sitter types. It
must not import the parent parser package.

## Operational Notes

This package emits no telemetry directly. Parser timing and runtime observability
remain owned by the parent engine.

Dead-code roots are intentionally derived, not exact. `Parse` marks direct
evidence for `cpp.main_function`, virtual and override methods, direct callback
argument targets, direct function-pointer initializer targets, and Node
native-addon entrypoints.
`AnnotatePublicHeaderRoots` marks functions and methods declared in directly
included local headers as `cpp.public_header_api` roots after checking that the
header path stays inside the repository root. Namespace-qualified out-of-class
method definitions keep the rightmost class qualifier as `class_context`, so
direct local header declarations can match implementations under namespace
prefixes. It does not recurse through include graphs or resolve build targets,
template instantiations, overload sets, broad virtual dispatch, dynamic symbol
lookup, or external linkage.

### Regex disposition (issue #3540)

Two whole-source regex scans were converted to AST walks and removed:

- `appendCTypedefAliasesFromSource` (whole-source typedef line scan) was deleted;
  typedef aliases, structs, enums, and unions now come solely from tree-sitter
  `type_definition` and `declaration` nodes, proven parity-identical on the C++
  fixtures.
- `cppNodeAddonRegistrationPattern` (whole-source `NODE_MODULE(...)` scan) was
  replaced by `annotateCPPNodeAddonRegistrationRoot`, which walks
  `call_expression` nodes, matches the callee identifier against
  `cppNodeAddonMacros`, and reads the initializer from the second call argument.

The remaining regexes are documented within-AST / external-text exceptions, not
primary symbol extraction:

- `cTypedefAliasPattern` is a fallback over a `type_definition` node's own text.
- `cppQualifiedFunctionPattern`, `cppDirectInitializerTargetPattern`, and
  `cppBraceInitializerPattern` operate on the text of an already-located
  `function_definition` or `declaration` node.
- `cppFreeHeaderPrototypePattern`, `cppClassBlockPattern`,
  `cppClassMethodPrototypePattern`, and the comment-stripping patterns scan the
  bytes of directly included local header files, which are intentionally not
  tree-sitter parsed to keep cost bounded.
- `cppFunctionPointerAliasPattern` builds an auxiliary name index of
  function-pointer typedef/`using` aliases that gates the within-AST declaration
  handling; it produces no symbol or edge directly.
