// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

const contentSearchIndexFinalizerLockSQL = `
SELECT pg_advisory_xact_lock(
  hashtext('eshu_content_substring_indexes'),
  hashtext('finalizer')
)
`

const contentSearchIndexClaimBuildSQL = `
UPDATE content_substring_index_state
SET state = 'building',
    build_started_at = clock_timestamp(),
    build_completed_at = NULL,
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND NOT (
    state = 'ready'
    AND eshu_content_substring_indexes_valid()
  )
`

const contentSearchIndexPublishReadySQL = `
UPDATE content_substring_index_state
SET state = 'ready',
    build_completed_at = clock_timestamp(),
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND eshu_content_substring_indexes_valid()
`

const contentSearchIndexPublishFailedSQL = `
UPDATE content_substring_index_state
SET state = 'failed',
    failed_at = clock_timestamp(),
    failure_class = 'index_build_failed',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND NOT eshu_content_substring_indexes_valid()
`
