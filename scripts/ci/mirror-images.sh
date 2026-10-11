#!/usr/bin/env bash
# Publish canonical CI mirrors, then independently verify full anonymous pulls.
set -euo pipefail
[[ $# == 2 ]] || { echo 'usage: mirror-images.sh verify|publish LEDGER.json' >&2; exit 64; }
mode="$1"
ledger="$2"
[[ "${mode}" == verify || "${mode}" == publish ]] || exit 64
for tool in skopeo jq cmp; do command -v "${tool}" >/dev/null; done
root="$(cd "$(dirname "$0")/../.." && pwd)"
spec="${root}/specs/ci-images.v1.json"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT
printf '{"auths":{}}\n' > "${work}/anonymous.json"
# Exact canonical entry equality binds ID, source, destination, digest, platforms,
# and source/notice metadata. A nonempty unique subset is the only accepted input.
jq -e --slurpfile canonical "${spec}" '
  .version == $canonical[0].version and
  (.images | type == "array" and length > 0) and
  ([.images[].id] | length == (unique | length)) and
  ([.images[].destination] | length == (unique | length)) and
  all(.images[]; . as $entry | any($canonical[0].images[]; . == $entry))
' "${ledger}" >/dev/null || { echo 'Ledger differs from canonical CI image spec' >&2; exit 1; }
if [[ "${mode}" == publish ]]; then
  jq -e 'all(.images[]; .destination | test("^ghcr.io/eshu-hq/ci-mirror-(neo4j|xx|golang|alpine|buildkit|ubuntu)$"))' \
    "${ledger}" >/dev/null || { echo 'Publish includes a read-only or unapproved package' >&2; exit 1; }
  [[ -n "${CI_IMAGE_DEST_AUTHFILE:-}" && -f "${CI_IMAGE_DEST_AUTHFILE}" ]] || {
    echo 'Publish requires CI_IMAGE_DEST_AUTHFILE' >&2; exit 1;
  }
  # Inspect only the actor name. Never render the decoded credential or token.
  jq -e '(.auths["ghcr.io"].auth // "" | @base64d | split(":")) as $parts |
    ($parts | length >= 2) and $parts[0] == "linuxdynasty" and ($parts[1] | length > 0)' \
    "${CI_IMAGE_DEST_AUTHFILE}" >/dev/null 2>&1 || {
      echo 'Destination authfile must name the linuxdynasty GHCR actor' >&2; exit 1;
    }
fi
jq -r '.images[] | [.source, .destination, .digest] | @tsv' "${ledger}" > "${work}/images.tsv"
while IFS=$'\t' read -r source destination digest; do
  # The approved sources use docker.io (no registry port). Remove the tag;
  # Skopeo rejects tag+digest references and the digest alone selects content.
  repository="${source%:*}"
  upstream="docker://${repository}@${digest}"
  mirror="docker://${destination}@${digest}"
  source_auth="${CI_IMAGE_SOURCE_AUTHFILE:-${work}/anonymous.json}"
  skopeo inspect --authfile "${source_auth}" --raw "${upstream}" > "${work}/source.json"
  actual="$(skopeo manifest-digest "${work}/source.json")"
  [[ "${actual}" == "${digest}" ]] || { echo 'Source digest mismatch' >&2; exit 1; }
  if [[ "${mode}" == publish ]]; then
    tag="${digest#sha256:}"
    skopeo copy --src-authfile "${source_auth}" --dest-authfile "${CI_IMAGE_DEST_AUTHFILE}" \
      --all --preserve-digests --digestfile "${work}/copied.digest" \
      "${upstream}" "docker://${destination}:sha256-${tag}"
    [[ "$(<"${work}/copied.digest")" == "${digest}" ]] || {
      echo 'Copied digest mismatch' >&2; exit 1;
    }
    printf 'COPIED: %s@%s; run verify after the package is public\n' "${destination}" "${digest}"
    continue
  fi
  skopeo inspect --authfile "${work}/anonymous.json" --raw "${mirror}" > "${work}/mirror.json"
  cmp "${work}/source.json" "${work}/mirror.json"
  # --all traverses every runtime child and its config/layers. --preserve-digests
  # ensures the OCI destination did not silently rewrite a manifest descriptor.
  # The anonymous source authfile excludes any ambient registry credentials.
  skopeo copy --src-authfile "${work}/anonymous.json" --all --preserve-digests \
    --digestfile "${work}/pulled.digest" "${mirror}" \
    "oci:${work}/pulled:${digest#sha256:}"
  [[ "$(<"${work}/pulled.digest")" == "${digest}" ]] || {
    echo 'Anonymous full pull changed the root digest' >&2; exit 1;
  }
  printf 'PASS: anonymous full image %s@%s\n' "${destination}" "${digest}"
done < "${work}/images.tsv"
