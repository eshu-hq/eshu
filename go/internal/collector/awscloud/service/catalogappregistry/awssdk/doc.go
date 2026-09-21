// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package awssdk adapts the AWS SDK for Go v2 Service Catalog AppRegistry
// client into the metadata-only AppRegistry scanner interface.
//
// The adapter uses ListApplications, ListAttributeGroups,
// ListAttributeGroupsForApplication, ListAssociatedResources, and
// ListTagsForResource to read AppRegistry application and attribute-group
// control-plane metadata, application associations, and resource tags. It
// intentionally excludes GetAttributeGroup and GetConfiguration (which return
// the attribute-group content body), GetAssociatedResource (which returns the
// associated-resource tag value detail), and every Create/Update/Delete/
// Associate/Disassociate/Put/Tag mutation API, so the adapter cannot read
// content bodies or write AppRegistry state.
package awssdk
