# CI image sources and refresh

Required CI checks use fixed image digests. A registry pull failure still fails
its check. The image change does not change check names, required status,
queue selection, backend selection, or release attestations.

## Image boundaries

`specs/ci-images.v1.json` records the upstream source, approved GHCR destination,
index digest, platforms, source records, and notice policy for each mirror.
Its separate `tools` record pins the rebuilt CI tools index, recipe hash,
platform manifests, and package inventory digests consumed by the static gate.
Copy the full upstream index. Do not rebuild, retag a single-platform pull, or
add labels to an upstream mirror. Those acts change the content identity.

The existing PostgreSQL and main Neo4j mirror packages are reused by digest.
The refresh process must not change their tags, visibility, or access settings.
The other mirrors use dedicated `ci-mirror-*` packages. The NornicDB reference
keeps its existing GHCR digest in the separate `runtime_allowlist` ledger field;
it is an existing runtime dependency, not an image published by this mirror job.

The tools image serves the static contract gate matrix. Live Docker stacks run
on the host runner. The tools image contains Ubuntu, Go, and the CLI tools that
the matrix needs. It contains no Eshu checkout, Eshu executable, credentials,
or private build context. See `scripts/ci/tools.README.md` for its recipe,
license paths, package inventory, and local smoke test.

The tools recipe pins direct package versions and binary checksums. Ubuntu
transitive packages can advance at build time. Record the actual inventory for
each build. The consumed image digest freezes that build; the recipe alone is
not a claim that a future rebuild has identical bytes.

## Compose image selection

CI sets `ESHU_CI_IMAGES=1` and all five ledger pins: `POSTGRES_IMAGE`,
`NEO4J_IMAGE`, `ESHU_XX_IMAGE`, `ESHU_GO_IMAGE`, and `ESHU_ALPINE_IMAGE`.
The helper renders the original Compose stack in memory, then writes a minimal
image and build-argument overlay. It does not write the rendered environment
or secret values to disk or logs. The temporary directory has mode 0700 and
the overlay has mode 0600.

Append the overlay as the last Compose file. Use it for the whole stack
lifecycle, including logs, exec, inspection, and down. Remove the overlay only
after teardown. Each invocation owns its temporary path. The child wrapper
checks the prepared input signature before reuse.

Missing pins, pins that differ from the ledger, unknown images, and unknown
Dockerfile identities fail before a build starts. There is no Docker Hub
fallback in CI mode. Normal product Compose files and Dockerfile defaults keep
their original upstream references when CI mode is off. `GATE_STACK_DIR`
changes the explicit stack root; it does not change the helper's trusted ledger.
The Compose helper accepts NornicDB only at the exact repository, tag, and digest
listed in `runtime_allowlist`; an overridden NornicDB image fails preflight.

The PostgreSQL readiness fixture uses `ESHU_LIVE_POSTGRES_IMAGE`. The query-plan
profile uses `ESHU_QUERYPLAN_PROFILE_IMAGE`. These separate consumers must match
their ledger entries. Buildx uses the `CI_BUILDKIT_IMAGE` mirror explicitly so
its builder startup does not add an implicit Docker Hub pull.

## Queue-path gate

`scripts/verify-dockerhub-login.sh` fails a job that reaches Docker Hub when its
workflow runs on `merge_group`, `pull_request`, or `pull_request_target`, follows
such a workflow through `workflow_run`, or is a reusable callee of one. A login
step is not a finding of this rule.

The gate reads `specs/ci-images.v1.json` and the required image ids in
`scripts/lib/ci_image_compose.py` from the tree under test at run time. It holds
no copy of a digest or an image name. A step is in CI image mode when
`ESHU_CI_IMAGES=1` and every required env pin equals the ledger
`destination@digest`.

### Two classes of Compose call

The gate classifies each Compose call it finds in a run text and in every script
the step reaches.

| Class | Call | Verdict |
|---|---|---|
| W | `scripts/ci/compose.sh` as the command word (see below), in a step in CI image mode, with the wrapper in the tree | Settled. The gate does not read the call's `-f` operands or the names inside it. |
| R | Every other call: `docker compose`, `docker-compose`, a command array, a variable command word, a wrapper call in a step without CI image mode, a look-alike path | Judged by the images its files name. An `-f` operand that does not resolve to a file is Docker Hub. |

A wrapper call is W when the command word, behind `bash`, `sh`, `exec`,
`timeout N`, `nice`, `sudo -E` or `env`, is the literal path tail
`scripts/ci/compose.sh` with no prefix, `./`, or one `${VAR}`, `$VAR` or `"$VAR"`
followed by `/`. A literal `bash scripts/ci/compose.sh` in the arguments of a
runner function, in a literal `bash -c` string, in a pipeline, a function, a
subshell or a background job is W too: the wrapper is what runs. A path held in
a variable is not W.

Words that run before the path decide the call
(`scripts/lib/dockerhub-login-wrapper-head.awk`). It is R, whatever follows, when
they hold a `docker` or `docker-compose` word (the wrapper path is then an
argument of a Compose call, and `run` pulls the image first), a variable or
array expansion at command position (`$DOCKER compose ...`: what it holds is not
read), `podman`, `kubectl`, `ssh`, `su`, `runuser`, `nsenter`, `chroot`,
`systemd-run`, `doas`, `bwrap`, `bubblewrap` or `firejail`, a `sudo` without `-E`
(its default `env_reset` drops `GITHUB_ACTIONS` and CI image mode), `env -i`
(also in a flag chain such as `-0i`), `env -`, `exec -c`, `setpriv --reset-env`,
`env -u` of `GITHUB_ACTIONS` or `ESHU_CI_IMAGES`, or a
`NAME=value` word that sets `GITHUB_ACTIONS` to anything but `true` or
`ESHU_CI_IMAGES` to anything but `1`. Each of these runs the wrapper in an
environment where the run-time guard (which tests `GITHUB_ACTIONS`) never fires.

`"${ESHU_CI_COMPOSE_OVERLAY}"` as an `-f` operand settles nothing. A direct
`docker compose -f <file> -f "${ESHU_CI_COMPOSE_OVERLAY}" up` after a correct
prepare is an R call, and a Docker Hub image in its file is a reach. The gate
holds no model of prepare calls, arrays, aliases or flow.

### Why the wrapper is trusted

`scripts/ci/compose.sh` renders an overlay for the exact flag set of the call. The
overlay builder (`scripts/lib/ci_image_compose.py`) fails closed: a rendered
service image that is Docker Hub and not a ledger runtime source, an image
outside the allowlist, and a build that is not the canonical root Dockerfile all
exit 1, and the wrapper exits 1 with them. A changed flag set, file or pin
fails the signature check. In CI (`GITHUB_ACTIONS=true`) the wrapper also
refuses to run when `ESHU_CI_IMAGES` is not `1` (`ci-compose: CI image mode is
off in CI`), so a step that clears the variable becomes a red job, never a
Docker Hub pull. The guard cannot see a call that removed `GITHUB_ACTIONS`
itself; the gate treats the `env` and prefix shapes that do so as R (above).
`scripts/test-ci-image-compose.sh` runs these refusals.

### Migration rule

A CI Compose call that can pull or build (`up`, `run`, `pull`, `create`,
`build`) goes through `scripts/ci/compose.sh`. A direct `docker compose` with
one of those subcommands on the queue path is judged by its files. A direct call
with a non-pulling subcommand (`exec`, `ps`, `logs`, `down`, `config`, `stop`,
`restart`) reaches no image and stays outside the gate, as in main's gate; a
script may keep such calls direct where a wrapper call would render an overlay
per iteration of a poll. Pass the file flags before `--`:

```text
bash "${repo_root}/scripts/ci/compose.sh" "${compose_args[@]}" -- up -d
```

A script that makes many calls may call `eshu_ci_compose_prepare "${repo_root}"
"${compose_args[@]}"` once, so the wrapper takes the signature path. The
signature hashes the value of every variable that a root `docker-compose*.y*ml`
names, comments included, so after a prepare any `export`, `unset` or
`NAME=value` prefix of such a variable makes the next wrapper call exit 1
(`prepared overlay no longer matches`). Prepare after the last such write, or do
not prepare and let each call render its own overlay (about 1 s). The golden
corpus gate does not prepare: it runs one preflight call and one wrapper `up`,
keeps its readiness polls, `psql` helper and teardown direct, and exports the Ask
and metrics source variables afterwards (two wrapper renders per job instead of
about one per poll). Do not append the overlay by hand.

### Dockerfile ARGs and PATH-shadowed stubs

A Dockerfile `ARG` takes its value from `--build-arg`, then from its default;
the environment reaches it only through `--build-arg NAME=${NAME}`. A script that
writes a `docker` stub into a directory and puts that directory first on `PATH`,
both before any `docker` use in the same shell scope, has no docker reach for
itself or the scripts it runs. A call by absolute path, a prepend in a subshell,
or a reset of `PATH` between the prepend and the call breaks the shadow.

### Static limits

The gate reads text. It does not run a script, and main's gate had the same gaps.

- A wrapper path held in a variable is not seen at all: the call is neither W
  nor named as a look-alike, so a script that a variable names is judged only
  by the gate's reader of scripts.
- A tool that replaces the environment and is not in the list above is not
  seen: `chpst -e`, `envdir`, `capsh`, `runcon`, and any shell function or alias
  that wraps one of these or one of the listed tools. The list is the set a real
  call would reach for; it is not a proof that no other tool clears the
  environment.
- A direct `docker compose "${flags[@]}" exec ...` is read as non-pulling because
  the word after the array-held flags is `exec`. An array that itself holds a
  pulling subcommand (`flags=(-f x up)`) followed by a non-pulling word is not
  seen.
- A statement before the call that removes the mode (`unset GITHUB_ACTIONS
  ESHU_CI_IMAGES`, `export GITHUB_ACTIONS=false`) is not seen. The wrapper guard
  closes a write of `ESHU_CI_IMAGES` while `GITHUB_ACTIONS` stays `true`; a script
  that removes `GITHUB_ACTIONS` as well runs the wrapper raw. A runner function
  that clears the mode inside its own body is not seen either.
- The docker lexer reads a heredoc body only when the line that opens it is a
  shell (`bash <<EOF`). It does not read `cat <<EOF | bash`,
  `sh -c "$(cat <<EOF ...)"`, `eval "$(cat <<EOF ...)"`, `. /dev/stdin <<EOF`, or
  a body written to a file that a later command runs.
- Variable-held command names. Seen: a variable or array assigned the word
  `docker` (`DOCKER_BIN=docker; $DOCKER_BIN pull ...`, `DC=(docker compose ...);
  "${DC[@]}" up`) and a variable command word that passes `compose`
  (`$DOCKER compose up`). Each is a raw call. Not seen: a name built from quoted
  pieces (`dock""er`), by concatenation or by `printf`, and a command word read
  from a file, from the environment or from a command substitution.
- A stub directory variable that changes between the write and the prepend is
  not seen by the PATH-shadow model.

The reference reader (which scripts a file runs) fails closed on shapes it
cannot place: a heredoc body is read as commands when its opening line holds a
shell word, a dot command, or a pipe, when it is written to a file (`> file`,
`>> file`, `tee`), when it has no delimiter line, or when it substitutes a
command. An arithmetic `<<` (`$(( 1 << 2 ))`, `(( x = 1 << 3 ))`, `let`) opens
no heredoc. A conditional literal in a workflow env settles only in the form
`cond && 'a' || cond && 'b' || 'c'`, and every literal must settle.

## Refresh and proof

1. Read the upstream recipe, license notices, corresponding source records,
   and every platform manifest. Reject foreign or nondistributable layers.
2. Update the ledger in a reviewed branch. Preserve existing runtime versions
   unless the task explicitly includes a version update.
3. Run the publisher regression test. Prepare an exact canonical subset ledger
   for the six dedicated mirror destinations. Use explicit protected source
   and destination authfiles. Destination publication uses `linuxdynasty`.
4. Run `mirror-images.sh publish` with that ledger. Require exit 0 and one
   `COPIED` result per image. A copy result does not prove public access.
5. Set only the new dedicated packages public and link their repository.
   Run `mirror-images.sh verify` with the same ledger. It compares root
   manifests and pulls every child, config, and layer anonymously into OCI.
   Require exit 0 and one `PASS` result per image.
6. Build tools from an empty context for each supported architecture. Add the
   public repository source label and the recipe hash. Run the smoke test and
   the full selected matrix against a separate checkout per test container.
   Fixture tests must not share a mutable checkout.
7. Publish the tools image only after review. Record the registry index digest,
   package inventory, recipe hash, and anonymous pull proof. A local image
   configuration ID is not a registry index digest.
8. Update workflow pins. Prove Compose configuration parity, real startup and
   teardown, mirrored-base builds, and the selected blocking gates locally.
   Then follow the repository review, attestation, push, and merge queue rules.

Upstream mirror layers retain their license and copyright files unchanged.
Source records identify the known upstream projects and source releases. This
record is not a claim of a complete component-by-component legal audit.

## Failure diagnosis

CI step logs identify the selected image digest and the failed operation.
Distinguish source-copy failures, public-access failures, local overlay
validation failures, and runtime health failures. Fix the failing boundary;
do not mark a required check advisory to pass a registry outage. The change
adds no application runtime or OTEL instrumentation surface.
