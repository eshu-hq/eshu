#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo 'usage: stage-differential-capture.sh check-leg DIR | stage DOWNLOAD_ROOT OUTPUT_ROOT RUN_ID CAPTURE_RESULT BACKEND...' >&2
  exit 2
}

require_recordings() {
  local dir="$1"
  local files=0
  local file
  if [[ ! -d "${dir}" ]]; then
    echo "::error title=Missing differential capture::${dir} does not exist" >&2
    return 1
  fi
  for file in "${dir}"/*.jsonl; do
    if [[ -f "${file}" && ! -L "${file}" && -s "${file}" ]]; then
      files=$((files + 1))
    fi
  done
  if (( files == 0 )); then
    echo "::error title=Missing differential capture::no non-empty *.jsonl recording in ${dir}" >&2
    return 1
  fi
  echo "${dir}: ${files} non-empty recording file(s)"
}

case "${1:-}" in
  check-leg)
    [[ "$#" -eq 2 ]] || usage
    require_recordings "$2"
    ;;
  stage)
    [[ "$#" -ge 6 ]] || usage
    download_root="$2"
    output_root="$3"
    run_id="$4"
    capture_result="$5"
    # The caller names the backends whose captures this run produced, so the
    # join requires exactly those legs. Disabling a backend in the workflow
    # matrix is then a workflow edit, not a change to this helper.
    shift 5
    backends=("$@")
    legs=()
    for backend in "${backends[@]}"; do
      case "${backend}" in
        nornicdb|neo4j) ;;
        *) echo "::error title=Unknown backend::${backend} is not nornicdb or neo4j" >&2; exit 2 ;;
      esac
      legs+=("pair1-${backend}" "pair2-${backend}")
    done
    if [[ "${capture_result}" != success ]]; then
      echo "::error title=Differential capture failed::matrix result is ${capture_result}" >&2
      exit 1
    fi
    if [[ -e "${output_root}" ]]; then
      echo "::error title=Stale differential capture::output path already exists: ${output_root}" >&2
      exit 1
    fi
    for leg in "${legs[@]}"; do
      source_dir="${download_root}/differential-capture-${run_id}-${leg}"
      require_recordings "${source_dir}"
    done
    trap 'rm -rf "${output_root}"' ERR
    for leg in "${legs[@]}"; do
      source_dir="${download_root}/differential-capture-${run_id}-${leg}"
      pair="${leg%-*}"
      backend="${leg#*-}"
      target_dir="${output_root}/${pair}/${backend}"
      mkdir -p "${target_dir}"
      cp -R "${source_dir}/." "${target_dir}/"
      require_recordings "${target_dir}"
    done
    trap - ERR
    ;;
  *) usage ;;
esac
