#!/usr/bin/env bash
# Conditional image expressions: both possible literal results must avoid Hub.
image_expr_case() { # <name> <container expression> <green|red>
  local dir
  dir="$(new_case "$1")"
  IMAGE_EXPR="$2" yq e -i '.jobs.fixture-image = {"runs-on": "ubuntu-latest", "container": {"image": strenv(IMAGE_EXPR)}, "steps": [{"run": "echo ok"}]}' "${dir}/.github/workflows/verify-agent-hygiene.yml"
  if [[ "$3" == green ]]; then
    expect_green "$1" "${dir}"
  else
    expect_red "$1" "${dir}" 'verify-agent-hygiene.yml:fixture-image: container'
  fi
}

image_expr_case literal-ghcr-or-empty "\${{ (github.event_name != 'merge_group' || matrix.queue_selected) && 'ghcr.io/eshu-hq/tools@sha256:abc' || '' }}" green
image_expr_case literal-foreign-or-ghcr "\${{ matrix.use_foreign && 'quay.io/eshu/tool:1' || 'ghcr.io/eshu/tool:1' }}" green
image_expr_case literal-hub-first "\${{ matrix.use_hub && 'docker.io/library/alpine:3' || 'ghcr.io/eshu/tool:1' }}" red
image_expr_case literal-hub-second "\${{ matrix.use_hub && 'ghcr.io/eshu/tool:1' || 'alpine:3' }}" red
image_expr_case dynamic-branch "\${{ matrix.use_image && matrix.image || 'ghcr.io/eshu/tool:1' }}" red
image_expr_case misleading-parentheses "\${{ (false) || 'docker.io/foo' || (true) && 'ghcr.io/foo' || '' }}" red
image_expr_case unbalanced-guard "\${{ (matrix.enabled && 'ghcr.io/eshu/tool:1' || '' }}" red
image_expr_case doubled-quote-image "\${{ matrix.enabled && 'ghcr.io/eshu/it''s-tool:1' || '' }}" red
