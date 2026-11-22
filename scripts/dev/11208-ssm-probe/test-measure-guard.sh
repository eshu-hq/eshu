#!/usr/bin/env bash
# Self-test for the container-name guard in measure.sh (#11208). Hermetic: a stub
# `docker` on PATH records its calls, so no container or database is involved.
#   RED:   a name that is not perf11208-* must exit 2 and never call docker.
#   GREEN: a perf11208-* name must reach docker (the stub exits 0).
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT
log="${tmp}/docker.log"
cat >"${tmp}/docker" <<STUB
#!/usr/bin/env bash
echo "\$*" >>"${log}"
cat >/dev/null
STUB
chmod +x "${tmp}/docker"
export PATH="${tmp}:${PATH}"

fail=0
for name in prod-postgres eshu-postgres-1 perf11208 xperf11208-pg; do
  : >"${log}"
  set +e
  "${here}/measure.sh" "${name}" ssm 2>/dev/null
  status=$?
  set -e
  if [[ "${status}" -ne 2 || -s "${log}" ]]; then
    echo "FAIL: '${name}' exit=${status} docker calls: $(cat "${log}")" >&2
    fail=1
  else
    echo "ok: '${name}' refused with exit 2, docker not called"
  fi
done

: >"${log}"
"${here}/measure.sh" perf11208-pg ssm
if ! rg -q 'exec -i perf11208-pg psql' "${log}"; then
  echo "FAIL: perf11208-pg did not reach docker exec" >&2
  fail=1
else
  echo "ok: perf11208-pg reached docker exec"
fi
exit "${fail}"
