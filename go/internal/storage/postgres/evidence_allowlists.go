// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

// awsCloudInventoryAttributeAllowlist is the closed image/version allowlist
// for aws_resource attributes (issues #5449 and #9298). It surfaces AWS
// runtime and patch-level fields the collector observes -- an ECS task's
// running container image and digest plus its owning task definition; a
// Lambda function's image URI/digest, code version, code_sha256, package_type,
// and runtime; EKS platform, nodegroup release and add-on versions; and EC2
// instance AMI id -- while dropping every other AWS attribute key (cluster_arn,
// role_arn, kms_key_arn, network_interfaces, environment, vpc_config, and any
// key not named here). package_type is the bounded Lambda packaging
// discriminator ("Zip"/"Image") the readback uses to attach the #5454
// deployment-code correlation limitation to a zip-packaged Lambda whose
// code_sha256 has no collected CI/package/OCI counterpart; it is a
// low-cardinality enum, never a locator or secret.
var awsCloudInventoryAttributeAllowlist = cloudInventoryAttributeAllowlist{
	scalarKeys: map[string]struct{}{
		"task_definition_arn": {},
		"image_uri":           {},
		"resolved_image_uri":  {},
		"code_sha256":         {},
		"package_type":        {},
		"version":             {},
		"platform_version":    {},
		"release_version":     {},
		"addon_version":       {},
		"ami_id":              {},
		"runtime":             {},
	},
	nestedArrayKeys: map[string]map[string]struct{}{
		// containers' sub-key set is intentionally maintained independently of
		// cloudInventoryContainerAttributeKeys in
		// go/internal/query/cloud_inventory_read_model.go, which applies the same
		// {image, image_digest} sub-key set as a second, independent gate on the
		// already-filtered value. There is no shared constant and no test tying
		// the two together across packages: this loader is the sole upstream of
		// that projector's input (every containers value it ever sees already
		// passed through this filter), so drift between the two sets can only
		// ever make the read model MORE restrictive than this loader -- i.e.
		// silently drop container data the loader already allowed -- never leak
		// a raw sub-key this loader dropped. The query package pins its own set
		// with TestCloudInventoryContainerAttributeKeysIsImageAndDigestOnly so a
		// change there is caught by a test even without a cross-package tie.
		"containers": {
			"image":        {},
			"image_digest": {},
		},
	},
}

// azureCloudInventoryAttributeAllowlist is the closed image/version allowlist
// for azure_cloud_resource attributes (issue #5449). It is intentionally EMPTY
// today: the azure_cloud_resource fact's attributes map carries only identity
// and boundary fields (arm_resource_id, subscription_id, resource_group,
// tenant_id, tags, the redacted "extension" object, ...), never an image or
// version field -- Azure's runtime image evidence is emitted as a separate
// azure_image_reference fact kind that this admission mapping does not
// consume. The allowlist mechanism is wired now so Azure image/version keys
// can be added the moment a source fact carries them, without touching the
// filter path again; see TestCloudInventoryRecordFromRowAzureAttributesAlwaysDropped
// for the regression guard that every current Azure key stays dropped.
var azureCloudInventoryAttributeAllowlist = cloudInventoryAttributeAllowlist{}

const awsCloudInventoryAMIResourceType = "aws_ec2_ami"

// awsCloudInventoryAMIAttributeAllowlist admits the AMI name ONLY for the
// aws_ec2_ami resource type (hybrid ruling on issue #9525, decision 7b). The
// name is the single validatedScalarKeys entry: a JSON string that passes the
// AWS CreateImage structural validator below. description is ALWAYS dropped,
// even from a rogue producer payload, because no allowlist names it: the
// closed filter keeps only named keys. This entry is deliberately narrow: it
// inherits nothing from the base AWS image/version allowlist, and no other
// resource type admits name.
var awsCloudInventoryAMIAttributeAllowlist = cloudInventoryAttributeAllowlist{
	validatedScalarKeys: map[string]func(string) (string, bool){
		"name": cloudInventoryAMIName,
	},
}

const awsCloudInventoryAppSyncGraphQLAPIResourceType = "aws_appsync_graphql_api"

// awsCloudInventoryAppSyncGraphQLAPIAttributeAllowlist exposes only the
// non-secret count and lifecycle timestamps from an AppSync GraphQL API.
// API key Id values and descriptions never enter cloud inventory readback.
var awsCloudInventoryAppSyncGraphQLAPIAttributeAllowlist = cloudInventoryAttributeAllowlist{
	scalarKeys: map[string]struct{}{
		"api_key_count": {},
	},
	nestedArrayKeys: map[string]map[string]struct{}{
		"api_key_lifecycles": {
			"expires": {},
			"deletes": {},
		},
	},
}

// cloudInventoryAMINameDropSentinel is the exact synthetic value the
// allowlist tests use for must-drop fields (issue #9450). It stays dropped as
// an AMI name value even though it is charset-valid: exact-sentinel
// preservation, not secret detection. A bare patternless token that is NOT
// this sentinel still passes when it fits the charset (see the honesty clause
// below); blocklisting one exact test string is deterministic, unlike judging
// what a value looks like.
const cloudInventoryAMINameDropSentinel = "must-not-surface"

// cloudInventoryAMINameMinRunes and cloudInventoryAMINameMaxRunes are the AWS
// CreateImage Name bounds (3-128 characters), derived from the API contract,
// not invented. Any failure below drops the field only and keeps ami_id and
// every typed field; the name is never truncated, because a truncated name is
// a name AWS never reported.
const (
	cloudInventoryAMINameMinRunes = 3
	cloudInventoryAMINameMaxRunes = 128
)

// cloudInventoryAMIName admits one AMI name value under the hybrid ruling on
// issue #9525 (conditional admission for name, permanent exclusion for
// description). ALL of these must hold, fail-closed:
//
//  1. The value is already a JSON string (enforced by filter before this
//     runs: non-strings never reach here and are never stringified).
//  2. The trimmed text holds 3-128 runes, the AWS CreateImage maximum.
//     Surrounding whitespace is trimmed before validation, so the trimmed
//     value is what is admitted and surfaced, never the padded original.
//  3. Every rune is printable with no controls, and inside the exact AWS
//     CreateImage charset: alphanumerics plus ()[] ./-'@_. Hence no "=",
//     no ":", no URL or header shape, and no percent-encoding.
//  4. The shared free-text hygiene walk (urlredact.FreeText over the shared
//     sensitive-key predicate) reports nothing to remove. This is a
//     defense-in-depth drop trigger, not a second deciding gate: the charset
//     already rejects every key=value and key:value shape (FreeText
//     fast-paths out when none of =:% is present, so it cannot fire on a
//     charset-passing input today), and a patternless bare token passes here
//     by design. The call stays in the drop path so a future charset widening
//     still trips on credential-shaped pairs instead of silently admitting
//     them; key=value coverage is proved through the charset gate.
//
// What is claimed and tested is only the falsifiable part (shape, charset,
// drop behavior). The admitted name is untrusted creator-controlled text and
// is explicitly NOT proven secret-free: a bare patternless token fits the
// charset and will pass. That limitation is printed in the contract docs, not
// hidden (ruling decision 6, disclaimer condition).
func cloudInventoryAMIName(value string) (string, bool) {
	s := strings.TrimSpace(value)
	if s == "" || s == cloudInventoryAMINameDropSentinel {
		return "", false
	}
	if n := utf8.RuneCountInString(s); n < cloudInventoryAMINameMinRunes || n > cloudInventoryAMINameMaxRunes {
		return "", false
	}
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.IsControl(r) || !cloudInventoryAMINameRuneAllowed(r) {
			return "", false
		}
	}
	if _, hit := urlredact.FreeText(s, nil); hit {
		return "", false
	}
	return s, true
}

// cloudInventoryAMINameRuneAllowed reports whether r is inside the exact AWS
// CreateImage Name charset: ASCII alphanumerics plus ()[] ./-'@_ and space.
// The printable/no-control gate in cloudInventoryAMIName stays separate so it
// survives any future charset widening.
func cloudInventoryAMINameRuneAllowed(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	switch r {
	case '(', ')', '[', ']', ' ', '.', '/', '-', '\'', '@', '_':
		return true
	}
	return false
}
