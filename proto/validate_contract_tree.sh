#!/usr/bin/env bash
set -euo pipefail

# Run from the repo root: bash proto/validate_contract_tree.sh
root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
proto_dir="${root_dir}/proto/eshu/data_plane"

required_files=(
  "${proto_dir}/scope/v1/scope.proto"
  "${proto_dir}/facts/v1/facts.proto"
  "${proto_dir}/queue/v1/queue.proto"
  "${proto_dir}/reducer/v1/reducer.proto"
  "${proto_dir}/projection/v1/projection.proto"
  "${root_dir}/buf.yaml"
  "${root_dir}/buf.gen.yaml"
)

for file in "${required_files[@]}"; do
  if [[ ! -f "${file}" ]]; then
    echo "missing required contract file: ${file}" >&2
    exit 1
  fi
done

declare -A expected_packages=(
  ["${proto_dir}/scope/v1/scope.proto"]="eshu.data_plane.scope.v1"
  ["${proto_dir}/facts/v1/facts.proto"]="eshu.data_plane.facts.v1"
  ["${proto_dir}/queue/v1/queue.proto"]="eshu.data_plane.queue.v1"
  ["${proto_dir}/reducer/v1/reducer.proto"]="eshu.data_plane.reducer.v1"
  ["${proto_dir}/projection/v1/projection.proto"]="eshu.data_plane.projection.v1"
)

declare -A expected_go_packages=(
  ["${proto_dir}/scope/v1/scope.proto"]="github.com/eshu-hq/eshu/go/gen/proto/eshu/data_plane/scope/v1;scopev1"
  ["${proto_dir}/facts/v1/facts.proto"]="github.com/eshu-hq/eshu/go/gen/proto/eshu/data_plane/facts/v1;factsv1"
  ["${proto_dir}/queue/v1/queue.proto"]="github.com/eshu-hq/eshu/go/gen/proto/eshu/data_plane/queue/v1;queuev1"
  ["${proto_dir}/reducer/v1/reducer.proto"]="github.com/eshu-hq/eshu/go/gen/proto/eshu/data_plane/reducer/v1;reducerv1"
  ["${proto_dir}/projection/v1/projection.proto"]="github.com/eshu-hq/eshu/go/gen/proto/eshu/data_plane/projection/v1;projectionv1"
)

for file in "${!expected_packages[@]}"; do
  package_line="package ${expected_packages[${file}]};"
  if ! rg -n --fixed-strings "${package_line}" "${file}" >/dev/null; then
    echo "missing package declaration '${package_line}' in ${file}" >&2
    exit 1
  fi
done

for file in "${!expected_go_packages[@]}"; do
  go_package_line="option go_package = \"${expected_go_packages[${file}]}\";"
  if ! rg -n --fixed-strings "${go_package_line}" "${file}" >/dev/null; then
    echo "missing go_package declaration '${go_package_line}' in ${file}" >&2
    exit 1
  fi
done

echo "contract tree validation passed"
