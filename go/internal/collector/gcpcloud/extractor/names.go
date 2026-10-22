// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extractor

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Cross-group resource-name builders shared by extractors in several service
// groups. The CAI asset types, resource-name prefixes, sentinels, and markers
// they build with live in the catalog leaf (#6696, slice L) so the root,
// every extractor file, and the runtime subpackages share one definition.
//
// Helpers here decode only resource identities (bucket names, topic names,
// hosts, key names). They never return data-plane locators such as object
// paths inside source URIs or request bodies.

// CloudFunctionProjectLocation extracts the project id and location from a Cloud
// Functions CAI full resource name
// (//cloudfunctions.googleapis.com/projects/<project>/locations/<location>/functions/<name>).
// It returns empty strings when either segment is absent.
func CloudFunctionProjectLocation(functionFullName string) (project, location string) {
	segments := strings.Split(strings.TrimSpace(functionFullName), "/")
	for i := 0; i+1 < len(segments); i++ {
		switch segments[i] {
		case "projects":
			project = segments[i+1]
		case "locations":
			location = segments[i+1]
		}
	}
	return project, location
}

// EventarcProjectLocation extracts the project id and location from a CAI full
// resource name by scanning for the projects/<id> and locations/<id> segments.
// It is domain-agnostic so it does not depend on any other asset type's parser.
func EventarcProjectLocation(fullName string) (project, location string) {
	segments := strings.Split(strings.TrimSpace(fullName), "/")
	for i := 0; i+1 < len(segments); i++ {
		switch segments[i] {
		case "projects":
			project = segments[i+1]
		case "locations":
			location = segments[i+1]
		}
	}
	return project, location
}

// SecretManagerTopicFullName builds the CAI Pub/Sub Topic full resource name
// from a Secret Manager topic reference (projects/.../topics/...). An
// already-normalized CAI full resource name (//pubsub.googleapis.com/...) is
// returned unchanged so the prefix is never doubled. It returns "" for a blank
// reference or one that does not name a topic so the caller emits no topic
// edge.
func SecretManagerTopicFullName(topicRef string) string {
	trimmed := strings.TrimSpace(topicRef)
	if trimmed == "" || !strings.Contains(trimmed, "/topics/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return catalog.PubSubResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// PubSubTopicRefFullName builds the CAI Pub/Sub Topic full resource name from
// a topic reference (projects/.../topics/...). An already-normalized CAI full
// resource name is returned unchanged. It returns "" for a blank reference,
// the deleted-topic sentinel, or a reference that does not name a topic.
func PubSubTopicRefFullName(topicRef string) string {
	trimmed := strings.TrimSpace(topicRef)
	if trimmed == "" || trimmed == catalog.PubSubDeletedTopicSentinel || !strings.Contains(trimmed, "/topics/") {
		return ""
	}
	if strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	return catalog.PubSubResourceNamePrefix + strings.TrimPrefix(trimmed, "/")
}

// PubSubPushEndpointHostFingerprint returns a stable, case-normalized digest of
// a push endpoint host so subscriptions sharing an endpoint can be correlated
// without persisting the raw DNS name. A blank host fingerprints to "".
func PubSubPushEndpointHostFingerprint(host string) string {
	normalized := strings.ToLower(strings.TrimSpace(host))
	if normalized == "" {
		return ""
	}
	return "sha256:" + facts.StableID("GCPPubSubPushEndpointHost", map[string]any{"host": normalized})
}

// GCSBucketFromURI extracts only the bucket name from a gs://bucket/object
// URI. The object path is never returned so no data-plane locator leaves the
// parser.
func GCSBucketFromURI(uri string) string {
	trimmed := strings.TrimSpace(uri)
	rest, ok := strings.CutPrefix(trimmed, "gs://")
	if !ok {
		return ""
	}
	bucket, _, _ := strings.Cut(rest, "/")
	return strings.TrimSpace(bucket)
}

// GitRemoteHost extracts the lowercase host from a git remote reference,
// supporting both URL forms (https://, ssh://, git://) and the SCP-like syntax
// git remotes commonly use ([user@]host.xz:path/to/repo.git). It returns "" when
// no host can be determined. The path and any embedded credentials are dropped.
func GitRemoteHost(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}
	// SCP-like syntax has no scheme and a ":" that precedes the first "/".
	if !strings.Contains(trimmed, "://") {
		if colon := strings.Index(trimmed, ":"); colon >= 0 {
			if slash := strings.Index(trimmed, "/"); slash == -1 || colon < slash {
				hostPart := trimmed[:colon]
				if at := strings.LastIndex(hostPart, "@"); at >= 0 {
					hostPart = hostPart[at+1:]
				}
				return strings.ToLower(strings.TrimSpace(hostPart))
			}
		}
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

// ComputeFullResourceNameFromSelfLink builds a CAI full resource name from a
// Compute Engine reference expressed as a full selfLink
// (https://.../compute/<version>/projects/...), a project-qualified partial
// (projects/p/...), or a project-less partial (global/networks/n,
// regions/r/subnetworks/s); project-less partials resolve against
// sourceProjectID (the source network's project). A value already in CAI
// full-resource-name form is returned unchanged. An empty string is returned
// for a blank reference, an unrecognized shape, or a project-less partial with
// no source project, so only well-formed resource identities become edge
// endpoints.
func ComputeFullResourceNameFromSelfLink(selfLink, sourceProjectID string) string {
	trimmed := strings.TrimSpace(selfLink)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, catalog.ComputeResourceNamePrefix) {
		return trimmed
	}
	// Full selfLink: take the path after the /compute/<version>/ segment.
	if idx := strings.Index(trimmed, catalog.ComputeSelfLinkMarker); idx >= 0 {
		afterMarker := trimmed[idx+len(catalog.ComputeSelfLinkMarker):]
		if _, path, ok := strings.Cut(afterMarker, "/"); ok {
			if normalized := ComputeResourceNameFromPath(path, sourceProjectID); normalized != "" {
				return normalized
			}
		}
		return ""
	}
	// Partial reference (no scheme, no /compute/ segment).
	return ComputeResourceNameFromPath(trimmed, sourceProjectID)
}

// ComputeResourceNameFromPath builds a CAI full resource name from a Compute
// resource path that is either project-qualified (projects/p/...) or a
// project-less partial (global/..., regions/..., zones/...) resolved against
// sourceProjectID. It returns an empty string for any other shape.
func ComputeResourceNameFromPath(path, sourceProjectID string) string {
	path = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(path), "/"))
	switch {
	case path == "":
		return ""
	case strings.HasPrefix(path, "projects/"):
		return catalog.ComputeResourceNamePrefix + path
	case strings.HasPrefix(path, "global/"),
		strings.HasPrefix(path, "regions/"),
		strings.HasPrefix(path, "zones/"):
		project := strings.TrimSpace(sourceProjectID)
		if project == "" {
			return ""
		}
		return catalog.ComputeResourceNamePrefix + "projects/" + project + "/" + path
	default:
		return ""
	}
}
