// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsshieldtypes "github.com/aws/aws-sdk-go-v2/service/shield/types"

	shieldservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/shield"
)

// mapProtection converts an AWS SDK Shield Protection into the scanner-owned
// Protection. Only identity and the protected resource ARN survive; the Route 53
// health-check ids and automatic-response configuration are intentionally
// dropped because they are operational detail, not graph identity.
func mapProtection(protection awsshieldtypes.Protection) shieldservice.Protection {
	return shieldservice.Protection{
		ARN:         strings.TrimSpace(aws.ToString(protection.ProtectionArn)),
		ID:          strings.TrimSpace(aws.ToString(protection.Id)),
		Name:        strings.TrimSpace(aws.ToString(protection.Name)),
		ResourceARN: strings.TrimSpace(aws.ToString(protection.ResourceArn)),
	}
}

// mapSubscription converts an AWS SDK Shield Subscription into the scanner-owned
// Subscription, carrying the ARN, the supplied state, and the auto-renew flag
// only. Subscription limits, time commitment, start/end time, and proactive
// engagement status are intentionally dropped as billing/operational detail.
func mapSubscription(subscription *awsshieldtypes.Subscription, state string) *shieldservice.Subscription {
	if subscription == nil {
		return nil
	}
	return &shieldservice.Subscription{
		ARN:       strings.TrimSpace(aws.ToString(subscription.SubscriptionArn)),
		State:     strings.TrimSpace(state),
		AutoRenew: strings.TrimSpace(string(subscription.AutoRenew)),
	}
}
