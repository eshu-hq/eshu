// Package cloudfront converts Amazon CloudFront distribution metadata into AWS
// resource and relationship facts.
//
// Scanner accepts only the CloudFront service boundary and emits
// metadata-only facts for distributions. The package reports direct ACM
// certificate and WAF web ACL selectors as source evidence, while keeping
// object contents, origin payloads, policy documents, certificate bodies,
// private keys, origin custom header values, and mutation APIs outside the
// contract described by the AWS cloud scanner ADR.
package cloudfront
