// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package amplify

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// cloudFrontDomainSuffix is the public DNS suffix of every CloudFront
// distribution domain. Amplify subdomain DNS records that point at CloudFront
// end in this suffix, so the app->CloudFront edge only fires when the record
// resolves to a *.cloudfront.net host.
const cloudFrontDomainSuffix = ".cloudfront.net"

// appARN builds the canonical Amplify app ARN. ListApps already returns the
// ARN, so this is only the synthesis fallback for the defensive case where the
// ARN is absent. The partition is derived from the scan boundary, never
// hardcoded, so a GovCloud or China app id resolves to an ARN in its own
// partition instead of dangling the app node and its outgoing edges.
func appARN(boundary awscloud.Boundary, appID string) string {
	appID = strings.TrimSpace(appID)
	if appID == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:amplify:%s:%s:apps/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, appID)
}

// branchARN builds the canonical Amplify branch ARN. ListBranches already
// returns the ARN, so this is only the synthesis fallback. The partition is
// derived from the scan boundary, never hardcoded.
func branchARN(boundary awscloud.Boundary, appID, branchName string) string {
	appID = strings.TrimSpace(appID)
	branchName = strings.TrimSpace(branchName)
	if appID == "" || branchName == "" {
		return ""
	}
	return fmt.Sprintf("arn:%s:amplify:%s:%s:apps/%s/branches/%s",
		awscloud.PartitionForBoundary(boundary), boundary.Region, boundary.AccountID, appID, branchName)
}

// appResourceID returns the identity an app node publishes and every one of the
// app's own outgoing edges sources from. It prefers the API-reported ARN and
// falls back to the partition-aware synthesized ARN, then the bare app id.
func appResourceID(boundary awscloud.Boundary, app App) string {
	return firstNonEmpty(app.ARN, appARN(boundary, app.ID), app.ID)
}

// branchResourceID returns the identity a branch node publishes. It prefers the
// API-reported ARN and falls back to the partition-aware synthesized ARN, then
// a stable app#branch composite.
func branchResourceID(boundary awscloud.Boundary, branch Branch) string {
	synthesized := branchARN(boundary, branch.AppID, branch.Name)
	composite := ""
	if appID := strings.TrimSpace(branch.AppID); appID != "" && strings.TrimSpace(branch.Name) != "" {
		composite = appID + "#branch#" + strings.TrimSpace(branch.Name)
	}
	return firstNonEmpty(branch.ARN, synthesized, composite, branch.Name)
}

// SanitizeRepositoryURL reduces an Amplify repository URL to scheme, host, and
// path only, dropping any embedded userinfo (a token-bearing
// https://x-access-token:TOKEN@github.com/... form) and any query or fragment.
// A token must never reach a fact payload or a graph join key. For non-URL
// values that do not parse as an absolute URL (scp-style
// git@github.com:org/repo.git) it strips any leading "<user>@" segment, so
// "git@github.com:org/repo.git" becomes "github.com:org/repo.git", dropping a
// user component that could carry a token while preserving the host and path.
// Values without an "@" are returned trimmed and otherwise unchanged. It is
// exported so the SDK adapter strips the token at the boundary, before the raw
// URL reaches a scanner-owned record.
func SanitizeRepositoryURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		// Not a parseable absolute URL (for example an scp-style git address).
		// Strip any userinfo@ segment defensively without inventing structure.
		if at := strings.LastIndex(raw, "@"); at >= 0 && !strings.Contains(raw, "://") {
			return strings.TrimSpace(raw[at+1:])
		}
		return raw
	}
	rebuilt := &url.URL{
		Scheme: parsed.Scheme,
		Host:   parsed.Host,
		Path:   parsed.Path,
	}
	return rebuilt.String()
}

// cloudFrontDomainFromDNSRecord extracts the CloudFront distribution domain
// (the *.cloudfront.net host) from an Amplify subdomain DNS record. The record
// is published as a CNAME, sometimes with a leading "CNAME " type token or a
// trailing dot. It returns the empty string when the record does not resolve to
// a CloudFront host so the app->CloudFront edge never dangles against a non-edge
// target.
func cloudFrontDomainFromDNSRecord(record string) string {
	record = strings.TrimSpace(record)
	if record == "" {
		return ""
	}
	// Amplify formats the record as "<type> <value>" (for example
	// "CNAME d123.cloudfront.net"); take the last whitespace-separated field.
	if fields := strings.Fields(record); len(fields) > 0 {
		record = fields[len(fields)-1]
	}
	record = strings.TrimSuffix(strings.ToLower(record), ".")
	if strings.HasSuffix(record, cloudFrontDomainSuffix) {
		return record
	}
	return ""
}

// normalizedDomain lowercases and trims a trailing dot from a domain name so an
// app->Route53 edge joins the hosted-zone node by its normalized_name
// correlation anchor, which the route53 scanner publishes the same way.
func normalizedDomain(domain string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
