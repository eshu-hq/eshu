#!/bin/bash
# usage: 7759-wal-one.sh <table> <round>
T=$1
/usr/lib/postgresql/16/bin/psql -h 127.0.0.1 -p 55439 -U postgres -q -c "TRUNCATE $T;" -c "CHECKPOINT;" -c "EXPLAIN (ANALYZE, WAL) INSERT INTO $T SELECT * FROM content_entities WHERE repo_id IN ('repository:meda','repository:small','repository:tiny');" 2>&1 | rg "WAL|Execution Time|INSERT"
