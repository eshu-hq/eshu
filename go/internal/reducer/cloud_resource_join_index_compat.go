// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/cloudjoin"
)

// This file is the reducer root's compatibility surface for the AWS
// CloudResource join index, which moved to [cloudjoin] (issue #6061) so the
// iamcan family can build and read it without importing the root. Root call
// sites keep their current spelling.

// cloudResourceJoinIndex is the root spelling of
// [cloudjoin.CloudResourceJoinIndex].
type cloudResourceJoinIndex = cloudjoin.CloudResourceJoinIndex

// buildCloudResourceJoinIndex forwards to
// [cloudjoin.BuildCloudResourceJoinIndex].
func buildCloudResourceJoinIndex(envelopes []facts.Envelope) (cloudResourceJoinIndex, []quarantinedFact, error) {
	return cloudjoin.BuildCloudResourceJoinIndex(envelopes)
}

// cloudResourceUID forwards to [cloudjoin.CloudResourceUID].
func cloudResourceUID(accountID, region, resourceType, resourceID string) string {
	return cloudjoin.CloudResourceUID(accountID, region, resourceType, resourceID)
}
