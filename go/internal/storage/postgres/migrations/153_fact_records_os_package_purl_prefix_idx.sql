-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7154: the supply-chain impact handler reads the installed OS packages of its
-- intent's affected packages, keyed by package id. The installed package id is
-- the purl with surrounding whitespace trimmed, cut at its first '@'
-- (core.packageIDFromPURL). Without an index the read scanned every active
-- installed package of the ecosystem for every intent: 60,164 buffers per page
-- at 50,000 rows. With this index and the materialized candidates query in
-- go/internal/storage/postgres/installed_advisory_targets.go it reads 271
-- buffers for 2 keys and 2,746 for 100 keys.
--
-- The expression text must stay identical to osPackagePURLPrefixExpression in
-- that file (TestOSPackagePURLPrefixExpressionMatchesMigration pins it): an
-- index on any other spelling is not used. The btrim character set is exactly
-- the 25 runes Go's strings.TrimSpace strips.
--
-- Build concurrently to keep fact writes available. Keep this the only
-- statement in the file: a concurrent index build cannot run inside a
-- transaction block, and the bootstrap sends each file as one string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_os_package_purl_prefix_idx
    ON fact_records ((split_part(btrim(payload->>'purl', E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'), '@', 1)), fact_id)
    WHERE fact_kind = 'vulnerability.os_package'
      AND is_tombstone = FALSE;
