#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >>"${FAKE_CURL_LOG:?}"
if [[ "${FAKE_CURL_FAIL:-}" == "status" && "$*" == *"/api/v0/status/answer-narration"* ]]; then
	exit 22
fi
if [[ "${FAKE_CURL_FAIL:-}" == "json" && "$*" == *"/api/v0/ask"* && "$*" != *"text/event-stream"* ]]; then
	exit 22
fi
if [[ "${FAKE_CURL_FAIL:-}" == "sse" && "$*" == *"text/event-stream"* ]]; then
	exit 22
fi
if [[ "$*" == *"/api/v0/status/answer-narration"* ]]; then
	printf '{"provider_configured":true,"state":"available"}\n'
elif [[ "$*" == *"text/event-stream"* ]]; then
	if [[ "${FAKE_CURL_BAD_SSE:-}" == "true" ]]; then
		printf 'event: token\ndata: {"delta":"missing done"}\n\n'
	elif [[ "${FAKE_CURL_LEAK:-}" == "true" ]]; then
		printf 'event: token\ndata: {"delta":"AKIAIOSFODNN7EXAMPLE"}\n\nevent: done\ndata: {}\n\n'
	else
		printf 'event: token\ndata: {"delta":"governed"}\n\nevent: answer\ndata: {"answer_prose":"governed answer","evidence_handles":["citation:redacted-demo"],"truth":{"level":"code_hint"}}\n\nevent: done\ndata: {}\n\n'
	fi
elif [[ "$*" == *"/api/v0/ask"* ]]; then
	if [[ "${FAKE_CURL_LEAK:-}" == "true" ]]; then
		printf '{"answer_prose":"AKIAIOSFODNN7EXAMPLE","evidence_handles":["citation:redacted-demo"],"truth":{"level":"code_hint"}}\n'
	else
		printf '{"answer_prose":"governed answer","evidence_handles":["citation:redacted-demo"],"truth":{"level":"code_hint"}}\n'
	fi
else
	printf 'unexpected curl call: %s\n' "$*" >&2
	exit 2
fi
