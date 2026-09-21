// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"errors"
	"strings"

	awsquicksighttypes "github.com/aws/aws-sdk-go-v2/service/quicksight/types"
	"github.com/aws/smithy-go"
)

// isThrottleError reports whether err is an AWS throttle response so the shared
// telemetry can record it on the throttle counter without the adapter retrying.
func isThrottleError(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	code := apiErr.ErrorCode()
	return strings.Contains(strings.ToLower(code), "throttl") ||
		code == "RequestLimitExceeded" ||
		code == "TooManyRequestsException"
}

// isNotSubscribed reports whether err is QuickSight's account-scoped "not signed
// up" response. QuickSight returns ResourceNotFoundException or
// AccessDeniedException whose message reports that the account is not subscribed
// for QuickSight. Matching the specific "not signed up"/"not subscribed" phrase
// is required because the error codes are shared with genuine missing-resource
// and IAM authorization failures, which must be surfaced rather than masked as a
// clean empty scan.
func isNotSubscribed(err error) bool {
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	if !isAccessDenied(err) && !isResourceNotFound(err) {
		return false
	}
	message := strings.ToLower(apiErr.ErrorMessage())
	return strings.Contains(message, "not signed up") ||
		strings.Contains(message, "not subscribed")
}

// isAccessDenied reports whether err is a QuickSight AccessDeniedException. A
// single denied tag or describe read degrades to summary-only metadata; a denied
// first list call is only treated as empty when it is also the not-subscribed
// case, so genuine authorization failures still fail the scan.
func isAccessDenied(err error) bool {
	var apiErr *awsquicksighttypes.AccessDeniedException
	return errors.As(err, &apiErr)
}

// isResourceNotFound reports whether err is a QuickSight ResourceNotFoundException,
// returned both for a removed resource and for a not-subscribed account.
func isResourceNotFound(err error) bool {
	var apiErr *awsquicksighttypes.ResourceNotFoundException
	return errors.As(err, &apiErr)
}

// errorClass returns the AWS error code for warning telemetry, or a generic
// "error" label when err is not an AWS API error.
func errorClass(err error) string {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode()
	}
	return "error"
}
