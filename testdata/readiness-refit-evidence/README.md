# Readiness refit evidence data

`inputs.tar.gz` holds the trimmed inputs of the 28 GitHub Actions runs behind
the pooled readiness refit (#9519) plus the jobs API listings of the two runs
behind the later cap cancellations (40933609276, 40951175891; jobs listings
only, no shard logs). `expected/` holds the outputs recorded when
that refit was made, plus the re-derived Prediction-table rows and the later
cancellation walls. `scripts/dev/readiness-refit-evidence/reproduce.sh` reads
both; its README lists the runs, the retention limits and how to rebuild the
archive.
