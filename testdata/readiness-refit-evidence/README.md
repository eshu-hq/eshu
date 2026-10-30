# Readiness refit evidence data

`inputs.tar.gz` holds the trimmed inputs of the 28 GitHub Actions runs behind
the pooled readiness refit (#9519). `expected/` holds the outputs recorded when
that refit was made. `scripts/dev/readiness-refit-evidence/reproduce.sh` reads
both; its README lists the runs, the retention limits and how to rebuild the
archive.
