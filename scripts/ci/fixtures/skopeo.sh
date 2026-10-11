#!/usr/bin/env bash
printf '%s\n' "$*" >> "${MOCK_IMAGE_CALLS:?}"
# Match Skopeo's Docker transport: a tag cannot accompany a digest.
for argument in "$@"; do
  if [[ "${argument}" == docker://*@sha256:* ]]; then
    repository="${argument%@*}"
    [[ "${repository##*/}" != *:* ]] || exit 65
  fi
done
case "$1" in
  inspect) printf '{}\n' ;;
  manifest-digest) printf '%s\n' "${MOCK_DIGEST:?}" ;;
  copy)
    [[ "${MOCK_COPY_FAIL:-}" != 1 ]] || exit 7
    args=("$@")
    for ((i=0; i<${#args[@]}; i++)); do
      if [[ "${args[i]}" == --digestfile ]]; then
        printf '%s\n' "${MOCK_DIGEST:?}" > "${args[i+1]}"
      fi
    done ;;
esac
