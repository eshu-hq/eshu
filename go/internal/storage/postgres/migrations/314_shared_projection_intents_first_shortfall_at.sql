-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Durable first-shortfall anchor for the code-call CALLS shortfall park
-- (#10947). The #10938 close parks a batch retryably when an enforced CALLS
-- write shorts its MATCH, with no elapsed bound: a deleted endpoint in a
-- never-superseded generation parks one partition forever. The runner
-- stamps first_code_call_shortfall_at COALESCE-once on every parked batch
-- and dead-letters the batch once the oldest stamp reaches the 30-minute
-- elapsed bound, so NULL means "never parked" and always defers.
ALTER TABLE shared_projection_intents
    ADD COLUMN IF NOT EXISTS first_code_call_shortfall_at TIMESTAMPTZ NULL;
