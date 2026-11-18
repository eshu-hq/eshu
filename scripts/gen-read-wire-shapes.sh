#!/usr/bin/env bash
# Regenerate the step-2 upstream wire-shape snapshots from the oracle pin.
#
# Reads the frozen FORWARDED-READS rows in contracts/step2-frozen-scope.md
# and dumps each row's oracle fragment verbatim into
# contracts/upstream/<family>-read-wire-shapes.txt (family lowercased).
# Outputs are snapshots: never hand-edit them; rerun this script on a
# deliberate fragment change. Deterministic: no timestamps, table order.
#
# Usage (worktree root): bash scripts/gen-read-wire-shapes.sh
set -euo pipefail

cd "$(dirname "$0")/.."

ORACLE=8a78eb3f48
SCOPE=contracts/step2-frozen-scope.md
OUT=contracts/upstream

rows=$(rg -c '^\| R[0-9]+ \|' "$SCOPE")
if [ "$rows" -eq 0 ]; then
	echo "gen-read-wire-shapes: no frozen rows in $SCOPE" >&2
	exit 1
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# family<TAB>fragment<TAB>row, one line per frozen row, in table order.
rg '^\| R[0-9]+ \|' "$SCOPE" | awk -F'|' '{
	row=$2; gsub(/[^R0-9]/, "", row);
	fam=$5; gsub(/^[ \t]+|[ \t]+$/, "", fam);
	frag=$6; gsub(/^[ \t`]+|[ \t`]+$/, "", frag);
	if (fam == "" || frag == "") { print "EMPTY" > "/dev/stderr"; exit 1 }
	print tolower(fam) "\t" frag "\t" row
}' >"$tmp/rows.tsv"

count=$(wc -l <"$tmp/rows.tsv")
if [ "$count" != "$rows" ]; then
	echo "gen-read-wire-shapes: parsed $count rows, want $rows" >&2
	exit 1
fi

while IFS=$'\t' read -r fam frag row; do
	if ! git cat-file -e "$ORACLE:$frag" 2>/dev/null; then
		echo "gen-read-wire-shapes: missing $frag at $ORACLE (row $row)" >&2
		exit 1
	fi
	echo -e "$fam\t$frag\t$row" >>"$tmp/ok.tsv"
done <"$tmp/rows.tsv"

mkdir -p "$OUT"
rm -f "$OUT"/*-read-wire-shapes.txt

while IFS=$'\t' read -r fam frag row; do
	dest="$OUT/$fam-read-wire-shapes.txt"
	if [ ! -f "$dest" ]; then
		printf '# GENERATED - never hand-edit. Regenerate: bash scripts/gen-read-wire-shapes.sh\n' >"$dest"
	fi
	{
		printf '# Source: %s at core oracle %s, pinned by contracts/step2-frozen-scope.md row %s.\n' "$frag" "$ORACLE" "$row"
		printf '# Byte-verbatim port of the oracle fragment; a deliberate upstream change\n'
		printf '# regenerates this file, never an edit.\n\n'
		git show "$ORACLE:$frag"
	} >>"$dest"
done <"$tmp/ok.tsv"

echo "gen-read-wire-shapes: $count rows -> $OUT"
