// Package awssdk adapts AWS SDK for Go v2 CloudFront APIs into the
// metadata-only cloudfront scanner port.
//
// Client pages distribution summaries and reads tags for ARN-addressable
// distributions. The adapter maps only safe control-plane fields and drops
// origin custom header values before data reaches the scanner-owned model.
// Callers receive errors from AWS pagination and tag reads with the original
// cause preserved.
package awssdk
