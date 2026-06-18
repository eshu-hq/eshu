// Package java parses Java source and metadata files behind the parent parser
// dispatch package.
//
// Parse uses a caller-owned tree-sitter parser and shared Options to emit the
// Java payload buckets consumed by the collector. When Options.EmitDataflow is
// set, Parse also emits the opt-in shared value-flow buckets for a conservative
// Java taint subset: Spring request-parameter annotations backed by real Spring
// imports as sources, typed JDBC/JPA receiver calls backed by matching imports
// as SQL sinks, local Java summaries, parameter source rows, and same-file
// interprocedural findings. Durable summary and source rows require stable
// RepositoryID and Java package identity. PreScan returns source symbol names
// for dependency indexing. The parser keeps method-reference target evidence
// bounded to source-proven receivers, including unambiguous same-file declared
// Java types. ParseMetadata and
// MetadataClassReferences turn bounded ServiceLoader and Spring metadata files
// into ClassReference rows while rejecting unsupported paths, invalid class
// names, and duplicate evidence.
package java
