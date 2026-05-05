# Codebase Concerns

**Analysis Date:** 2026-04-13

## Tech Debt

### File Size Violations (Approaching/Exceeding 500-Line Limit)

**Coordinator Pipeline Complexity:**
- Issue: `src/eshu/indexing/coordinator_pipeline.py` (1,151 lines) is heavily overloaded with async coordination, state management, and parse recovery logic
- Files: `src/eshu/indexing/coordinator_pipeline.py`
- Impact: Difficult to reason about repository parse lifecycle, hard to test individual phases, increased cognitive load for modifications
- Fix approach: Split into specialized modules: parse coordination, recovery handling, state checkpointing, and telemetry emission. Each module should handle one phase cleanly.

**Status Store Database Complexity:**
- Issue: `src/eshu/runtime/status_store_db.py` (867 lines) contains monolithic SQL operations for runtime state persistence
- Files: `src/eshu/runtime/status_store_db.py`
- Impact: Schema changes require careful coordination, state transitions are opaque, testing requires full database setup
- Fix approach: Extract query builders into domain-specific modules, create schema migration helpers, separate read/write concerns

**Coordinator Storage Logic:**
- Issue: `src/eshu/indexing/coordinator_storage.py` (589 lines) mixes snapshot persistence, checkpoint recovery, and state reconstruction
- Files: `src/eshu/indexing/coordinator_storage.py`
- Impact: State reconstruction after failure is fragile, checkpoint format is implicit, recovery paths not well tested
- Fix approach: Extract checkpoint format to explicit data class, create recovery validators, split persistent store concerns from in-memory coordination

**Graph Persistence Unwind Operations:**
- Issue: Multiple graph persistence files at 500-line limit: `src/eshu/graph/persistence/unwind.py` (485 lines), `src/eshu/relationships/postgres.py` (499 lines), `src/eshu/facts/work_queue/postgres.py` (500 lines)
- Files: `src/eshu/graph/persistence/unwind.py`, `src/eshu/relationships/postgres.py`, `src/eshu/facts/work_queue/postgres.py`
- Impact: Batch write logic is repetitive across modules, schema changes require changes in multiple places, testing coverage gaps around edge cases
- Fix approach: Extract shared batch write patterns into reusable builders, create schema-driven factories for common operations

---

## Known Bugs & Reliability Issues

### End-to-End Sync Never Completes

**Ingestion Pipeline Failure:**
- Symptoms: No complete end-to-end sync has ever finished; indexing stalls or fails partway through
- Files: `src/eshu/indexing/coordinator_pipeline.py`, `src/eshu/indexing/coordinator_facts.py`, `src/eshu/runtime/ingester/sync.py`
- Root causes (compound):
  1. **Excessive Variable Entity Emission**: Facts-first emitter creates too many Variable entities, overwhelming Neo4j transaction batch sizes
  2. **Serialized Commit Bottleneck**: Repository snapshots are committed one at a time in 5-file chunks, creating O(n) transaction overhead
  3. **Global O(n²) Function Call Resolution**: Cross-file call linking uses unoptimized quadratic algorithm in `src/eshu/relationships/execution.py`
  4. **GIL-Limited Parsing**: Threaded parse executor bottlenecked by Python GIL on multi-file parsing
- Workaround: None — pipeline stalls completely
- Fix approach (Phase 1): 
  - Enable INDEX_VARIABLES wiring to filter unnecessary entities
  - Increase batch size from 5 to 50-100 files per transaction
  - Remove global fallback call resolution path
  - Switch multiprocess parser to default in test environments
- See also: `~/PRD-eshu-ingestion-remediation.md` and `~/eshu-ingestion-analysis-findings.md`

### Multiprocess Parser Pool Degradation

**Process Pool Failure Recovery:**
- Symptoms: Parse executor breaks after N repositories; falls back to slower threaded mode without recovery
- Files: `src/eshu/indexing/parse_recovery.py`, `src/eshu/collectors/git/parse_execution.py`
- Cause: `BrokenProcessPool` from underlying `ProcessPoolExecutor` cascades; no restart mechanism in place
- Impact: Once pool is broken, all subsequent repositories parse at 1/8 speed (threaded vs multiprocess), causing timeouts in large batch operations
- Current code path: `parse_repository_snapshot_with_recovery()` catches `BrokenProcessPool` and downshifts to threaded, but does not attempt pool recreation
- Fix approach: Create pool health checker; implement pool restart logic with exponential backoff; emit clear telemetry when pool state changes

### Bare Exception Handlers & Silent Failures

**Overly Broad Exception Handlers:**
- Issue: Multiple modules catch `Exception` or `BaseException` without specific handling
- Files (sample):
  - `src/eshu/relationships/file_evidence_support.py` (lines 189, 470)
  - `src/eshu/collectors/git/parse_execution.py` (line 379)
  - `src/eshu/resolution/projection/common.py` (line 47)
  - `src/eshu/runtime/status_store_runtime.py` (line 244)
  - `src/eshu/core/database_falkordb.py` (lines 65, 233)
- Impact: Failures are silently swallowed; no signal to caller that operation failed; makes debugging cascading failures extremely difficult
- Fix approach: Replace with specific exception types, log before catching, re-raise or propagate explicitly

### Global Singleton State Management

**Thread-Safe But Rigid State Holders:**
- Issue: Multiple modules use module-level globals with threading locks for singleton instances
- Files: 
  - `src/eshu/facts/state.py` (lines 29-33, 49-100)
  - `src/eshu/relationships/state.py` (lines 12-13, 36-45)
  - `src/eshu/runtime/status_store_runtime.py` (lines 75, 237-239)
  - `src/eshu/observability/state.py` (lines 87-227)
- Impact: Test isolation requires explicit reset of all globals; cross-module state dependencies are hidden; makes concurrent test runs fragile
- Current mitigation: Test reset functions exist (`reset_facts_runtime_for_tests()`, `reset_relationship_store_for_tests()`) but are not consistently called
- Fix approach: Move to dependency injection or context managers; create test fixtures that auto-cleanup; document state lifecycle clearly

---

## Security Considerations

### Broad Exception Handlers & Error Information Leakage

**Error Messages Expose Internal State:**
- Risk: Exception messages from graph queries or parse failures may contain file paths, SQL details, or other sensitive information
- Files: `src/eshu/mcp/transport.py`, `src/eshu/viz/server.py`
- Current mitigation: Generic HTTP 500 responses are returned to clients (e.g., `HTTPException(status_code=500)`)
- Recommendations: 
  - Audit all exception messages for sensitive data
  - Use sanitized error codes instead of raw exception strings in public APIs
  - Log full details server-side only; expose generic messages to clients

### Environment Variable Surface

**Unvalidated Configuration From Environment:**
- Risk: Multiple configuration options read directly from environment without validation
- Files: `src/eshu/cli/config_catalog.py`, `src/eshu/cli/config_manager.py`
- Current approach: Config values are cast to int/bool but not range-checked
- Examples:
  - `ESHU_REPO_FILE_PARSE_CONCURRENCY` clamped to [1, 64] (safe)
  - `ESHU_*_ENABLED` flags check for specific false values only (risky — typos enable features)
- Recommendations:
  - Create enum-based configuration for boolean flags
  - Validate numeric ranges on load, not on use
  - Reject unknown environment prefixes to catch misspellings

---

## Performance Bottlenecks

### Quadratic Function Call Resolution

**O(n²) Cross-File Linking:**
- Problem: Global call resolution without index walks all function pairs to find matches
- Files: `src/eshu/relationships/execution.py` (489 lines), `src/eshu/relationships/cross_repo_linker.py`
- Cause: Missing function signature index; queries rebuild on every repository processed
- Symptoms: Indexing time increases quadratically with repository size; large repos (10k+ files) stall during call linking phase
- Improvement path:
  1. Build function signature index before cross-file matching
  2. Use fuzzy matching with early termination thresholds
  3. Parallelize call linking by file groups (embarrassingly parallel)
  4. Consider incremental call resolution after each batch commit

### Serialized Neo4j Transaction Commits

**5-File Transaction Chunks:**
- Problem: Repository snapshots committed with 5-file-per-transaction limit
- Files: `src/eshu/graph/persistence/worker.py`, `src/eshu/tools/graph_builder_persistence.py`
- Cause: Legacy transaction size limit to prevent OOM; now overly conservative
- Symptoms: 1000-file repo requires 200 separate transactions; connection pool saturation
- Improvement path:
  - Increase file limit to 50-100 per transaction (benchmark first)
  - Implement adaptive batch sizing based on available memory
  - Use bulk unwind operations for entity/relationship creation
  - Profile Neo4j memory usage under increased batch sizes

### Threaded Parsing Limited by GIL

**Python GIL Blocks Multiprocess Default:**
- Problem: File parsing is I/O-bound but threaded due to GIL contention on CPU-intensive parsing (AST walks, SCIP generation)
- Files: `src/eshu/collectors/git/parse_execution.py`, `src/eshu/collectors/git/parse_worker.py`
- Current approach: Multiprocess parsing is opt-in (`ESHU_REPO_FILE_PARSE_MULTIPROCESS=true`) and experimental
- Symptoms: Large repositories parse at 1/4-1/8 the speed of multiprocess mode; ingestion times exceed SLA in production
- Improvement path:
  1. Make multiprocess parsing the default in production
  2. Implement robust process pool restart on failures
  3. Add CPU count auto-detection for worker count
  4. Profile and optimize parse_worker message passing overhead

### Query Fallback Paths Hide Missing Edges

**Content Search as Query-Time Workaround:**
- Problem: When graph edges are incomplete (finalization issues), queries fall back to content search
- Files: `src/eshu/query/story_documentation.py` (lines 285-286), `src/eshu/query/status.py` (lines 217, 285), `src/eshu/query/infra.py` (line 161)
- Issue: Fallbacks mask upstream finalization failures; gives illusion of completeness while hiding data loss
- Examples:
  - `_checkpoint_status_fallback()` falls back to filesystem when database checkpoint missing
  - `_runtime_run_summary_fallback()` uses legacy checkpoint paths when live status missing
  - `content_search` evidence added when graph evidence incomplete
- User feedback: These fallbacks are NOT acceptable; finalization must be reliable and complete
- Fix approach: Remove all fallback paths; instead fix finalization reliability and add strict validation that all expected edges were written

---

## Fragile Areas

### Coordinator State Reconstruction After Restart

**Implicit Checkpoint Format:**
- Files: `src/eshu/indexing/coordinator_storage.py`, `src/eshu/runtime/status_store_db.py`
- Why fragile: Repository checkpoint structure is inferred from database schema; state transitions (pending → in-progress → completed) are implicit in code flow
- Safe modification:
  1. Read the full checkpoint schema from schema migrations
  2. Add validation on checkpoint load: verify required fields, check state transitions
  3. Test restart path by killing indexing mid-run and resuming
  4. Document expected checkpoint lifecycle explicitly
- Test coverage gaps: No integration tests for multi-restart scenarios; single-restart tested minimally

### Batch Sizing & Anomaly Detection

**Adaptive Batch Configuration Not Well Tested:**
- Files: `src/eshu/indexing/adaptive_batch_config.py`, `src/eshu/indexing/anomaly_detection.py`
- Why fragile: Batch size adjusted based on repo classification and anomaly thresholds; thresholds are empirically derived and not validated
- Safe modification:
  1. Document how thresholds were derived (analysis repo size, complexity metrics)
  2. Add threshold validation: reject config outside historical bounds
  3. Emit telemetry on batch size decisions
  4. Test with synthetic repos at boundary conditions
- Test coverage gaps: No synthetic repo generators; threshold tests use real repos

### Parse Recovery Executor State

**ProcessPoolExecutor Lifecycle Not Fully Understood:**
- Files: `src/eshu/indexing/parse_recovery.py`, `src/eshu/collectors/git/parse_execution.py`
- Why fragile: Pool state (alive, broken, shutdown) transitions are implicit; recovery logic assumes pool can transition from broken to threaded cleanly
- Safe modification:
  1. Create explicit pool state machine (Enum: alive, degraded, broken, shutdown)
  2. Add pool health checks before task submission
  3. Emit telemetry on state transitions
  4. Test pool lifecycle: healthy → broken → fallback → shutdown
- Test coverage gaps: Pool failure scenarios not systematically tested; only happy-path multiprocess tested

---

## Scaling Limits

### Neo4j Transaction Buffer Saturation

**Current Capacity:**
- Batch size: 5 files per transaction
- Typical file count: 100k+ for medium repos
- Result: 20k+ transactions per repo; connection pool exhaustion likely at 2-3 repos concurrently

**Limit:** Connection pool saturation around 5-10 concurrent repos

**Scaling path:**
  1. Increase batch size to 100 files (benchmark memory first)
  2. Implement adaptive batching (small files grouped, large files isolated)
  3. Implement transaction pooling with explicit release
  4. Monitor Neo4j transaction queue depth; backpressure on new repos if queue exceeds threshold

### Postgres Fact Work Queue Throughput

**Current Capacity:**
- Queue reads are serialized per resolution engine instance
- Single engine processes ~100 facts/second (typical)
- Fact count: O(files) — 100k files = ~100k facts minimum

**Limit:** Single resolution engine will take 1000+ seconds (16 min) to process single repo

**Scaling path:**
  1. Implement work queue sharding (partition by fact_id hash)
  2. Add multiple resolution engine replicas that consume from same queue
  3. Implement progressive delivery: emit facts in dependency order, resolve as they arrive
  4. Monitor fact queue depth; scale engines based on queue length

### Multiprocess Parser Worker Pool

**Current Capacity:**
- Worker count: auto-detected as CPU count (typical: 8-16)
- Per-worker throughput: ~100 files/second
- Maximum concurrent parse: 8-16 repos × 100 files/sec = 800-1600 files/sec

**Limit:** Parsing will block if 2+ large repos queued simultaneously

**Scaling path:**
  1. Implement per-repository parse queue with priority
  2. Distribute across multiple indexer instances (each with own parser pool)
  3. Implement parse task batching (multiple repos' files in one batch)
  4. Consider dedicated parse service separate from indexer

---

## Dependencies at Risk

### ProcessPoolExecutor Reliability

**Risk:** `concurrent.futures.ProcessPoolExecutor` raises `BrokenProcessPool` unpredictably; no recovery mechanism in stdlib

**Impact:** 
- Entire indexing run degrades to threaded parsing on first pool failure
- No way to create new pool and restart parsing
- Production runs become 8x slower after first process crash

**Migration plan:**
  1. Evaluate `multiprocessing.Pool` (manual lifecycle management)
  2. Evaluate `concurrent.futures.ProcessPoolExecutor` with custom wrapper (restart on broken)
  3. Consider `loky` library for more robust worker management
  4. Implement health checks and graceful degradation

### Neo4j Driver Connection Handling

**Risk:** Neo4j Python driver can exhaust connection pools silently; no automatic recovery

**Impact:**
- Long-running indexing runs hit connection exhaustion after N transactions
- Queries hang indefinitely waiting for connection
- No clear error signal to operator

**Current mitigation:** Manual connection closure in most query contexts

**Recommendations:**
  1. Implement connection pool health checks between batches
  2. Set explicit connection timeouts (currently may use defaults)
  3. Add telemetry for connection pool utilization
  4. Document Neo4j driver configuration best practices

---

## Missing Critical Features

### Work Queue Backpressure & Flow Control

**Problem:** Ingester can enqueue facts faster than resolution engine can process

**Blocks:** Scaling to large repositories; preventing OOM when queue grows unbounded

**Symptom:** Postgres fact queue grows to millions of rows; memory pressure mounts

**Fix approach:**
  1. Implement bounded work queue with max size
  2. Add backpressure signal from engine to ingester
  3. Ingest facts in progressive batches (emit facts for 100 files, resolve them, then emit next 100)
  4. Monitor queue depth; emit alerts when backpressure active

### Finalization Reliability Observability

**Problem:** When finalization fails (incomplete edges, missing entities), no clear diagnostic signal

**Blocks:** Debugging why syncs stall; identifying root cause in compound failure scenarios

**Current approach:** Logs buried in telemetry; no explicit finalization completion signal

**Fix approach:**
  1. Add finalization validation phase: scan graph to verify expected entities/edges
  2. Emit explicit "finalization complete" or "finalization incomplete" metric
  3. Create pre/post finalization graph snapshots for comparison
  4. Add query that counts expected vs actual entities by type

### Checkpoint Recovery Validation

**Problem:** Recovery path assumes checkpoints are valid; corrupted checkpoints cause confusing failures

**Blocks:** Safely recovering from storage corruption; clear error messaging on checkpoint issues

**Current approach:** Checkpoint format implicit; no schema validation on load

**Fix approach:**
  1. Define checkpoint schema explicitly
  2. Validate on load: check required fields, verify state transitions
  3. Emit diagnostic on validation failure: what field is missing, what state transition is invalid
  4. Allow explicit checkpoint reset/purge command if recovery fails

---

## Test Coverage Gaps

### End-to-End Ingestion Pipeline

**What's not tested:** Complete sync from clone through facts emission through resolution

**Files:** 
  - `tests/integration/indexing/test_git_facts_end_to_end.py` (exists but incomplete)
  - `tests/integration/indexing/test_git_facts_projection_parity.py` (exists but incomplete)

**Risk:** Failures only surface in production; integration test suite gives false confidence

**Priority:** HIGH

**How to test:**
  1. Use real source repos as fixtures (not synthetic; user feedback: real repos matter)
  2. Ingest via docker-compose stack
  3. Query via MCP tools
  4. Verify expected edges, entities, and content
  5. Verify against ground truth (manual inspection of source)

### Parser Recovery & Process Pool Failure

**What's not tested:** Pool breakage and fallback to threaded mode

**Files:** `src/eshu/indexing/parse_recovery.py`, `src/eshu/collectors/git/parse_execution.py`

**Risk:** Process pool failures will happen in production; fallback path untested

**Priority:** MEDIUM-HIGH

**How to test:**
  1. Create test double of ProcessPoolExecutor that raises `BrokenProcessPool` on nth call
  2. Verify fallback to threaded parsing activates
  3. Verify parsing completes successfully with fallback
  4. Verify telemetry emitted on fallback

### Checkpoint Restart Scenarios

**What's not tested:** Multi-restart (kill and resume multiple times)

**Files:** `src/eshu/indexing/coordinator_storage.py`, `src/eshu/runtime/status_store_db.py`

**Risk:** Restarts work once but break on second restart; corruption accumulates invisibly

**Priority:** MEDIUM

**How to test:**
  1. Start indexing, kill after N repos
  2. Resume from checkpoint
  3. Kill again after M more repos
  4. Resume again
  5. Verify final state matches non-interrupted run

### Query Fallback Path Removal

**What's not tested:** Query correctness when fallback paths removed

**Files:** `src/eshu/query/status.py`, `src/eshu/query/story_documentation.py`, `src/eshu/query/infra.py`

**Risk:** Removing fallbacks breaks queries if finalization is incomplete

**Priority:** MEDIUM

**How to test:**
  1. Verify finalization completes successfully (add validation)
  2. Remove all fallback paths
  3. Run integration queries
  4. Add regression test: if finalization incomplete, queries fail with clear error (not silent degradation)

---

*Concerns audit: 2026-04-13*
