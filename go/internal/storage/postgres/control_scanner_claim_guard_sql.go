// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// scannerDerivedClaimAuthority is shared by generic heartbeat and completion.
// A generic heartbeat cannot advance derived work before its result receipt;
// DerivedScannerImage routes heartbeat through the source-first protected path.
// Completion accepts only the exact same-transaction protected scan receipt.
const scannerDerivedClaimAuthority = `
      AND (
        (item.authorization_scope_id = ''
         AND item.scope_id NOT LIKE 'image://derived/%')
        OR EXISTS (
          SELECT 1 FROM scanner_image_scan_results AS result
          JOIN scanner_image_target_attempts AS attempt
            ON attempt.work_item_id = result.work_item_id
           AND attempt.binding_id = result.binding_id
           AND attempt.scan_scope_id = result.scan_scope_id
          WHERE result.work_item_id = item.work_item_id
            AND item.authorization_scope_id <> ''
            AND item.scope_id LIKE 'image://derived/%'
            AND result.scan_scope_id = item.scope_id
            AND result.scan_generation_id = item.generation_id
            AND result.claim_id = $5
            AND result.claim_fencing_token = $3
            AND result.claim_owner_id = $4
        )
      )
`

// scannerDerivedHeartbeatAuthority is substituted only after the protected
// source-first authorization function has locked every current witness in the
// same transaction. It never grants generic derived completion.
const scannerDerivedHeartbeatAuthority = `
      AND item.authorization_scope_id <> ''
      AND item.scope_id LIKE 'image://derived/%'
`
