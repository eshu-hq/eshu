#!/usr/bin/env bash
# Promotes a just-pushed per-commit image digest to the moving `main` and
# `latest` tags (#7699). Push runs are concurrent per commit sha, so this runs
# in its own short job AFTER the build pushes: it moves the tags only when the
# commit they currently point at is an ancestor of (or missing before) this
# run's commit, and stays green without pushing when a newer commit already
# won. There is deliberately no job-level concurrency group here: GitHub keeps
# only one pending run per group, so a mutex would DROP intermediate
# promotions and cancel their runs (skipping their Trivy scans); ordering by
# completion needs the ancestry guard anyway. Rare seconds-long transient dips
# self-heal on the next attempt.
#
# Required env: IMAGE (ghcr.io/eshu-hq/eshu), REPOSITORY (eshu-hq/eshu),
# DIGEST (sha256:... the build pushed), MY_SHA (this run's full commit sha),
# GITHUB_REF, GITHUB_TOKEN (packages read for GHCR API; the docker login step
# covers imagetools). Optional: PROMOTE_ATTEMPTS (default 10),
# PROMOTE_INTERVAL_S (default 15). Needs a full-history checkout for the
# ancestry check and docker buildx for imagetools create.
set -euo pipefail

image="${IMAGE:?IMAGE is required}"
repo="${REPOSITORY:?REPOSITORY is required}"
digest="${DIGEST:-}"
my_sha="${MY_SHA:?MY_SHA is required}"
ref="${GITHUB_REF:?GITHUB_REF is required}"
token="${GITHUB_TOKEN:?GITHUB_TOKEN is required}"
attempts="${PROMOTE_ATTEMPTS:-10}"
interval="${PROMOTE_INTERVAL_S:-15}"

api="https://ghcr.io/v2/${repo}"

# tag_digest_of <tag>: prints the tag's digest. Fails when the tag is absent.
tag_digest_of() {
	local tag="$1" headers line
	headers="$(curl -s -D - -o /dev/null -u "x-access-token:${token}" \
		-H 'Accept: application/vnd.oci.image.index.v1+json' \
		"${api}/manifests/${tag}")"
	while IFS= read -r line; do
		line="${line%$'\r'}"
		if [[ "${line,,}" == "docker-content-digest:"* ]]; then
			printf '%s' "${line#*: }"
			return 0
		fi
	done <<<"${headers}"
	return 1
}

# ghcr_get <path>: prints the response body. Returns 10 on 404, 1 otherwise.
ghcr_get() {
	local path="$1" resp code body
	resp="$(curl -s -w $'\n%{http_code}' -u "x-access-token:${token}" \
		-H 'Accept: application/vnd.oci.image.index.v1+json' "${api}${path}")"
	code="${resp##*$'\n'}"
	body="${resp%$'\n'*}"
	case "${code}" in
	200) printf '%s' "${body}" ;;
	404) return 10 ;;
	*)
		echo "promote: GHCR ${path} failed with HTTP ${code}" >&2
		return 1
		;;
	esac
}

# tag_revision <tag>: prints the org.opencontainers.image.revision label of the
# image a tag points at (amd64 platform when it is an index). Returns 10 when
# the tag is absent, 1 when the label cannot be read.
tag_revision() {
	local tag="$1" doc platform_digest cfg
	doc="$(ghcr_get "/manifests/${tag}")" || return $?
	if [[ "$(jq -r '.manifests != null' <<<"${doc}")" == "true" ]]; then
		platform_digest="$(jq -r '[.manifests[] | select(.platform.architecture == "amd64" and .platform.os == "linux") | .digest][0] // empty' <<<"${doc}")"
		if [[ -z "${platform_digest}" ]]; then
			echo "promote: ${tag}: index has no linux/amd64 manifest" >&2
			return 1
		fi
		doc="$(ghcr_get "/manifests/${platform_digest}")" || return $?
	fi
	cfg="$(jq -r '.config.digest // empty' <<<"${doc}")"
	if [[ -z "${cfg}" ]]; then
		echo "promote: ${tag}: manifest has no config digest" >&2
		return 1
	fi
	doc="$(ghcr_get "/blobs/${cfg}")" || return $?
	# No rg here: the publish runners are plain ubuntu-latest, so this script
	# is limited to curl, jq, git, docker, and POSIX text tools.
	rev="$(jq -r '.config.Labels["org.opencontainers.image.revision"] // empty' <<<"${doc}")"
	if [[ -z "${rev}" ]]; then
		echo "promote: ${tag}: image has no revision label" >&2
		return 1
	fi
	printf '%s' "${rev}"
}

# promote_and_verify: moves both tags to the digest in one command, then
# re-reads both. Fails when either tag does not resolve to the digest.
promote_and_verify() {
	docker buildx imagetools create \
		-t "${image}:main" -t "${image}:latest" \
		"${image}@${digest}" || return 1
	local main_now="" latest_now=""
	main_now="$(tag_digest_of main 2>/dev/null)" || main_now=""
	latest_now="$(tag_digest_of latest 2>/dev/null)" || latest_now=""
	[[ "${main_now}" == "${digest}" && "${latest_now}" == "${digest}" ]]
}

case "${ref}" in
refs/tags/*)
	# A tag push carries immutable semver tags already; there is nothing to
	# promote and the moving tags must not follow a tag.
	echo "promote: ${ref} is a tag; nothing to promote"
	exit 0
	;;
refs/heads/main)
	: # guarded promotion below
	;;
refs/heads/*)
	# Manual dispatch on a non-main branch: push that branch's tag
	# unguarded, matching what type=ref,event=branch used to push. The
	# operator invoked this run by hand, so there is no contention to guard.
	branch="${ref#refs/heads/}"
	tag="$(printf '%s' "${branch}" | tr -c 'A-Za-z0-9_.-' '-')"
	if [[ -z "${digest}" ]]; then
		echo "promote: build digest is empty; refusing to promote" >&2
		exit 1
	fi
	echo "promote: dispatch on ${branch}; pushing branch tag unguarded"
	docker buildx imagetools create -t "${image}:${tag}" "${image}@${digest}"
	exit $?
	;;
*)
	echo "promote: unexpected ref ${ref}" >&2
	exit 1
	;;
esac

if [[ -z "${digest}" ]]; then
	echo "promote: build digest is empty; refusing to promote" >&2
	exit 1
fi

attempt=1
while [[ "${attempt}" -le "${attempts}" ]]; do
	echo "promote: attempt ${attempt}/${attempts}"
	sha_now="$(tag_digest_of "sha-${my_sha}" 2>/dev/null)" || sha_now=""
	current=""
	rc=0
	if [[ "${sha_now}" == "${digest}" ]]; then
		current="$(tag_revision main 2>/dev/null)" || rc=$?
	fi
	if [[ "${sha_now}" != "${digest}" ]]; then
		echo "promote: sha-${my_sha} does not resolve to ${digest} yet; waiting ${interval}s"
	elif [[ "${rc}" == "10" ]]; then
		echo "promote: :main is absent; promoting"
		promote_and_verify && exit 0
		echo "promote: verify failed after push; waiting ${interval}s"
	elif [[ "${rc}" != "0" ]]; then
		echo "promote: cannot read :main; waiting ${interval}s"
	elif [[ "${current}" == "${my_sha}" ]]; then
		main_now="$(tag_digest_of main 2>/dev/null)" || main_now=""
		if [[ "${main_now}" == "${digest}" ]]; then
			echo "promote: :main already points at ${digest}; done"
			exit 0
		fi
		echo "promote: :main carries our revision with a different digest; re-promoting"
		promote_and_verify && exit 0
		echo "promote: verify failed after push; waiting ${interval}s"
	elif git merge-base --is-ancestor "${my_sha}" "${current}" 2>/dev/null; then
		echo "promote: :main already carries newer commit ${current}; skipping"
		exit 0
	elif git merge-base --is-ancestor "${current}" "${my_sha}" 2>/dev/null; then
		echo "promote: :main carries ancestor ${current}; promoting"
		promote_and_verify && exit 0
		echo "promote: verify failed after push; waiting ${interval}s"
	else
		echo "::error::promote: :main carries ${current}, unrelated to ${my_sha}; refusing to move it" >&2
		exit 1
	fi
	attempt=$((attempt + 1))
	sleep "${interval}"
done
echo "::error::promote: moving tags did not converge after ${attempts} attempts" >&2
exit 1
