// Package php parses PHP source evidence for the parent parser engine.
//
// Parse reads one PHP source file through shared.ReadSource and emits the
// legacy parser payload buckets for classes, traits, interfaces, functions,
// imports, variables, calls, and trait-use adaptations. PreScan returns
// declaration names from the same payload path so repository pre-scan and full
// parse stay aligned. Scope tracking preserves PSR-style type declarations
// whose opening brace appears on the next line, because method ownership is a
// parser contract for downstream root metadata. The package emits bounded
// dead-code root hints for syntax-proven PHP entrypoints, magic methods,
// interface and trait methods, controller actions, literal route handlers,
// Symfony route attributes, and WordPress hook callbacks; broader autoload,
// reflection, and dynamic-dispatch behavior stays non-exact. The package is
// deterministic and depends only on shared parser helpers.
package php
