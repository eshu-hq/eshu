#!/usr/bin/env bash
# Fake `psql` for test-claim-plane-cutover-handover.sh. Dispatches on the -f
# file: drain counts, snapshot rows, or a satellite session. The session
# branch asserts the committed seed wiring (temp table DDL plus the GREATEST
# logic from the committed seed.sql) and emits canned reports; the SQL
# semantics live in test-claim-plane-cutover-handover-sql.sh instead.
set -euo pipefail

file=""
prev=""
for arg in "$@"; do
	[[ "${prev}" == "-f" ]] && file="${arg}"
	prev="${arg}"
done
case "${file}" in
*claim-plane-handover-drain.sql)
	printf '%s\n%s\n' "${STUB_DRAIN_1:-0}" "${STUB_DRAIN_2:-0}"
	;;
*claim-plane-handover-snapshot-items.sql)
	if [[ "${STUB_ITEMS_EMPTY:-0}" == "1" ]]; then printf ''
	else printf 'item-a\t3\tclaim-a\tclaimed\nitem-b\t7\t\tpending\n'
	fi
	;;
*claim-plane-handover-snapshot-fences.sql)
	printf 'gen-1\titem-a\t3\n'
	;;
*)
	grep -q "CREATE TEMP TABLE handover_snapshot" "${file}" ||
		{ echo "stub psql: session lacks handover_snapshot DDL" >&2; exit 1; }
	grep -q "GREATEST(item.current_fencing_token, snap.h)" "${file}" ||
		{ echo "stub psql: session lacks committed seed logic" >&2; exit 1; }
	grep -q "seed.tsv" "${file}" ||
		{ echo "stub psql: session must COPY the 2-column seed.tsv, never items.tsv" >&2; exit 1; }
	printf '%s\n' "${STUB_MATCHED:-2}"
	printf '%b' "${STUB_MISSING:-}"
	;;
esac
