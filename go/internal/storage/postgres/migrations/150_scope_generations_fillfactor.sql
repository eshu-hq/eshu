-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7389: leave 10% free space on new scope_generations heap pages, so the
-- projector's write-start marker (migration 149) updates a freshly committed
-- generation row in place as a HOT update instead of moving it to another page
-- and touching every index. Measured on a fresh page set with no index on the
-- column: 300 of 300 marker updates were HOT, buffers median 7 (29 without the
-- free space, 0 of 300 HOT). Evidence: docs/internal/evidence/7389-superseded-writer-overlay.md.
--
-- SET (fillfactor) takes SHARE UPDATE EXCLUSIVE and changes only the catalog:
-- existing pages are not rewritten, and only pages filled after it keep the
-- free space, which is where new generation rows, and so marker updates, land.
-- The heap grows by at most about 10% as old pages turn over. Keep this the
-- only statement in the file.
ALTER TABLE scope_generations SET (fillfactor = 90);
