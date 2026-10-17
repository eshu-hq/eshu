#!/bin/sh
# SPDX-License-Identifier: MIT
# Copyright (c) 2025-2026 eshu-hq
# Run psql inside the disposable proof container (named eshu-proof-7766).
# Usage: ./psql.sh < sql/01_seed_background.sql
exec docker exec -i eshu-proof-7766 psql -U eshu -d eshu -X -v ON_ERROR_STOP=1 "$@"
