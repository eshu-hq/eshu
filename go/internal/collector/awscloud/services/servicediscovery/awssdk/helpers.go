// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"time"

	awssd "github.com/aws/aws-sdk-go-v2/service/servicediscovery"

	sdservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/servicediscovery"
)

// timeOrZero dereferences an AWS *time.Time into a UTC time, returning the zero
// time when the pointer is nil so callers can normalize absent timestamps.
func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

// Compile-time assertions: the adapter satisfies the scanner Client contract,
// and the production AWS SDK Cloud Map client satisfies the read-only apiClient
// surface the adapter consumes.
var (
	_ sdservice.Client = (*Client)(nil)
	_ apiClient        = (*awssd.Client)(nil)
)
