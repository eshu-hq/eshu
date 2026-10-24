// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// StoreOutcome names what ApplyContentStore did to one file body. The values
// are the bounded `outcome` label of the content-store policy counter.
type StoreOutcome string

const (
	// StoreOutcomeStored means the definition stores the body verbatim.
	StoreOutcomeStored StoreOutcome = "stored"
	// StoreOutcomeMetadata means the metadata policy withheld the body.
	StoreOutcomeMetadata StoreOutcome = "metadata"
	// StoreOutcomeRedacted means a redact policy replaced at least one value.
	StoreOutcomeRedacted StoreOutcome = "redacted"
	// StoreOutcomeClean means a redact policy found nothing to replace.
	StoreOutcomeClean StoreOutcome = "clean"
	// StoreOutcomeRedactorFailed means the redactor errored, the body was not
	// valid UTF-8, or the policy was unknown. The result is the metadata row's
	// empty body, never the source.
	StoreOutcomeRedactorFailed StoreOutcome = "redactor_failed"
)

// MaxGovernedBodyBytes bounds the body a redact policy hands the redactor. The
// redactor is linear but not cheap on adversarial input (a hostile 50 MB body
// measured 4 to 14 s), it runs twice per collection, and governed files are
// small configuration files. A larger body becomes a metadata row with reason
// FailureTooLarge, never an unbounded run and never the raw body.
const MaxGovernedBodyBytes = 4 << 20

// FailureReason is the bounded class of a redactor failure. It never carries
// error text, so it is safe in a log line and a metric label.
type FailureReason string

const (
	// FailureInvalidUTF8 means the body was not valid UTF-8.
	FailureInvalidUTF8 FailureReason = "invalid_utf8"
	// FailureNotText means the body held a NUL byte (BOM-less UTF-16 or UTF-32).
	FailureNotText FailureReason = "not_text"
	// FailureTooLarge means the body exceeded MaxGovernedBodyBytes.
	FailureTooLarge FailureReason = "too_large"
	// FailureRedactorError means the redactor returned an unclassified error.
	FailureRedactorError FailureReason = "redactor_error"
	// FailureRedactorPanic means the redactor panicked.
	FailureRedactorPanic FailureReason = "redactor_panic"
	// FailureUnknownPolicy means the definition named a policy the collector
	// does not know.
	FailureUnknownPolicy FailureReason = "unknown_policy"
	// FailureUnknownMode means the definition named an unknown redactor mode.
	FailureUnknownMode FailureReason = "unknown_mode"
)

// StoreResult is the body the content store may hold for one file.
type StoreResult struct {
	// Body is what the content fact, the content_files row, and every entity
	// source_cache slice may carry. It is empty for a metadata row.
	Body string
	// Outcome says which path produced Body.
	Outcome StoreOutcome
	// Replaced counts the credential values a redact policy replaced.
	Replaced int
	// Err is set only with StoreOutcomeRedactorFailed. It may carry text from a
	// recovered panic, so callers log Reason, not Err.
	Err error
	// Reason is the bounded failure class, set only with
	// StoreOutcomeRedactorFailed.
	Reason FailureReason
}

// redactFunc is one redactor mode's implementation.
type redactFunc func(body string, credentialKeys []string) (redacted string, replaced int, err error)

// ApplyContentStore applies one definition's content-store policy to a source
// body at the collector write boundary, before any fact or row exists. The
// caller keeps computing the content digest from the SOURCE bytes, so an
// unchanged file keeps its freshness hint under either policy and the registry
// fingerprint, not the digest, invalidates on a policy change.
//
// Every failure path returns an empty body: there is no raw-body fallback.
func ApplyContentStore(store parser.ContentStore, body string) StoreResult {
	return applyContentStore(store, body, redactMarker)
}

func applyContentStore(store parser.ContentStore, body string, marker redactFunc) StoreResult {
	switch store.Policy {
	case "", parser.ContentStoreStore:
		return StoreResult{Body: body, Outcome: StoreOutcomeStored}
	case parser.ContentStoreMetadata:
		return StoreResult{Outcome: StoreOutcomeMetadata}
	case parser.ContentStoreRedact:
		switch store.Redactor {
		case parser.RedactorMarker:
			return redactResult(marker, body, store.CredentialKeys)
		default:
			return StoreResult{
				Outcome: StoreOutcomeRedactorFailed,
				Err:     fmt.Errorf("unknown redactor mode %q", store.Redactor),
				Reason:  FailureUnknownMode,
			}
		}
	default:
		return StoreResult{
			Outcome: StoreOutcomeRedactorFailed,
			Err:     fmt.Errorf("unknown content store policy %q", store.Policy),
			Reason:  FailureUnknownPolicy,
		}
	}
}

func redactResult(redact redactFunc, body string, keys []string) (result StoreResult) {
	// A redactor panic is a redactor failure: the file must become a metadata
	// row, not take the collector down or leak the source through a recovered
	// partial result.
	defer func() {
		if r := recover(); r != nil {
			result = StoreResult{Outcome: StoreOutcomeRedactorFailed, Err: fmt.Errorf("redactor panic: %v", r), Reason: FailureRedactorPanic}
		}
	}()
	redacted, replaced, err := redact(body, keys)
	if err != nil {
		return StoreResult{Outcome: StoreOutcomeRedactorFailed, Err: err, Reason: failureReason(err)}
	}
	outcome := StoreOutcomeClean
	if replaced > 0 {
		outcome = StoreOutcomeRedacted
	}
	return StoreResult{Body: redacted, Outcome: outcome, Replaced: replaced}
}

func failureReason(err error) FailureReason {
	switch {
	case errors.Is(err, errInvalidUTF8):
		return FailureInvalidUTF8
	case errors.Is(err, errNotText):
		return FailureNotText
	case errors.Is(err, errTooLarge):
		return FailureTooLarge
	default:
		return FailureRedactorError
	}
}
