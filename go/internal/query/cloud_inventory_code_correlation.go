// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// Cloud-inventory deployment-code correlation limitation (issue #5454).
//
// A zip-packaged AWS Lambda function surfaces a code_sha256 attribute that is
// base64(SHA256(the exact deployment .zip)) computed by AWS over the uploaded
// package bytes. Eshu collects no hash that covers those bytes: the GitHub
// Actions artifact_digest hashes GitHub's OWN re-zipped archive (and is
// consumed as a container-image digest), package-registry hashes are of
// published tarballs/wheels/modules, and OCI image digests are of container
// manifests. None is byte-equal to a Lambda deployment zip, so a real hash join
// is infeasible in principle and no join site exists.
//
// The readback must not present this silently. For a zip-packaged Lambda that
// carries code_sha256, the readback attaches a bounded, content-free label that
// states the code hash is display-only evidence and is not correlated to any
// CI/package hash, using the established truth-label vocabulary
// (status/truth_basis/unsupported_reason). An image-packaged Lambda is
// deliberately excluded: its deployment code is the container image, which
// #5450 correlates to the OCI ContainerImage via image_uri/resolved_image_uri.
const (
	// cloudInventoryCodeCorrelationKey is the wire field carrying the bounded
	// deployment-code correlation limitation on a readback resource row.
	cloudInventoryCodeCorrelationKey = "code_sha256_correlation"
	// cloudInventoryZipCodeSHA256UnsupportedReason is the bounded, low-cardinality
	// reason token for the zip-Lambda code-hash gap. It names the exact
	// limitation: the zip code_sha256 has no collected CI/package/OCI counterpart
	// to join against.
	cloudInventoryZipCodeSHA256UnsupportedReason = "zip_code_sha256_no_ci_counterpart"
	// cloudInventoryCodeCorrelationStatusUncorrelated is the bounded status the
	// label reports for a code hash Eshu surfaces but does not correlate.
	cloudInventoryCodeCorrelationStatusUncorrelated = "uncorrelated"
	// cloudInventoryCodeCorrelationTruthBasisDisplayOnly is the bounded
	// truth_basis: the code_sha256 is surfaced as display-only evidence, not as a
	// correlation-backed join key.
	cloudInventoryCodeCorrelationTruthBasisDisplayOnly = "display_only_evidence"
)

// cloudInventoryLambdaPackageTypeZip is the AWS PackageType value for a
// zip-packaged Lambda function (as opposed to "Image"). It is the Lambda-only
// signal the label keys on: only the Lambda scanner emits a package_type
// attribute, and only its "Zip" value has an uncorrelatable code_sha256 (an
// "Image" Lambda's deployment code is the container image #5450 correlates).
const cloudInventoryLambdaPackageTypeZip = "Zip"

// cloudInventoryCodeCorrelationLabel returns the bounded deployment-code
// correlation limitation for a resource's already-projected attributes, or nil
// when no limitation applies. It fires only for a zip-packaged Lambda function
// (attributes.package_type == "Zip") that carries a non-blank code_sha256 --
// the exact case where a caller would otherwise expect the code hash to be
// correlated but no collected hash covers the Lambda deployment zip's bytes.
//
// It keys on package_type rather than resource_type because package_type is a
// Lambda-only attribute (no other AWS resource emits it) and its value cleanly
// separates the uncorrelatable zip case from the image case, independent of
// whether the resource_type is the cassette short form ("lambda.function") or
// the live collector's canonical string ("aws_lambda_function"). The returned
// map is content-free: it carries only bounded enum/label tokens, never the
// code_sha256 value or any locator.
func cloudInventoryCodeCorrelationLabel(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return nil
	}
	packageType, _ := attrs["package_type"].(string)
	if !strings.EqualFold(strings.TrimSpace(packageType), cloudInventoryLambdaPackageTypeZip) {
		return nil
	}
	codeSHA256, _ := attrs["code_sha256"].(string)
	if strings.TrimSpace(codeSHA256) == "" {
		return nil
	}
	return map[string]any{
		"status":             cloudInventoryCodeCorrelationStatusUncorrelated,
		"truth_basis":        cloudInventoryCodeCorrelationTruthBasisDisplayOnly,
		"unsupported_reason": cloudInventoryZipCodeSHA256UnsupportedReason,
	}
}
