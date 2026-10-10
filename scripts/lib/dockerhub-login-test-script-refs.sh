#!/usr/bin/env bash
#
# dockerhub-login-test-script-refs.sh - seeded RED/GREEN cases for how the
# verifier follows a repository script a workflow step runs (#7886, finding J1).
# Sourced, never run, from scripts/test-verify-dockerhub-login.sh after the
# shapes batch. It uses the test's helpers (new_case, add_job, expect_green) and
# failure counter, plus expect_jobs_red from lib/dockerhub-login-test-shapes.sh.
#
# The script a step names hides its docker use from the gate unless the gate
# follows the call. Every shape below runs scripts/fixture-pull.sh, which pulls a
# Docker Hub image, behind a prefix the gate must see through: an env prefix, a
# path-qualified shell, a wrapper with flags, or a second call on one line. Each
# batch plants a plain `bash scripts/fixture-pull.sh` control as one of its jobs,
# so the verifier fails and names every shape it missed.
#
# shellcheck disable=SC2154  # failures, repo_root, verifier come from the test

# j1_fixtures <dir>: the scripts the shapes call.
j1_fixtures() {
  printf '#!/usr/bin/env bash\ndocker pull alpine:3.21\n' >"$1/scripts/fixture-pull.sh"
  printf '#!/usr/bin/env bash\necho ok\n' >"$1/scripts/fixture-ok.sh"
  printf '#!/usr/bin/env bash\ndocker pull ghcr.io/eshu-hq/eshu:main\n' >"$1/scripts/fixture-ghcr.sh"
}

# --- J1: an env prefix before the script call -------------------------------------
d="$(new_case j1-env-prefix)"
j1_fixtures "${d}"
add_job "${d}" verify-agent-hygiene j1-control-plain-bash 'bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-prefix-bash 'RT=1 bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-prefix-direct 'RT=1 scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-three-env-prefixes 'A=1 B=2 C=3 bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-quoted-value 'RT="a b" bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-wrapper-assignment 'env RT=1 bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-prefix-behind-then $'if true; then RT=1 bash scripts/fixture-pull.sh; fi'
expect_jobs_red j1-env-prefixed-script-calls-need-the-hub-login "${d}" j1-control-plain-bash j1-env-prefix-bash \
  j1-env-prefix-direct j1-three-env-prefixes j1-env-quoted-value j1-env-wrapper-assignment j1-env-prefix-behind-then

# --- J1: a path-qualified shell ---------------------------------------------------
d="$(new_case j1-path-qualified-shell)"
j1_fixtures "${d}"
add_job "${d}" verify-agent-hygiene j1-control-plain-bash 'bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-bin-bash '/bin/bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-usr-bin-bash-flags '/usr/bin/bash -eu scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-usr-bin-env-bash '/usr/bin/env bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-prefix-and-path-qualified-shell 'RT=1 /bin/bash scripts/fixture-pull.sh'
expect_jobs_red j1-path-qualified-shell-calls-need-the-hub-login "${d}" j1-control-plain-bash j1-bin-bash \
  j1-usr-bin-bash-flags j1-usr-bin-env-bash j1-env-prefix-and-path-qualified-shell

# --- J1: a wrapper with flags, or the script passed as an argument ----------------
d="$(new_case j1-wrapper-flags)"
j1_fixtures "${d}"
add_job "${d}" verify-agent-hygiene j1-control-plain-bash 'bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-sudo-flag 'sudo -E bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-sudo-user-flag 'sudo -u root bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-ignore-environment 'env -i bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-env-unset-flag 'env -u HOME bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-nice-flag 'nice -n 5 bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-xargs-from-pipe 'echo scripts/fixture-pull.sh | xargs bash'
add_job "${d}" verify-agent-hygiene j1-xargs-flag 'xargs -n1 bash scripts/fixture-pull.sh'
expect_jobs_red j1-wrapper-flag-script-calls-need-the-hub-login "${d}" j1-control-plain-bash j1-sudo-flag \
  j1-sudo-user-flag j1-env-ignore-environment j1-env-unset-flag j1-nice-flag j1-xargs-from-pipe j1-xargs-flag

# --- J1: more than one script call on a line --------------------------------------
d="$(new_case j1-several-calls-on-a-line)"
j1_fixtures "${d}"
add_job "${d}" verify-agent-hygiene j1-control-plain-bash 'bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-second-after-and 'bash scripts/fixture-ok.sh && bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-second-after-semicolon 'bash scripts/fixture-ok.sh; scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-second-env-prefixed 'bash scripts/fixture-ok.sh && RT=1 bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-second-inside-loop $'for i in 1 2; do bash scripts/fixture-ok.sh; bash scripts/fixture-pull.sh; done'
expect_jobs_red j1-second-script-call-on-a-line-needs-the-hub-login "${d}" j1-control-plain-bash j1-second-after-and \
  j1-second-after-semicolon j1-second-env-prefixed j1-second-inside-loop

# --- J1 GREEN controls: the same prefixes on scripts that pull nothing from Hub ---
# A script mention in a comment, in a quoted string, as the argument of a command
# that does not run it, or in a variable that is only read is data (a documented
# limit: a script named only in a string is not followed).
d="$(new_case j1-controls)"
j1_fixtures "${d}"
add_job "${d}" verify-agent-hygiene j1-env-prefix-no-docker 'RT=1 bash scripts/fixture-ok.sh'
add_job "${d}" verify-agent-hygiene j1-path-qualified-no-docker '/bin/bash scripts/fixture-ok.sh'
add_job "${d}" verify-agent-hygiene j1-sudo-flag-no-docker 'sudo -E bash scripts/fixture-ok.sh'
add_job "${d}" verify-agent-hygiene j1-env-prefix-ghcr-only 'RT=1 bash scripts/fixture-ghcr.sh'
add_job "${d}" verify-agent-hygiene j1-comment-names-pulling-script $'# RT=1 bash scripts/fixture-pull.sh\necho hello'
add_job "${d}" verify-agent-hygiene j1-trailing-comment-names-pulling-script 'echo hello # bash scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-echo-string-names-pulling-script 'echo "run scripts/fixture-pull.sh later"'
# A script named as data, not run: an argument to a linter or a test, or held in
# a variable that is only read.
add_job "${d}" verify-agent-hygiene j1-shellcheck-argument 'shellcheck scripts/fixture-pull.sh'
add_job "${d}" verify-agent-hygiene j1-test-f-argument $'test -f scripts/fixture-pull.sh && echo present'
add_job "${d}" verify-agent-hygiene j1-assigned-path-never-run $'prepr="${PWD}/scripts/fixture-pull.sh"\nwc -l "${prepr}"'
expect_green j1-prefixed-calls-of-hub-free-scripts-need-no-login "${d}"
