# dockerhub-login-wrapper-head.awk - decides whether the words that run before a
# literal scripts/ci/compose.sh path let the call stay a wrapper call (#7886).
# Loaded with dockerhub-login-compose-flow.awk, which calls headbad() from
# is_wrapper(). The wrapper's run-time guard tests GITHUB_ACTIONS, so a call that
# removes it, runs the wrapper in a fresh environment, or is itself a Compose call
# carrying the path as arguments is a raw call judged by its files.

# headbad is 1 when the words T[a..e], which run before the wrapper path, make the
# call a raw call:
#   - a docker or docker-compose word (the path is then an argument of a Compose
#     call, and run pulls the image first);
#   - a command that starts another environment: podman, kubectl, ssh, su, runuser,
#     nsenter, chroot, systemd-run, doas, bwrap, bubblewrap, firejail;
#   - sudo without -E (env_reset drops GITHUB_ACTIONS and the mode), and sudo -i/-s;
#   - env with -i (also in a flag chain such as -0i), a bare -, --ignore-environment,
#     or -u/--unset of GITHUB_ACTIONS or ESHU_CI_IMAGES; exec -c; setpriv --reset-env;
#   - a NAME=value word that sets GITHUB_ACTIONS to anything but true or
#     ESHU_CI_IMAGES to anything but 1;
#   - a variable or array expansion at command position ($DOCKER, "${DC[@]}"): what
#     it holds is not read, and it may be docker compose.
function headbad(T, a, e,   k, m, w, base, n, v, keep, nxt) {
  for (k = a; k <= e; k++) {
    w = unq(T[k]); base = w; sub(/^.*\//, "", base)
    if (k == a && w ~ /^\$/) return 1
    if (base ~ /^(docker|docker-compose|podman|kubectl|ssh|su|runuser|nsenter|chroot|systemd-run|doas|bwrap|bubblewrap|firejail)$/) return 1
    if (base == "sudo") {
      keep = 0
      for (m = k + 1; m <= e; m++) {
        n = unq(T[m])
        if (n == "-E" || n == "--preserve-env") keep = 1
        if (n == "-i" || n == "--login" || n == "-s" || n == "--shell") return 1
      }
      if (!keep) return 1
    }
    if (base == "env") {
      for (m = k + 1; m <= e; m++) {
        n = unq(T[m])
        if (n == "-" || n == "--ignore-environment" || n ~ /^-[A-Za-z0-9]*i[A-Za-z0-9]*$/) return 1
        v = ""
        if (n == "--unset") v = unq(T[m + 1])
        else if (n ~ /^--unset=/) { v = n; sub(/^--unset=/, "", v) }
        else if (n ~ /^-[A-Za-z0-9]*u/) {
          # -u NAME, -uNAME and a short-flag chain ending in u (-0u NAME)
          v = n; sub(/^-[A-Za-z0-9]*u/, "", v)
          if (v == "") v = unq(T[m + 1])
        }
        if (v == "GITHUB_ACTIONS" || v == "ESHU_CI_IMAGES") return 1
      }
    }
    if (base == "setpriv") {
      for (m = k + 1; m <= e; m++) if (unq(T[m]) == "--reset-env") return 1
    }
    if (base == "exec") {
      for (m = k + 1; m <= e; m++) { n = unq(T[m]); if (n ~ /^-[A-Za-z]*c[A-Za-z]*$/) return 1 }
    }
    # the word a transparent prefix runs: a variable there is not read either
    if (base ~ /^(env|exec|sudo|nohup|command|time)$/) {
      for (m = k + 1; m <= e; m++) {
        nxt = unq(T[m])
        if (nxt ~ /^-/ || nxt ~ /^[A-Za-z_][A-Za-z0-9_]*=/) continue
        if (nxt ~ /^\$/) return 1
        break
      }
    }
    if (w ~ /^GITHUB_ACTIONS=/) { v = w; sub(/^[^=]*=/, "", v); if (unq(v) != "true") return 1 }
    if (w ~ /^ESHU_CI_IMAGES=/) { v = w; sub(/^[^=]*=/, "", v); if (unq(v) != "1") return 1 }
  }
  return 0
}
