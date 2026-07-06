#!/bin/sh
# One-shot demo-corpus staging step for docker-compose.demo.yaml.
#
# COPIES (not symlinks) each manifest-declared repo fixture from the read-only
# /src-fixtures mount into /data/corpus (ESHU_FILESYSTEM_ROOT — the source
# corpus). bootstrap-index then syncs from there into ESHU_REPOS_DIR
# (/data/repos, the managed working checkout), so the two MUST be different
# dirs. The filesystem discovery walker treats each immediate child of
# ESHU_FILESYSTEM_ROOT as a repo and does not follow symlinks (see
# scripts/verify-golden-corpus-gate.sh), so a symlinked fixture collapses to a
# single scope and breaks cross-repo edges such as rc-3 (Repository
# DEPENDS_ON Repository).
set -eu

src_root=/src-fixtures
dest_root=/data/corpus

[ -d "$src_root" ] || {
	echo "demo corpus staging failed: source fixtures root $src_root is missing" >&2
	exit 1
}

mkdir -p "$dest_root"

: "${ESHU_DEMO_CORPUS_REPOS:?set ESHU_DEMO_CORPUS_REPOS (space-separated fixture directory names)}"

staged_count=0
for fixture in $ESHU_DEMO_CORPUS_REPOS; do
	src="$src_root/$fixture"
	dest="$dest_root/$fixture"
	[ -d "$src" ] || {
		echo "demo corpus staging failed: fixture not found: $src" >&2
		exit 1
	}
	rm -rf "$dest"
	cp -R "$src" "$dest"
	staged_count=$((staged_count + 1))
	echo "staged: $fixture"
done

chown -R 10001:10001 "$dest_root" 2>/dev/null || true

echo "demo corpus staging passed: staged_count=$staged_count"
