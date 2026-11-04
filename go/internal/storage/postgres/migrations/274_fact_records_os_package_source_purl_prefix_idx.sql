-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #9859: the supply-chain-impact handler reads the installed OS packages of
-- its intent's affected packages keyed on the installed binary package id
-- (migration 153) and now also on the installed row's source package name
-- (this index). An advisory naming a source package (openssl) never meets
-- the binary prefix (pkg:deb/debian/libssl3) the 153 index serves, so the
-- narrowed read's OR branch needs its own expression index: the source key
-- is the version-stripped purl prefix with its last segment replaced by
-- source_package (core.installedOSPackageSourceKey), and rows whose source
-- is missing or held a rejected character key as ''.
--
-- The expression text must stay identical to
-- osPackageSourcePURLPrefixExpression in
-- go/internal/storage/postgres/installed_advisory_targets.go (with payload
-- unqualified, matching how the narrowed query calls it):
-- TestOSPackageSourcePURLPrefixExpressionMatchesMigration pins it. An index
-- on any other spelling is not used, and the narrowed read would silently
-- fall back to scanning every installed package.
--
-- Build concurrently to keep fact writes available. Keep this the only
-- statement in the file: a concurrent index build cannot run inside a
-- transaction block, and the bootstrap sends each file as one string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_os_package_source_purl_prefix_idx
    ON fact_records ((CASE WHEN btrim(payload->>'source_package', E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000') ~ '^[^/?#@[:space:]]+$' AND split_part(btrim(payload->>'purl', E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'), '@', 1) LIKE '%/%' THEN regexp_replace(split_part(btrim(payload->>'purl', E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000'), '@', 1), '/[^/]*$', '/' || btrim(payload->>'source_package', E' \t\n\u000b\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000')) ELSE '' END))
    WHERE fact_kind = 'vulnerability.os_package'
      AND is_tombstone = FALSE;
