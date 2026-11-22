-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq
--
-- #10881 build: the divided table shell (partitions and key are added by the script).
CREATE SCHEMA IF NOT EXISTS d;
CREATE TABLE d.fact_records (LIKE public.fact_records INCLUDING DEFAULTS INCLUDING STORAGE INCLUDING CONSTRAINTS) PARTITION BY HASH (scope_id);
