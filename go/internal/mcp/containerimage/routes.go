// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimagetools

import (
	"strconv"

	"github.com/eshu-hq/eshu/go/internal/mcp/routecontract"
)

// Route selects the internal HTTP request for a container-image identity tool
// without executing it. It reports handled only for tools owned by this package.
func Route(toolName string, args routecontract.Arguments) (routecontract.Request, bool) {
	switch toolName {
	case "list_container_image_identities":
		return identitiesRequest(args), true
	case "list_container_image_tag_history":
		return tagHistoryRequest(args), true
	case "count_container_image_identities":
		return aggregateCountRequest(args), true
	case "get_container_image_identity_inventory":
		return aggregateInventoryRequest(args), true
	default:
		return routecontract.Request{}, false
	}
}

// identitiesRequest maps list_container_image_identities to the bounded
// read-only route GET /api/v0/supply-chain/container-images/identities, which
// query.SupplyChainHandler.listContainerImageIdentities serves.
//
// The handler requires limit and rejects a request with no scope anchor, so
// dropping limit here 400s every call the profile supports, and dropping
// whichever of digest, image_ref, source_repository_id, repository_id, or
// outcome the caller anchored on 400s that call -- except for a scoped token
// with no grants, which is answered with an empty page before the anchor is
// checked. after_identity_id is the keyset cursor; dropping
// it re-serves page one instead of failing. The handler owns the 1-200 limit
// bound and the exact_digest/tag_resolved outcome check.
func identitiesRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/container-images/identities", Query: map[string]string{
		"after_identity_id":    args.String("after_identity_id"),
		"digest":               args.String("digest"),
		"image_ref":            args.String("image_ref"),
		"limit":                strconv.Itoa(args.IntOr("limit", 50)),
		"outcome":              args.String("outcome"),
		"repository_id":        args.String("repository_id"),
		"source_repository_id": args.String("source_repository_id"),
	}}
}

// tagHistoryRequest maps list_container_image_tag_history to the bounded
// read-only route GET /api/v0/images/tag-history, which
// query.TagHistoryHandler.listTagHistory serves.
//
// The path prefix is the one asymmetry in this family: the other three tools
// sit under /api/v0/supply-chain/container-images/identities, while tag
// history is mounted at /api/v0/images/tag-history. Folding it onto the
// sibling prefix selects a path no handler mounts.
//
// repository_id and tag are both required, and the handler composes them into
// the image_ref it anchors on, rejecting a repository_id without the
// oci-registry:// prefix. limit and offset are offset-paged rather than
// cursor-paged here; the handler applies the same default of 50 on an empty
// limit and owns the 1-200 bound and the non-negative offset check.
func tagHistoryRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/images/tag-history", Query: map[string]string{
		"limit":         strconv.Itoa(args.IntOr("limit", 50)),
		"offset":        strconv.Itoa(args.IntOr("offset", 0)),
		"repository_id": args.String("repository_id"),
		"tag":           args.String("tag"),
	}}
}

// aggregateCountRequest maps count_container_image_identities to the cheap
// summary route GET /api/v0/supply-chain/container-images/identities/count,
// which query.SupplyChainHandler.countContainerImageIdentities serves.
//
// This is the only route in the family with no paging at all: the handler
// returns whole-scope totals by outcome and identity strength. Giving it a
// limit for symmetry with its three siblings would not cap anything: the
// handler never reads one, so the key would be inert and would advertise a
// bound the endpoint does not honor. The five filters are the scope, and each
// one dropped here widens the count and drops that key from the scope block the
// response echoes back, which reads as a broader answer rather than a wrong
// one.
func aggregateCountRequest(args routecontract.Arguments) routecontract.Request {
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/container-images/identities/count", Query: map[string]string{
		"digest":               args.String("digest"),
		"image_ref":            args.String("image_ref"),
		"source_repository_id": args.String("source_repository_id"),
		"repository_id":        args.String("repository_id"),
		"outcome":              args.String("outcome"),
	}}
}

// aggregateInventoryRequest maps get_container_image_identity_inventory to the
// grouped summary route
// GET /api/v0/supply-chain/container-images/identities/inventory, which
// query.SupplyChainHandler.containerImageIdentityInventory serves.
//
// The group_by fallback is not what makes an omitted dimension work: the
// handler independently defaults an empty group_by to outcome and rejects
// anything outside outcome, identity_strength, and repository_id with a 400.
// What the fallback does is keep the selected wire value stable, so changing
// it to another dimension would change the grouping the caller receives. An
// unsupported value is forwarded verbatim so the handler answers with its own
// 400 rather than the route silently correcting a typo into a valid grouping.
//
// limit defaults to 100 and offset to 0, matching the handler's own defaults;
// the handler owns the 1-500 limit bound and the 10000 offset ceiling.
func aggregateInventoryRequest(args routecontract.Arguments) routecontract.Request {
	groupBy := args.String("group_by")
	if groupBy == "" {
		groupBy = "outcome"
	}
	return routecontract.Request{Method: "GET", Path: "/api/v0/supply-chain/container-images/identities/inventory", Query: map[string]string{
		"group_by":             groupBy,
		"digest":               args.String("digest"),
		"image_ref":            args.String("image_ref"),
		"source_repository_id": args.String("source_repository_id"),
		"repository_id":        args.String("repository_id"),
		"outcome":              args.String("outcome"),
		"limit":                strconv.Itoa(args.IntOr("limit", 100)),
		"offset":               strconv.Itoa(args.IntOr("offset", 0)),
	}}
}
