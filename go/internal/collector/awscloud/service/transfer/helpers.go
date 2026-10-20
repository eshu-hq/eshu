// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package transfer

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// isARN reports whether value is an ARN, matching the isARN helpers the other
// scanner packages use to gate ARN-keyed relationship emission.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// partition returns the AWS partition for the scan boundary's region — aws,
// aws-cn, or aws-us-gov. Transfer servers and users carry ARNs from the API,
// but home-directory S3 buckets and EFS file systems are reported as bare
// paths, so the boundary region is the partition source for the synthesized
// bucket and file-system ARNs. Hardcoding the commercial partition would dangle
// the user->S3-bucket and user->EFS-file-system edges in GovCloud and China.
func partition(boundary awscloud.Boundary) string {
	region := strings.TrimSpace(boundary.Region)
	switch {
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	default:
		return "aws"
	}
}

// firstPathSegment returns the first non-empty `/`-delimited segment of path
// and the remaining suffix after it. It returns ok=false when path has no
// leading segment. The path is treated as a POSIX-style home directory such as
// `/bucket/key/prefix` or `/fs-0123/path`.
func firstPathSegment(path string) (segment string, remainder string, ok bool) {
	trimmed := strings.TrimSpace(path)
	trimmed = strings.TrimPrefix(trimmed, "/")
	if trimmed == "" {
		return "", "", false
	}
	slash := strings.IndexByte(trimmed, '/')
	if slash < 0 {
		return trimmed, "", true
	}
	segment = strings.TrimSpace(trimmed[:slash])
	if segment == "" {
		return "", "", false
	}
	remainder = strings.TrimSpace(trimmed[slash+1:])
	return segment, remainder, true
}

// looksLikeEFSFileSystemID reports whether segment is an EFS file system ID
// (fs-...). AWS Transfer EFS-domain home directories are keyed by the EFS file
// system ID as the leading path segment, so the scanner classifies the backing
// store by this prefix when the server domain is not authoritative.
func looksLikeEFSFileSystemID(segment string) bool {
	// EFS file system IDs are fs- followed by 8 (legacy) or 17 hex characters.
	// An S3 bucket name can legally start with "fs-", so a bare prefix check
	// would misclassify an S3 home directory as EFS; require the hex shape.
	rest, ok := strings.CutPrefix(strings.TrimSpace(segment), "fs-")
	if !ok || len(rest) < 8 {
		return false
	}
	for _, r := range rest {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')
		if !isHex {
			return false
		}
	}
	return true
}

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
