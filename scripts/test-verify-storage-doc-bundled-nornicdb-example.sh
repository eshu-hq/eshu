#!/usr/bin/env bash
# Hermetic self-test for verify-storage-doc-bundled-nornicdb-example.sh.
# Red: the pre-#5103 example shape (neo4j.auth.secretName: "" with no
# neo4j.auth.password) must fail the verifier with a helm-template diagnostic.
# Green: the committed docs/public/deploy/kubernetes/storage.md must pass.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
verifier="${repo_root}/scripts/verify-storage-doc-bundled-nornicdb-example.sh"
bad_doc="${repo_root}/scripts/lib/test-verify-storage-doc-bundled-nornicdb-example-bad.md"

if ESHU_STORAGE_DOC_PATH="${bad_doc}" "${verifier}" >/tmp/eshu-storage-doc-bad.out 2>/tmp/eshu-storage-doc-bad.err; then
	echo "expected verifier to fail on the pre-fix (secretName empty, no password) example" >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-bad.out >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-bad.err >&2
	exit 1
fi

if ! rg -q "fails 'helm template'" /tmp/eshu-storage-doc-bad.err; then
	echo "expected a helm-template-failure diagnostic, got:" >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-bad.err >&2
	exit 1
fi

if ! rg -q 'neo4j.auth.password is required' /tmp/eshu-storage-doc-bad.err; then
	echo "expected the chart's own required-value error in the diagnostic, got:" >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-bad.err >&2
	exit 1
fi

if ! "${verifier}" >/tmp/eshu-storage-doc-real.out 2>/tmp/eshu-storage-doc-real.err; then
	echo "expected verifier to pass against the committed storage.md" >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-real.out >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-real.err >&2
	exit 1
fi

if ! rg -q 'verify-storage-doc-bundled-nornicdb-example: pass' /tmp/eshu-storage-doc-real.out; then
	echo "expected pass marker in verifier stdout, got:" >&2
	sed -n '1,120p' /tmp/eshu-storage-doc-real.out >&2
	exit 1
fi

echo "test-verify-storage-doc-bundled-nornicdb-example: pass"
