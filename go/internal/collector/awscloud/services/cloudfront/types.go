package cloudfront

import (
	"context"
	"time"
)

// Client lists CloudFront distribution metadata for one claimed account.
type Client interface {
	ListDistributions(context.Context) ([]Distribution, error)
}

// Distribution is the metadata-only scanner view of a CloudFront distribution.
type Distribution struct {
	ARN                  string
	ID                   string
	DomainName           string
	Status               string
	Enabled              bool
	Comment              string
	HTTPVersion          string
	IPV6Enabled          bool
	LastModifiedTime     time.Time
	PriceClass           string
	Staging              bool
	WebACLID             string
	Aliases              []string
	Origins              []Origin
	DefaultCacheBehavior CacheBehavior
	CacheBehaviors       []CacheBehavior
	ViewerCertificate    ViewerCertificate
	Tags                 map[string]string
}

// Origin is the metadata-only scanner view of a CloudFront origin. Custom
// header values are intentionally excluded because they can contain secrets.
type Origin struct {
	ID                    string
	DomainName            string
	OriginPath            string
	OriginAccessControlID string
	CustomHeaderNames     []string
}

// CacheBehavior is the metadata-only scanner view of CloudFront request
// routing and policy selectors.
type CacheBehavior struct {
	PathPattern             string
	TargetOriginID          string
	ViewerProtocolPolicy    string
	AllowedMethods          []string
	CachedMethods           []string
	CachePolicyID           string
	OriginRequestPolicyID   string
	ResponseHeadersPolicyID string
	Compress                bool
}

// ViewerCertificate captures CloudFront viewer TLS certificate selectors
// without certificate bodies or private material.
type ViewerCertificate struct {
	ACMCertificateARN            string
	CloudFrontDefaultCertificate bool
	IAMCertificateID             string
	MinimumProtocolVersion       string
	SSLSupportMethod             string
}
