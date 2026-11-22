#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# Per-scope fact_records row counts from the read replica (read-only).
# Writes scope_counts.csv (scope_id,scope_kind,rows) into $WORKDIR.
# Usage: REPLICA_DSN=... bash census_replica.sh
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
mkdir -p "$WORKDIR"
export RQ_TIMEOUT=900000
rq -t -F, -o "$WORKDIR/scope_counts.csv" -c "
select s.scope_id, s.scope_kind,
       (select count(*) from fact_records f where f.scope_id = s.scope_id)
from ingestion_scopes s"
echo "rows: $(awk -F, '{n+=$3} END{print n}' "$WORKDIR/scope_counts.csv") scopes: $(wc -l <"$WORKDIR/scope_counts.csv")"
