# Fragments for scripts/verify-moved-file-refs.sh (#8876).
#
# New entries go here, one file per removed component area, named
# go-<area>.txt for `go/` paths (e.g. go-cmd-webhook-listener.txt). The main
# scripts/moved-file-refs-allowlist.txt is frozen at the 500-line cap: do not
# append to it. An entry exempts ONE referencing file naming ONE vacated path,
# exactly like the main file (`<referencing-file>:<vacated-path>` with the
# `go/` prefix kept), with a comment saying why. The gate greps citations
# WITHOUT the prefix, so keep the key format identical.
