// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimagetools

import (
	toolcontract "github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
)

// Tools returns the container-image identity list definitions owned by this
// package: the cursor-paged identity listing and the ordered tag history.
// The parent mcp package splices the whole family slice at its long-standing
// position inside the supply-chain block, so a future arity change registers
// automatically instead of panicking on an index.
func Tools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "list_container_image_identities",
			Description: "List reducer-owned container image identity facts by digest, image reference, source repository bridge, OCI repository, or outcome. Populated by the opt-in oci_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus container-registry credentials), so a default git-only deploy returns an empty page.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"digest": map[string]any{
						"type":        "string",
						"description": "Image digest such as sha256:...",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Original image reference observed in source or runtime evidence.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "OCI repository identity such as oci-registry://registry.example/team/api.",
					},
					"source_repository_id": map[string]any{
						"type":        "string",
						"description": "source repository id or selector for bridge reads; this is not an OCI image repository identity.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter.",
						"enum":        []string{"exact_digest", "tag_resolved"},
					},
					"after_identity_id": map[string]any{
						"type":        "string",
						"description": "Identity ID from next_cursor when continuing a truncated page.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum identity rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
				},
			},
		},
		{
			Name:        "list_container_image_tag_history",
			Description: "List the bounded, ordered ContainerImageTagObservation history captured for one repository_id+tag (issue #5459): what digest the tag was first observed as, and the order its digests changed. repository_id and tag are both required; the server composes the image_ref anchor from them. A tag that flips back to a previously observed digest (A -> B -> A) collapses onto the same observation node rather than producing a new event, and first_observed_at is a set-once value that holds the FIRST projected observation rather than a full chronological event log -- see the route's doc comment for both limitations. Populated by the opt-in oci_registry collector (off in a default deploy; enable with ESHU_COLLECTOR_INSTANCES_JSON plus container-registry credentials), so a default git-only deploy returns an empty page. Scoped (personal-token) callers see only rows bound to their repository grant through ContainerImage-[:BUILT_FROM]->Repository (#6564): a row is kept only when the image at its resolved_digest is BUILT_FROM a granted repository, previous_digest is omitted unless its image is too, and observations whose image has no BUILT_FROM edge are withheld, so a scoped caller can see less history than the shared ESHU_API_KEY does. mutated is left exactly as observed, so a row with mutated=true and no previous_digest still tells you some prior digest existed, without telling you which. A grant-filtered page is refilled across further reads until it holds limit rows, the history ends, or a small per-request read cap is reached, so a short page does NOT tell you how many rows were withheld; each refill read covers a fixed 200 raw rows whatever limit you asked for, so one request scans a constant 800. A scan that read only withheld rows comes back with zero rows and truncated=true rather than ending the history. Continue with next_cursor, an encrypted token bound to this image_ref -- its plaintext names one row's first_observed_at and uid rather than a row position, so paging is forward-only (an observation inserted before that point is not returned later) and the token stays valid if you change limit mid-walk. Pass it back verbatim as cursor; you cannot parse or synthesize one, and a token this server did not issue -- one issued before a key rotation, or one issued under repository grants other than the ones you hold now -- gets a 400, so restart from page one. Keep following it until truncated is false. What a capped page discloses is a COUNT and never an identity: zero rows with truncated=true means the next 800 raw observations after the row you were last shown, or after the frontier of your previous capped page, held nothing you are entitled to. You cannot aim that span, cannot read the frontier, and never learn a withheld observation's first_observed_at, uid or digest. If the server has no cursor sealing key configured (ESHU_AUTH_SECRET_ENC_KEY/_FILE), a scoped caller's truncated page omits next_cursor and truth.reason says so, and sending a cursor gets a 503; unscoped callers are unaffected. offset is for unscoped/shared-key callers only: a scoped caller sending a non-zero offset gets a 400, and a grant-filtered response omits the offset field. Scoped responses carry grant_filtered=true.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"repository_id": map[string]any{
						"type":        "string",
						"description": "OCI repository identity such as oci-registry://registry.example/team/api. Required; must carry the oci-registry:// prefix.",
					},
					"tag": map[string]any{
						"type":        "string",
						"description": "Tag observed for the image, such as 1.0.0. Required.",
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum tag-observation rows to return.",
						"default":     50,
						"minimum":     1,
						"maximum":     200,
					},
					"cursor": map[string]any{
						"type":        "string",
						"description": "Continuation token from a truncated page's next_cursor. Pass it back verbatim: it is an authenticated-encryption envelope over a row key, so only a token this deployment issued for this image_ref opens. Edited, synthesized, truncated, foreign-image_ref, pre-rotation and foreign-grant tokens all get a 400 -- restart from page one. The last case covers your own token after your repository grants change mid-walk, not only another caller's. There is no row position inside it, so it is not bound to the limit it was issued for, and a key matching no current row is not an error. This is the only continuation a scoped (personal-token) caller may use, and on a server with no sealing key configured that caller gets a 503 naming the variable instead.",
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Raw row offset for continuation, for unscoped/shared-key callers only. A scoped caller sending a non-zero offset gets a 400 and must use cursor instead; offset=0 names the start of the history and is always legal.",
						"default":     0,
						"minimum":     0,
					},
				},
				"required": []string{"repository_id", "tag"},
			},
		},
	}
}

// AggregateTools returns the cheap-summary aggregate definitions owned by
// this package: the identity count and the grouped identity inventory. They
// ship alongside the listing for ecosystem-level questions that do not need
// paging through individual identity rows. The parent mcp package splices
// the whole family slice at its long-standing aggregates position.
func AggregateTools() []toolcontract.ToolDefinition {
	return []toolcontract.ToolDefinition{
		{
			Name:        "count_container_image_identities",
			Description: "Return reducer-owned container image identity totals for one optional scope without paging through individual identity rows. Provides total identities and rollups by outcome (exact_digest / tag_resolved) and identity_strength. Use before list_container_image_identities when the question is a count, not a list.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"digest": map[string]any{
						"type":        "string",
						"description": "Optional image digest (such as `sha256:...`) to scope the totals.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional original image reference observed in source or runtime evidence to scope the totals.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional OCI repository identity (such as `oci-registry://registry.example/team/api`) to scope the totals.",
					},
					"source_repository_id": map[string]any{
						"type":        "string",
						"description": "Optional source repository id or selector for bridge-scoped totals; this is not an OCI image repository identity.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter applied before counting.",
						"enum":        []string{"exact_digest", "tag_resolved"},
					},
				},
			},
		},
		{
			Name:        "get_container_image_identity_inventory",
			Description: "Return a paginated grouped count of reducer-owned container image identities along one dimension (outcome, identity_strength, repository_id). Replaces the page-and-iterate caller pattern for ecosystem-level inventory questions.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"group_by": map[string]any{
						"type":        "string",
						"description": "Grouping dimension. outcome (default) groups by reducer outcome; identity_strength groups by reducer identity strength; repository_id groups by OCI repository.",
						"enum":        []string{"outcome", "identity_strength", "repository_id"},
						"default":     "outcome",
					},
					"digest": map[string]any{
						"type":        "string",
						"description": "Optional image digest to scope the inventory.",
					},
					"image_ref": map[string]any{
						"type":        "string",
						"description": "Optional original image reference to scope the inventory.",
					},
					"repository_id": map[string]any{
						"type":        "string",
						"description": "Optional OCI repository identity to scope the inventory.",
					},
					"source_repository_id": map[string]any{
						"type":        "string",
						"description": "Optional source repository id or selector for bridge-scoped inventory; this is not an OCI image repository identity.",
					},
					"outcome": map[string]any{
						"type":        "string",
						"description": "Optional reducer identity outcome filter applied before grouping.",
						"enum":        []string{"exact_digest", "tag_resolved"},
					},
					"limit": map[string]any{
						"type":        "integer",
						"description": "Maximum buckets to return per page.",
						"default":     100,
						"minimum":     1,
						"maximum":     500,
					},
					"offset": map[string]any{
						"type":        "integer",
						"description": "Zero-based result offset for paging.",
						"default":     0,
						"minimum":     0,
						"maximum":     10000,
					},
				},
			},
		},
	}
}
