// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package db holds the shared database contracts for the Postgres storage
// layer: the row cursor (Rows), the read and write adapter surfaces (Queryer,
// Executor, ExecQueryer), and the transaction surface (Transaction, Beginner,
// ReadOnlyRepeatableReadBeginner).
//
// The package is a dependency leaf on purpose. It imports only the Go
// standard library, so domain stores can depend on these contracts without
// importing the postgres root package. If a store moved to a subpackage while
// these types stayed in root, the store would import root; once root also
// references that store, that is an import cycle. Hoisting the contracts here
// first removes that risk for every later domain move under #6693.
//
// The concrete adapters (SQLDB, SQLTx, SQLQueryer), the schema bootstrap and
// migration ledger, and the advisory-lock machinery stay in the root package
// with the types they guard. SQLDB.withSchemaBootstrapLock satisfies the
// package-private schemaBootstrapLocker contract asserted by
// applyBootstrapDefinitions, so moving SQLDB without the lock implementation
// would silently degrade bootstrap to unlocked DDL; the lock implementation
// in turn shares unexported helpers and ledger types with root tests. See
// docs/internal/design/storage-collector-tree.md for the recorded trap.
package db
