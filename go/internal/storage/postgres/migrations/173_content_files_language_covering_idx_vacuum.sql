-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7728, step 3 of 3: make the covering index from 172 pay off immediately.
-- Measured on the production-shaped scratch: the covering index at 0%
-- all-visible still reads 27,000 heap blocks for 27,000 rows, and after
-- VACUUM the same read is an Index Only Scan with Heap Fetches 0. A single
-- full-table rewrite clears ~50% of all-visible pages, so without this
-- vacuum the deploy would keep the slow plan until autovacuum catches up.
-- Steady state afterwards relies on default autovacuum (its dead-tuple
-- threshold refires after every large sync); no storage-parameter change,
-- since tuning autovacuum knobs on the write-hot content_files table is a
-- separate, unmeasured theory.
-- Sole statement on purpose: VACUUM cannot run inside a transaction block,
-- and the runner executes a multi-statement file in one implicit
-- transaction. VACUUM takes no write-blocking lock; reads and content
-- writes proceed.
VACUUM (ANALYZE) content_files;
