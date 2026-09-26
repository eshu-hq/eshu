// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

// deadCodeSuppressedBucketLimit is the ceiling on the suppressed (modeled-root)
// bucket of both dead-code scans. The bucket actually returned is bounded by
// min(request limit, this ceiling) -- see suppressedBucketLimit.
const deadCodeSuppressedBucketLimit = 50

// suppressedBucketLimit returns how many suppressed rows a response may carry
// for a request `limit`: the smaller of the limit and the fixed ceiling. The
// suppressed bucket used to hold up to the ceiling however small the limit
// was, which let `limit=1` return 24 suppressed rows and made the bucket the
// part of the payload no caller-supplied bound could shrink (#7168). A
// nonpositive limit falls back to the ceiling; the handlers normalize it
// before scanning, so that arm only guards direct callers of the scan.
func suppressedBucketLimit(limit int) int {
	if limit <= 0 || limit > deadCodeSuppressedBucketLimit {
		return deadCodeSuppressedBucketLimit
	}
	return limit
}

// appendBoundedSuppressed appends results to bucket until it holds bound
// rows. It reports true when a result was dropped, so the caller can say the
// bucket was cut instead of presenting a bounded sample as the whole set.
func appendBoundedSuppressed(bucket, results []map[string]any, bound int) ([]map[string]any, bool) {
	for _, result := range results {
		if len(bucket) >= bound {
			return bucket, true
		}
		bucket = append(bucket, result)
	}
	return bucket, false
}

func (scan *DeadCodeInvestigationScan) addSuppressed(results []map[string]any) {
	var truncated bool
	scan.Suppressed, truncated = appendBoundedSuppressed(scan.Suppressed, results, scan.SuppressedLimit)
	if truncated {
		scan.SuppressedTruncated = true
	}
}

// addSuppressed appends suppressed results to the cross-repo scan under the
// same bound the investigation scan uses. Before #7168 this path appended
// every suppressed row with no cap at all.
func (scan *CrossRepoDeadCodeScan) addSuppressed(results []map[string]any) {
	var truncated bool
	scan.Suppressed, truncated = appendBoundedSuppressed(scan.Suppressed, results, scan.SuppressedLimit)
	if truncated {
		scan.SuppressedTruncated = true
	}
}
