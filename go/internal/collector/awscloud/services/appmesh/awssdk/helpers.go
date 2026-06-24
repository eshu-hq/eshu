// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"time"

	awsappmesh "github.com/aws/aws-sdk-go-v2/service/appmesh"

	appmeshservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/appmesh"
)

// timeOrZero dereferences an AWS *time.Time into a UTC time, returning the zero
// time when the pointer is nil so callers can normalize absent timestamps.
func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return value.UTC()
}

// Compile-time assertions.
var (
	_ appmeshservice.Client = (*Client)(nil)
	_ apiClient             = (*awsappmesh.Client)(nil)
)
