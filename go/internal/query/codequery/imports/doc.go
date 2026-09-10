// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package imports implements import-dependency investigation for the
// code-family queries: the parameter map the Cypher builders send, the
// module-scope reads, and the row readers behind the investigation
// route's dispatch. It split out of package codequery (#6060 naming
// follow-up) so the investigation can be read, tested, and changed
// without pulling in the rest of the code surface, and so it can one
// day move into its own repo.
//
// The request type, validation, Cypher builders, and row shaping
// already live in codemodel (#6060 lane A L1); this leaf owns the
// execution half that still ran in codequery. The split keeps thin
// *CodeHandler forwarders in codequery/import_dependencies_execution.go
// -- four carry queryplan source_sha256 pins in
// query-source-coverage.yaml (with their QP-CODE-IMPORT entry ids) --
// and calls this leaf through qualification. The HTTP handler, the data
// dispatcher, and the exported parameter builder stay in codequery for
// the same reason.
//
// Import discipline: this package may import the same dependency-neutral
// leaves codequery uses (codemodel, querycontract) but NEVER package
// codequery itself and NEVER root package query -- both would create an
// import cycle (codequery calls this leaf, and root aliases codequery).
package imports
