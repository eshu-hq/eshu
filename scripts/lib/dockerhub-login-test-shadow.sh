#!/usr/bin/env bash
#
# dockerhub-login-test-shadow.sh - seeded RED/GREEN cases for a PATH-shadowed
# docker stub on the queue path of scripts/verify-dockerhub-login.sh (#7886).
# Sourced, never run, from scripts/test-verify-dockerhub-login.sh after the queue
# batch. It uses queue_tree, qjob, steps_run and expect_queue_jobs_red from
# lib/dockerhub-login-test-queue.sh and the test's helpers.
#
# A script that writes an executable named docker into a directory D and puts D
# first on PATH, both before any docker use and in the same shell scope, has no
# real docker reach for itself or for the scripts it runs (the two real shapes are
# scripts/test-ci-image-compose.sh and scripts/ci/test-build-reproducibility-image.sh).
# Each RED job breaks one bound: the stub is written after the call, in a
# directory that is never prepended, a different directory than the one
# prepended, the prepend is in a subshell, PATH is reset between, docker is
# called by absolute path, or a child script runs outside the region. The GREEN
# jobs keep the real shapes. There is no list of scripts: every case plants its
# own files and the derivation reads their text.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

# sh_put <dir> <name>: writes scripts/<name> from stdin behind a bash shebang.
sh_put() {
  { printf '#!/usr/bin/env bash\n'; cat; } >"$1/scripts/$2"
}

# sh_job <dir> <job> <script name>: a queue job that runs the script.
sh_job() { qjob "$1" "$2" "$(steps_run "bash scripts/$3")"; }

# --- GREEN: the stub shapes the repository's own tests use -------------------------
queue_tree queue-green-path-shadowed-docker
d="${QUEUE_DIR}"
sh_put "${d}" fixture-sh-real-shape.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
cat >"${work}/bin/docker" <<'SH'
#!/usr/bin/env bash
exit 0
SH
chmod +x "${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
docker pull alpine:3.21
EOF
sh_job "${d}" g-real-shape-export fixture-sh-real-shape.sh
sh_put "${d}" fixture-sh-assign-shape.sh <<'EOF'
work="$(mktemp -d)"
mkdir "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
chmod +x "${work}/bin/docker"
PATH="${work}/bin:${PATH}"
docker pull alpine:3.21
EOF
sh_job "${d}" g-assignment-without-export fixture-sh-assign-shape.sh
sh_put "${d}" fixture-sh-child-pull.sh <<'EOF'
docker pull alpine:3.21
EOF
sh_put "${d}" fixture-sh-child-shape.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
chmod +x "${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
bash scripts/fixture-sh-child-pull.sh
EOF
sh_job "${d}" g-child-script-inherits-the-stub fixture-sh-child-shape.sh
expect_green queue-path-script-with-a-path-shadowed-docker-has-no-reach "${d}"

# --- RED: each bound of the model --------------------------------------------------
queue_tree queue-red-path-shadow-bounds
d="${QUEUE_DIR}"
sh_put "${d}" fixture-sh-after-call.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
docker pull alpine:3.21
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
EOF
sh_job "${d}" r-stub-written-after-the-call fixture-sh-after-call.sh
sh_put "${d}" fixture-sh-not-prepended.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
docker pull alpine:3.21
EOF
sh_job "${d}" r-stub-directory-never-prepended fixture-sh-not-prepended.sh
sh_put "${d}" fixture-sh-other-dir.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin" "${work}/other"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/other:${PATH}"
docker pull alpine:3.21
EOF
sh_job "${d}" r-prepended-directory-is-not-the-stub-directory fixture-sh-other-dir.sh
sh_put "${d}" fixture-sh-subshell.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
( export PATH="${work}/bin:${PATH}"; true )
docker pull alpine:3.21
EOF
sh_job "${d}" r-prepend-in-a-subshell fixture-sh-subshell.sh
sh_put "${d}" fixture-sh-reset.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
PATH=/usr/bin:/bin
docker pull alpine:3.21
EOF
sh_job "${d}" r-path-reset-between fixture-sh-reset.sh
sh_put "${d}" fixture-sh-absolute.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
/usr/bin/docker pull alpine:3.21
EOF
sh_job "${d}" r-absolute-path-docker-after-the-prepend fixture-sh-absolute.sh
sh_put "${d}" fixture-sh-env-i.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
env -i docker pull alpine:3.21
EOF
sh_job "${d}" r-env-i-clears-the-path fixture-sh-env-i.sh
sh_put "${d}" fixture-sh-both-sides.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
docker pull alpine:3.21
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
docker pull alpine:3.21
EOF
sh_job "${d}" r-one-call-before-the-region fixture-sh-both-sides.sh
sh_put "${d}" fixture-sh-child-first.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
bash scripts/fixture-sh-child-pull.sh
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
EOF
sh_job "${d}" r-child-script-runs-before-the-stub fixture-sh-child-first.sh
sh_put "${d}" fixture-sh-child-pull.sh <<'EOF'
docker pull alpine:3.21
EOF
sh_put "${d}" fixture-sh-child-reset.sh <<'EOF'
PATH=/usr/bin:/bin
docker pull alpine:3.21
EOF
sh_put "${d}" fixture-sh-parent-of-reset.sh <<'EOF'
work="$(mktemp -d)"
mkdir -p "${work}/bin"
printf '#!/bin/sh\nexit 0\n' >"${work}/bin/docker"
export PATH="${work}/bin:${PATH}"
bash scripts/fixture-sh-child-reset.sh
EOF
sh_job "${d}" r-child-script-resets-the-path fixture-sh-parent-of-reset.sh
sh_job "${d}" r-no-stub-at-all fixture-sh-child-pull.sh
expect_queue_jobs_red queue-path-shadow-bounds-fail "${d}" r-stub-written-after-the-call r-stub-directory-never-prepended \
  r-prepended-directory-is-not-the-stub-directory r-prepend-in-a-subshell r-path-reset-between \
  r-absolute-path-docker-after-the-prepend r-env-i-clears-the-path r-one-call-before-the-region \
  r-child-script-runs-before-the-stub r-child-script-resets-the-path r-no-stub-at-all
