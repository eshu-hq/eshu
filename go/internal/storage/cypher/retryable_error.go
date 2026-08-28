// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"errors"
	"strings"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const malformedNeo4jConnectivityErrorMessage = "neo4j connectivity error is missing its underlying cause"

const (
	nornicDBRestartTransactionStartCode = "Neo.ClientError.Transaction.TransactionStartFailed"
	nornicDBRestartTransactionStartMsg  = "failed to write WAL tx begin: wal: closed"
	// nornicDBEngineClosedTransactionStartMsg is the SECOND spelling NornicDB
	// uses for the same begin-side teardown, reported when the engine itself is
	// already closed rather than only its WAL. Observed live in the
	// restart-backend-between-phase-groups cell:
	//   Neo.ClientError.Transaction.TransactionStartFailed
	//   (failed to start transaction: engine is closed)
	// dead-lettered a gcp_relationship_materialization write as
	// failure_class=projection_bug. Both spellings mean no transaction body has
	// run, so replay is equally safe; matching only the WAL wording made the
	// restart cell intermittently fail on the very fault it injects, and did so
	// as a terminal classification rather than a retry.
	nornicDBEngineClosedTransactionStartMsg = "failed to start transaction: engine is closed"
	// nornicDBStoreClosingCommitMsg is the body NornicDB reports under
	// nornicDBTransactionCommitFailedCode when its embedded Badger store has
	// already blocked writes for shutdown, so the commit is refused rather than
	// attempted. "Writes are blocked, possibly due to DropAll or Close" is
	// Badger's own exported ErrBlockedWrites sentence, which is why matching it
	// is stable across NornicDB builds; the code is checked alongside it so an
	// unrelated commit failure stays terminal.
	nornicDBStoreClosingCommitMsg = "Writes are blocked, possibly due to DropAll or Close"
	// nornicDBStoreClosedMsg is what NornicDB reports once the store is closed
	// rather than merely closing: Badger's own ErrDBClosed sentence
	// ("DB Closed", dgraph-io/badger/v4 v4.9.2 errors.go:116, the Badger
	// version NornicDB v1.1.11 requires).
	//
	// That text reaches Eshu on two paths, but only ONE guard below matches
	// this bare constant -- isNornicDBStoreClosedStatementFailure. The
	// commit-side guard matches the full operation-prefixed spellings declared
	// after this constant instead, because the bare tail is not safe under the
	// commit code; nornicDBStoreClosedCommitMsg carries the reason.
	//
	// The path this constant serves is the UNWIND MERGE chain fast path, which
	// reports the closed store mid-statement, e.g.
	// "UNWIND MERGE chain create failed: checking node existence: reading node:
	// DB Closed". It arrives under nornicDBStatementSyntaxErrorCode -- the same
	// code a genuinely malformed query uses -- so the message, not the code, is
	// what separates a backend teardown from a Cypher bug here.
	nornicDBStoreClosedMsg = "DB Closed"
	// nornicDBStoreClosedCommitMsg is the commit-path spelling of the same
	// condition, matched with its operation prefix rather than by its
	// "DB Closed" tail.
	//
	// The tail alone is NOT safe under the commit code. A genuine constraint
	// violation is reported there with the conflicting identity inlined --
	// `Node with uid="..." already exists` -- and identities are
	// evidence-derived, so one can carry a repo-relative path or any other
	// text, "DB Closed" included. Matching the tail would classify that
	// terminal schema conflict as a backend restart; the queue would retry it
	// and apply backpressure until the attempt budget was spent, turning a
	// loud stop into a slow one.
	//
	// The statement-side guard can use the bare tail because its code is
	// Statement.SyntaxError, which a schema conflict never carries (#6162).
	//
	// PROVENANCE for this spelling and its sibling below, so both literals can
	// be checked against the backend without a live run. At NornicDB v1.1.11 --
	// the tag deploy/helm/eshu/values.yaml pins -- BadgerTransaction.Commit
	// (pkg/storage/badger_transaction.go:1610) makes two store calls back to
	// back and wraps each with its own operation prefix:
	//
	//	badger_transaction.go:1680  fmt.Errorf("allocating mvcc commit version: %w", err)
	//	badger_transaction.go:1685  fmt.Errorf("materializing mvcc commit state: %w", err)
	//
	// The %w on a closed store is Badger's ErrDBClosed, "DB Closed"
	// (nornicDBStoreClosedMsg above). The outer "commit failed: " that the
	// driver reports comes from pkg/cypher/executor.go:2168, whose own comment
	// calls that substring a wire contract for downstream Bolt classifiers.
	// Composing those three gives the two literals verbatim.
	nornicDBStoreClosedCommitMsg = "materializing mvcc commit state: DB Closed"
	// nornicDBStoreClosedCommitAllocMsg is the sibling shape at the same commit
	// site, wrapped at badger_transaction.go:1680 (see the provenance block
	// above). It is the earlier of the two calls: Commit allocates the MVCC
	// version immediately before materializing it.
	//
	// The allocation half reaches the store only on a namespace cache MISS.
	// allocateMVCCVersion (pkg/storage/badger.go:1007) delegates to
	// namespaceMVCC (pkg/storage/badger_mvcc_per_namespace.go:101), which
	// returns a cached namespaceMVCCState from memory on a hit and touches no
	// store. Only the first write to a namespace in a process falls through to
	// loadPersistedNamespaceSequence (badger_mvcc_per_namespace.go:251) and
	// recoverNamespaceMVCCFloor (:275); both run b.db.View, which answers
	// "DB Closed" on an already-closed store. Rarer than its sibling, which is
	// why the live dead-letter surfaced the other one first, but the same
	// backend restart and the same retry decision.
	nornicDBStoreClosedCommitAllocMsg = "allocating mvcc commit version: DB Closed"
)

var errMalformedNeo4jConnectivity = errors.New(malformedNeo4jConnectivityErrorMessage)

// retryableNeo4jCodes lists Neo4j error codes that are safe to retry in
// reducer materialization paths. Scoped narrowly to codes evidenced as
// transient under concurrent projector/reducer graph access.
//
// See docs/public/reference/service-workflows.md and
// docs/public/deployment/service-runtimes.md for the current shared-write and
// reduction-flow contract behind these retry classifications.
var retryableNeo4jCodes = map[string]bool{
	"Neo.ClientError.Statement.EntityNotFound":        true,
	"Neo.TransientError.Transaction.DeadlockDetected": true,
}

// neo4jRetryableError wraps a Neo4j error and implements
// reducer.RetryableError for codes evidenced as transient in concurrent
// projector/reducer access patterns.
type neo4jRetryableError struct {
	inner error
	code  string
}

func (e *neo4jRetryableError) Error() string   { return e.inner.Error() }
func (e *neo4jRetryableError) Unwrap() error   { return e.inner }
func (e *neo4jRetryableError) Retryable() bool { return true }

// FailureClass reports the durable graph-write-timeout failure class so a
// transient driver-retry graph write (deadlock budget exhausted, connectivity
// loss) is recorded on the retrying row under the same class as a bounded
// graph-write deadline. Producer write-timeout backpressure (#3560) scopes its
// pressure signal to this class, which keeps a graph-write retry distinguishable
// from a reducer readiness backlog that also persists as a retrying row.
func (e *neo4jRetryableError) FailureClass() string { return GraphWriteTimeoutFailureClass }

// schemaFenceError marks a canonical write refused because the applied graph
// schema no longer admits this writer (see
// CanonicalNodeWriter.WithSchemaWriteFence).
//
// It is retryable rather than terminal on purpose. The refusal says this
// process must not write, not that the work is bad: the queue should hold it
// for a writer the schema does admit, which is the pod that replaces this one.
// Dead-lettering instead would turn a rolling upgrade into a backlog an
// operator has to redrive by hand.
//
// It carries no FailureClass. The graph-write-timeout class exists for
// transient driver failures and feeds producer write-timeout backpressure;
// a schema refusal is neither, and slowing producers would not clear it.
type schemaFenceError struct {
	inner error
}

func (e *schemaFenceError) Error() string   { return e.inner.Error() }
func (e *schemaFenceError) Unwrap() error   { return e.inner }
func (e *schemaFenceError) Retryable() bool { return true }

// WrapRetryableNeo4jError inspects err for graph-write failures that are safe
// to retry from the durable reducer queue. It wraps known retryable Neo4j error
// codes, driver retry-budget exhaustion, connectivity failures, and the three
// NornicDB failures a backend restart emits: the transaction-start failure
// raised once the WAL is closed or the engine is closed (matched on exact
// messages), the commit failure raised once the store has blocked writes for
// shutdown or is closed outright, and the statement failure raised once the
// store is closed outright (the latter two matched on a distinguishing
// substring, since their bodies carry variable context). Every one requires its
// error code as well. Malformed connectivity errors remain terminal, and all
// other errors are returned unchanged.
func WrapRetryableNeo4jError(err error) error {
	if err == nil {
		return nil
	}
	if isMalformedNeo4jConnectivityError(err) {
		return errMalformedNeo4jConnectivity
	}
	// TransactionExecutionLimit means session.ExecuteWrite exhausted its
	// internal retry budget (typically 30s for deadlocks). The queue should
	// retry later when contention subsides.
	var txLimit *neo4jdriver.TransactionExecutionLimit
	if errors.As(err, &txLimit) {
		return &neo4jRetryableError{inner: err, code: "TransactionExecutionLimit"}
	}
	var connectivityErr *neo4jdriver.ConnectivityError
	if errors.As(err, &connectivityErr) {
		return &neo4jRetryableError{inner: err, code: "ConnectivityError"}
	}
	if isNornicDBRestartTransactionStartFailure(err) {
		return &neo4jRetryableError{inner: err, code: nornicDBRestartTransactionStartCode}
	}
	if isNornicDBStoreClosingCommitFailure(err) {
		return &neo4jRetryableError{inner: err, code: nornicDBTransactionCommitFailedCode}
	}
	if isNornicDBStoreClosedStatementFailure(err) {
		return &neo4jRetryableError{inner: err, code: nornicDBStatementSyntaxErrorCode}
	}
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) {
		return err
	}
	if retryableNeo4jCodes[neo4jErr.Code] {
		return &neo4jRetryableError{inner: err, code: neo4jErr.Code}
	}
	return err
}

func isMalformedNeo4jConnectivityError(err error) bool {
	var connectivityErr *neo4jdriver.ConnectivityError
	return errors.As(err, &connectivityErr) && connectivityErr.Inner == nil
}

// isNornicDBRestartTransactionStartFailure recognizes the begin-side failures a
// backend restart produces. NornicDB reports the same condition under two
// message spellings -- the WAL closing ahead of a new transaction, and the
// engine already being closed -- and this accepts both. No transaction body has
// run in either case, so immediate and durable queue replay are equally safe.
// Keep the message guard alongside the code because NornicDB reports this
// backend-unavailable condition under a ClientError prefix, which would
// otherwise classify as a terminal projection bug.
func isNornicDBRestartTransactionStartFailure(err error) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	return errors.As(err, &neo4jErr) &&
		neo4jErr.Code == nornicDBRestartTransactionStartCode &&
		(neo4jErr.Msg == nornicDBRestartTransactionStartMsg ||
			neo4jErr.Msg == nornicDBEngineClosedTransactionStartMsg)
}

// isNornicDBStoreClosingCommitFailure recognizes the commit-side twin of
// isNornicDBRestartTransactionStartFailure: a backend restart that reaches the
// store between the transaction body and its commit, so Badger refuses the
// commit instead of the WAL refusing the begin. A restart interrupts a
// canonical write at one of four points -- store healthy, store closing
// (here), WAL closed (the start failure above), process gone (a
// *neo4jdriver.ConnectivityError) -- and this one was the only point still
// classified as terminal, which dead-lettered a restart as failure_class
// projection_bug and stranded every intent gated on the phase it would have
// published.
//
// Retry here means DURABLE QUEUE replay only; this shape is deliberately kept
// out of classifyTransientNeo4jError, which replays a transaction body in
// place. A commit failure leaves an outcome this process cannot observe, and
// that file already excludes the driver's own CommitFailedDeadError for
// exactly that reason. Queue replay needs no such observation: it re-runs the
// whole handler, and the canonical writers it drives are MERGE-shaped upserts
// that converge on the same node or edge however many times they run. Measured
// against the pinned image, a restart under a long grouped transaction rolls
// every executed statement back rather than tearing, so a replay finds nothing
// half-applied to begin with; and the relationship handlers keep a second
// guarantee anyway -- shouldSkipRetract stops skipping the prior-generation
// retract once AttemptCount exceeds 1, which would sweep a partial from any
// other source before the replay rewrites the scope.
//
// Both the code and the message are required, matching the start-failure guard
// beside it: NornicDB reports this backend-unavailable condition under a
// ClientError prefix that a real constraint violation also uses, so the code
// alone would swallow genuine terminal commit failures.
//
// Like the start-side guard, this accepts more than one body for the one
// condition, because the store reports commit-side teardown differently
// depending on how far into shutdown it is. There are THREE:
//
//	nornicDBStoreClosingCommitMsg      Badger refusing a commit while the
//	                                   store is still CLOSING
//	nornicDBStoreClosedCommitMsg       the store already CLOSED under the open
//	                                   transaction, failing the materialize call
//	nornicDBStoreClosedCommitAllocMsg  the same closed store, failing the
//	                                   version-allocation call just before it
//
// so the full second shape reads:
//
//	Neo.ClientError.Transaction.TransactionCommitFailed
//	commit failed: materializing mvcc commit state: DB Closed
//
// That shape dead-lettered gcp_resource_materialization as
// failure_class=projection_bug in eshu-hq/eshu run 32665272053 and blew the
// restart cell's 4-minute drain budget. It is the cross-product of the two
// guards this file added for #6142 -- the commit code from this one, the
// "DB Closed" body from isNornicDBStoreClosedStatementFailure -- and because
// each guard requires its OWN pairing, the cross matched neither and fell
// through to terminal. Refs #6162.
//
// This guard does NOT match the bare "DB Closed" tail. The two closed-store
// bodies carry their operation prefix, and that is what keeps the widening
// safe: a genuine constraint violation is reported under this same commit code
// with the conflicting identity inlined, identities are evidence-derived, and
// one carrying the tail is plausible where one carrying the whole prefix is
// not. The statement-side guard can match the bare constant because its code
// is Statement.SyntaxError, which a schema conflict never carries.
func isNornicDBStoreClosingCommitFailure(err error) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	return errors.As(err, &neo4jErr) &&
		neo4jErr.Code == nornicDBTransactionCommitFailedCode &&
		(strings.Contains(neo4jErr.Msg, nornicDBStoreClosingCommitMsg) ||
			strings.Contains(neo4jErr.Msg, nornicDBStoreClosedCommitMsg) ||
			strings.Contains(neo4jErr.Msg, nornicDBStoreClosedCommitAllocMsg))
}

// isNornicDBStoreClosedStatementFailure recognizes a statement that failed
// because the store was already closed under it. NornicDB's UNWIND MERGE chain
// fast path reports this as nornicDBStatementSyntaxErrorCode, so the code alone
// cannot be trusted -- a genuinely malformed query carries the same one. The
// "DB Closed" body is what makes it unambiguous.
//
// It needs no statement-shape guard, but NOT because nothing was written -- an
// earlier version of this comment claimed that and it is false. This error was
// raised live from a WRITE transaction that had already executed 4,597 of
// 20,000 statements. Replay is safe on the measured outcome instead: that same
// probe found the interrupted transaction rolled back whole (survived=0 of
// 20,000), so there is nothing half-applied for a replay to double up. See
// evidence-6142-backend-restart-transient-classification.md.
func isNornicDBStoreClosedStatementFailure(err error) bool {
	var neo4jErr *neo4jdriver.Neo4jError
	return errors.As(err, &neo4jErr) &&
		neo4jErr.Code == nornicDBStatementSyntaxErrorCode &&
		strings.Contains(neo4jErr.Msg, nornicDBStoreClosedMsg)
}
