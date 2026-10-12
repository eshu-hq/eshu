#!/usr/bin/env bash
# The golden gate's Compose order against the real wrapper: lock held, one
# preflight call, the runtime exports, then the readiness call behind a variable
# prefix. Run by scripts/test-ci-image-compose.sh with repo_root and TEST_HOME set,
# the fake docker first on PATH and CI image mode on. A script of its own so that
# set -e is real. Exits 0 when every wrapper call ran.
set -euo pipefail
export GITHUB_ACTIONS=true
use_compose=1 graph_service=nornicdb
ESHU_POSTGRES_PORT=15432 ESHU_NEO4J_PASSWORD=change-me ESHU_POSTGRES_PASSWORD=change-me
compose_args=(-p eshu-golden -f docker-compose.yaml)
log() { :; }
die() { printf 'die: %s\n' "$*" >&2; exit 1; }
release_live_gate_lock() { :; }
. "${repo_root}/scripts/lib/golden-corpus-ci-compose.sh"
. "${repo_root}/scripts/lib/golden-corpus-readiness.sh"
host_tcp_port_open() { return 0; }
golden_corpus_preflight_ci_overlay
export ESHU_HOME="${TEST_HOME}" ESHU_API_KEY=golden-key ESHU_EMIT_DATAFLOW=true
export ESHU_FILESYSTEM_ROOT="${TEST_HOME}/corpus" ESHU_REPO_SOURCE_MODE=filesystem
start_golden_corpus_backends
