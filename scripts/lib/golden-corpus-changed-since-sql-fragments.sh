#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# GENERATED FILE - DO NOT EDIT. Regenerate with:
#   ESHU_UPDATE_CHANGED_SINCE_ORACLE_FRAGMENTS=1 go test ./internal/storage/postgres -run ChangedSinceOracleFragments
# The SQL fragments the golden-corpus changed-since oracle shares with the
# get_changed_since statement (go/internal/storage/postgres/changed_since_sql.go).
# shellcheck disable=SC2034
golden_changed_since_digest_input="CASE WHEN fact_kind = 'content_entity' AND jsonb_typeof(payload) = 'object' THEN payload - 'indexed_at' ELSE payload END"
golden_changed_since_exclude_reducer_kinds="fact_kind NOT LIKE 'reducer\\_%'"
