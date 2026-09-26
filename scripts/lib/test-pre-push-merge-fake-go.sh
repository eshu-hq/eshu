#!/usr/bin/env bash
# Fake `go` for scripts/lib/test-pre-push-merge-cases.sh (#7111 F5). It models
# one compile rule: a call to p.Old() needs a `func Old(` somewhere in the tree
# it runs in, so a head that builds alone but not merged with main fails where
# a real compiler would, in the merged tree. Copied to <fixture>/bin/go.
printf 'go %s cwd=%s\n' "$*" "${PWD}" >> "${DRIVER_ARGS_LOG}"
case "${1:-}" in
	build | vet) ;;
	*) exit 0 ;;
esac
# Case K: move the fixture's HEAD while the merged tree is being vetted, the
# way a concurrent commit or amend would.
if [[ -n "${FAKE_GO_MOVE_HEAD_REPO:-}" && "${PWD}" == *eshu-pre-push-merge* ]]; then
	git -C "${FAKE_GO_MOVE_HEAD_REPO}" -c core.hooksPath=/dev/null -c user.name=Test \
		-c user.email=test@example.invalid commit -q --allow-empty -m "moved mid-run"
fi
if rg -q --glob '*.go' 'p\.Old\(' . && ! rg -q --glob '*.go' '^func Old\(' .; then
	printf 'internal/r/call.go:5:12: undefined: p.Old\n' >&2
	exit 1
fi
exit 0
