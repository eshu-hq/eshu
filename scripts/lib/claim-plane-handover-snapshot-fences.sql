-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Token high-water snapshot for the claim-plane cutover handover (#9376):
-- committed fence maxima. Run against the CORE database after ClaimNext is
-- frozen. Deterministic LC_ALL=C order: the committed fencing token per
-- (generation, work item). Stored as fences.tsv beside items.tsv and hashed
-- with it; the fence rows prove no commit anchored a token the snapshot
-- missed. Read-only.
SELECT f.generation_id,
       f.work_item_id,
       f.fencing_token
  FROM generation_commit_fences AS f
 ORDER BY f.generation_id, f.work_item_id;
