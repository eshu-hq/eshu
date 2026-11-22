#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
#
# #10881 removal cell: one arm on one rehearsal database, decided on WAL bytes
# and page counts, not seconds.
# Usage: LIST=removal_list.txt bash removal_bench.sh DB ARM
#   ARM=drain_asis    shipped retention drain (fact_id-only delete), committed pages
#   ARM=drain_scoped  divided-table form (scope_id AND fact_id = ANY(...))
# LIST holds the generation ids to remove, one per line (pick_removal_list.sh),
# so every cell removes the same rows. SCHEMA=public|d picks the table when one
# database holds both shapes. Steps: VACUUM (FREEZE) then CHECKPOINT (so the arm
# does not pay a first-vacuum freeze of a freshly loaded table), then in ONE
# session: delete, then VACUUM (VERBOSE). Per-backend WAL comes from
# pg_stat_get_backend_wal, so other cluster activity does not pollute it. Rows
# deleted are summed from bench_drain_log. Relation size is the sum over
# pg_partition_tree leaves (pg_total_relation_size of a partitioned parent is 0).
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "$here/common.sh"
db="$1"
arm="$2"
list="${LIST:?set LIST to the removal list file}"
page="${PAGE_ROWS:-100000}"
export LP_PGOPTIONS="-c search_path=${SCHEMA:-public},public"

case "$arm" in
  drain_asis) scoped=false ;;
  drain_scoped) scoped=true ;;
  *) echo "unknown arm $arm" >&2; exit 2 ;;
esac
arr="ARRAY[$(awk '{printf "%s'"'"'%s'"'"'", (NR>1?",":""), $0}' "$list")]::text[]"

lp "$db" -q -f - <"$here/removal_procs.sql"
lp "$db" -q -c "TRUNCATE bench_drain_log"
lp "$db" -q -c "VACUUM (FREEZE) fact_records"
lp "$db" -q -c "CHECKPOINT"
printf '# cell db=%s arm=%s list=%s generations=%s loadavg=%s io_psi=%s\n' "$db" "$arm" "$list" "$(wc -l <"$list")" \
  "$(cut -d' ' -f1-3 /proc/loadavg)" "$(awk '/some/{print $2,$3}' /proc/pressure/io)"
vlog="${WORKDIR:-/tmp}/vacuum_${db}_${arm}.log"
lp "$db" -q 2>"$vlog" -v arr="$arr" -v page="$page" -v scoped="$scoped" -v db="$db" -v arm="$arm" -f - <"$here/removal_cell.sql"
echo "vacuum pages: $(python3 -I "$here/parse_vacuum.py" "$vlog")"
