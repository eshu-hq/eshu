// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"testing"
)

// TestWireAPIRejectsMalformedCursorKeyringBeforeConnectingDatastores pins the
// boot ordering the tag-history cursor sealing key (#6564) depends on: a
// malformed ESHU_AUTH_SECRET_ENC_KEY must abort wiring BEFORE any datastore is
// dialed, and therefore before BackfillCloudResourceOwnerLedger mutates the
// graph.
//
// The getenv below supplies a malformed DEK and NOTHING else -- no
// ESHU_POSTGRES_DSN, no graph backend. So the returned error names whichever
// step ran first. Parsed in its original position (beside the router, after
// the backfill), the key was never reached: wiring failed on the missing DSN
// instead, which is this test's RED. Parsed in the validation-before-datastore
// block it is reached first and the keyring error is what comes back.
//
// This covers the leaked-handle half of the same finding too, and covers it by
// construction rather than by assertion: an abort that happens before any
// handle is opened has no db.Close()/driver.Close() to forget.
func TestWireAPIRejectsMalformedCursorKeyringBeforeConnectingDatastores(t *testing.T) {
	_, _, _, _, err := wireAPI(context.Background(), func(key string) string {
		if key == "ESHU_AUTH_SECRET_ENC_KEY" {
			// Valid base64 that decodes to 5 bytes, not the 32 AES-256
			// requires: configured-but-malformed, which is the abort case.
			return "aGVsbG8="
		}
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want the malformed cursor keyring refusal")
	}
	if !strings.Contains(err.Error(), "configure tag-history cursor keyring") {
		t.Fatalf(
			"wireAPI() error = %q, want it to name the cursor keyring; a datastore error here means the key is still parsed after the datastores are opened",
			err,
		)
	}
}

// TestWireAPIKeepsMissingCursorKeyringNonFatal is the negative control for the
// test above, and guards the behaviour the reordering must NOT change: a
// deployment with no DEK at all degrades rather than aborting. Only a
// MALFORMED key is fatal.
//
// With no DEK and no DSN, wiring must fail on the DSN -- proving the keyring
// step let the boot through. Asserting only "some error" would pass even if a
// missing key had been made fatal, so the assertion is on which error.
func TestWireAPIKeepsMissingCursorKeyringNonFatal(t *testing.T) {
	_, _, _, _, err := wireAPI(context.Background(), func(string) string {
		return ""
	}, nil, nil)
	if err == nil {
		t.Fatal("wireAPI() error = nil, want the missing-DSN refusal")
	}
	if strings.Contains(err.Error(), "configure tag-history cursor keyring") {
		t.Fatalf("wireAPI() error = %q, want an unconfigured DEK to degrade, not abort", err)
	}
	if !strings.Contains(err.Error(), "ESHU_POSTGRES_DSN") {
		t.Fatalf("wireAPI() error = %q, want wiring to have proceeded past the keyring to the DSN check", err)
	}
}
