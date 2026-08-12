#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_GO_LOG:?}"
# go_test_run_guard (#6055) runs `go test -list <pattern> ...` before the
# real `go test -run` to assert a minimum matched-test count. This fake `go`
# binary does not run real tests, so a real -list call would print nothing
# and starve every go_test_run_guard call in verify-ask-eshu-local-proof.sh
# below its minimum, failing the --deepseek control-flow proof this fake
# binary exists to isolate. Report a generous, fixed match count instead —
# comfortably above every real min_matches used in that script today — so
# this stub stays decoupled from any one call site's specific number.
if [[ "$*" == *"-list"* ]]; then
	for i in $(seq 1 50); do
		printf 'TestFakeMatched%d\n' "$i"
	done
	exit 0
fi
if [[ "$*" == *"answer-quality-scorecard"* && "${FAKE_SCORECARD_FAIL:-}" == "true" ]]; then
	exit 17
fi
exit 0
