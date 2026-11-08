-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #10389 follow-up: pin the reviewed pass-state contract as object comments.
-- The 284 header bullet overpromised cross-generation fencing and blanket
-- complete-pass clearing; 284 is shipped, so this migration carries the
-- narrowed wording instead of editing it. The fencing-token guard orders
-- passes by token; a stale-generation pass admitted later draws a higher
-- token and can still overwrite current state (#10422). Only a full
-- complete pass clears stuck state; a scoped complete leaves it untouched
-- because it re-derives only its stamped CVE subset (#9779).
COMMENT ON TABLE supply_chain_impact_scope_pass_state IS
'Per-scope reducer pass state (#10389): a partial pass marks the scope stuck; a full complete pass clears it; a scoped complete leaves it untouched. The fencing-token guard orders passes by token and does not fence across generations (#10422).';
COMMENT ON COLUMN supply_chain_impact_scope_pass_state.partial_since IS
'When the scope first became continuously partial; preserved across further partial passes, cleared by a full complete pass.';
