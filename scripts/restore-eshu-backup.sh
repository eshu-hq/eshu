#!/usr/bin/env bash
set -euo pipefail

cat >&2 <<'EOF'
The historical Python backup-restore helper is not shipped on the Go branch.

There is no supported local restore wrapper in this repository today.
If you need to recover an old backup, restore it with the backing datastore
tooling directly or recover the historical helper from git history.
EOF
exit 1
