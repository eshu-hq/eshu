-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- In-flight drain check for the claim-plane cutover handover (#9376).
-- Run against the CORE database while ClaimNext is frozen. Both counts must
-- be zero before the snapshot step: an unexpired live claim, or a claimed
-- item pointing at one, means a pin could still land after the snapshot and
-- invalidate it (any old-authority issue after the snapshot aborts
-- activation). Read-only.
SELECT count(*)
  FROM workflow_claims AS c
 WHERE c.status = 'active'
   AND c.lease_expires_at > now();

SELECT count(*)
  FROM workflow_work_items AS i
  JOIN workflow_claims AS c
    ON c.claim_id = i.current_claim_id
 WHERE i.status = 'claimed'
   AND c.status = 'active'
   AND c.lease_expires_at > now();
