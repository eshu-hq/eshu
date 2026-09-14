// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package chain

// TagHistory is the OpenAPI path fragment documenting the
// `/api/v0/images/tag-history` route. openapi.Spec concatenates it into the
// published document; keep it in lockstep with the handlers and
// docs/public/reference/http-api.md.
const TagHistory = `
    "/api/v0/images/tag-history": {
      "get": {
        "tags": ["images"],
        "summary": "List one image_ref's captured tag-mutation history (OCI)",
        "description": "Lists the bounded, ordered ContainerImageTagObservation history captured for one repository_id+tag over the authoritative graph (issue #5459): what digest the tag was first observed as, and the order its digests changed. Anchored on the existing container_image_tag_observation_ref index over image_ref, which the API composes server-side from repository_id and tag. Each read window is bounded by limit+1 with deterministic ordering by first_observed_at then uid, and continuation is the next_cursor token returned when truncated is true: a keyset token naming one row's first_observed_at and uid rather than a row position, so paging is forward-only and an observation inserted before that point is not returned on a later page. A tag that flips back to a previously observed digest collapses onto the same observation node, and first_observed_at is a set-once value that holds the first projected observation rather than a full chronological event log; see TagHistoryHandler's doc comment for both limitations. Scoped tokens receive the same shape bound to their repository grant (#6564). ContainerImageTagObservation nodes carry no source-repository key, so the API makes one extra single-clause read per window joining that window's resolved_digest and previous_digest values to ContainerImage.digest and following ContainerImage-[:BUILT_FROM]->Repository, then keeps a row only when the image at its resolved_digest is BUILT_FROM a granted repository. previous_digest is omitted unless its image is also BUILT_FROM a granted repository; mutated is left exactly as observed, so a row with mutated=true and no previous_digest still discloses that some prior digest existed, without disclosing which. Observations whose image has no BUILT_FROM edge (no resolved source build) are withheld from scoped callers; that is the coverage cost of this binding, and it hides history a shared-key caller can see. A scoped caller holding no grant gets an empty page without a graph read. A grant-filtered page is REFILLED across further reads until it holds limit visible rows, the history ends, or a small per-request read cap is reached, so on a filled page count below limit does not measure how many rows the grant filter withheld. Each refill read covers a FIXED 200 raw rows regardless of limit, so the span one request scans is a constant 800 raw rows. A capped page reports truncated=true with a cursor that resumes where the scan stopped, and an entirely withheld scan still returns a usable page (count 0, truncated true) rather than a silent end of history. Keep following next_cursor until truncated is false; it is encrypted with the deployment key, so pass it back exactly as issued and expect a 400 for any token this server did not issue, including one issued before a key rotation (restart from page one). What a capped page does disclose, stated here rather than hidden, is a COUNT and never an identity: with truncated=true and count below limit, the remainder of the 800 raw observations after the row you were last shown -- or after the frontier of your previous capped page -- are ones you may not see. You cannot choose where that span starts, cannot read the frontier, and never learn a withheld observation's first_observed_at, uid or digest. On a deployment that has configured no cursor sealing key (ESHU_AUTH_SECRET_ENC_KEY or ESHU_AUTH_SECRET_ENC_KEY_FILE), a grant-filtered truncated page omits next_cursor and says so in truth.reason, and a grant-filtered request carrying a cursor is refused with a 503; unscoped and all-scope callers are unaffected. A grant-filtered caller MUST continue with next_cursor: the offset parameter is the raw pre-filter row position, so a non-zero offset is refused with a 400 and the response omits the offset field. Unscoped and all-scope callers keep offset paging and the echoed offset. A scoped response carries grant_filtered=true and states the binding in truth.reason. An all-scope caller has no grant to bind: an all-scope bearer token or tenant-bound all-scope console session is admitted only when ESHU_GOVERNANCE_MODE is local_no_policy, hosted_single_tenant, or unset (which defaults to local_no_policy); hosted_multi_tenant and any unrecognized mode refuse it with a 403.",
        "operationId": "listContainerImageTagHistory",
        "x-scoped-token-support": true,
        "parameters": [
          {"name": "repository_id", "in": "query", "required": true, "schema": {"type": "string"}, "description": "OCI repository id such as oci-registry://host/path. Required; must carry the oci-registry:// prefix."},
          {"name": "tag", "in": "query", "required": true, "schema": {"type": "string"}, "description": "Tag observed for the image, such as 1.0.0. Required."},
          {"name": "limit", "in": "query", "schema": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50}, "description": "Maximum tag-observation rows per page (1..200, default 50)."},
          {"name": "cursor", "in": "query", "schema": {"type": "string"}, "description": "Continuation token from a truncated page's next_cursor. Pass it back verbatim: it is an authenticated-encryption envelope over a row key, so only a token this deployment issued for this image_ref opens, and anything else -- edited, synthesized, truncated, issued for another image_ref, or sealed under a key a rotation retired -- returns 400 rather than a silently reset or empty page. There is no row position inside it, so it stays valid when you change limit mid-walk, and a key that matches no current row is not an error (paging simply continues after that position). A grant-filtered caller on a deployment with no sealing key configured gets a 503 naming the variable to set instead. Mutually exclusive with a non-zero offset."},
          {"name": "offset", "in": "query", "schema": {"type": "integer", "minimum": 0, "default": 0}, "description": "Raw row offset for continuation. Accepted for unscoped and all-scope callers only; a grant-filtered (scoped-token) caller must continue with cursor and receives a 400 for a non-zero offset, because the offset is the pre-filter row position. offset=0 is always legal and names the start of the history."}
        ],
        "responses": {
          "504": {"$ref": "#/components/responses/GatewayTimeout"},
          "200": {
            "description": "Container image tag-observation history rows",
            "content": {
              "application/json": {
                "schema": {
                  "type": "object",
                  "properties": {
                    "tag_history": {
                      "type": "array",
                      "items": {
                        "type": "object",
                        "properties": {
                          "tag": {"type": "string"},
                          "resolved_digest": {"type": "string"},
                          "previous_digest": {"type": "string"},
                          "mutated": {"type": "boolean"},
                          "first_observed_at": {"type": "string"},
                          "repository_id": {"type": "string"},
                          "identity_strength": {"type": "string"}
                        },
                        "required": ["tag", "resolved_digest", "mutated", "repository_id"]
                      }
                    },
                    "count": {"type": "integer"},
                    "limit": {"type": "integer"},
                    "offset": {"type": "integer", "description": "Echoed request offset. Omitted for a grant-filtered page, which pages by cursor alone: the offset of such a page is the raw pre-filter frontier the refill advanced to."},
                    "truncated": {"type": "boolean"},
                    "image_ref": {"type": "string"},
                    "repository_id": {"type": "string"},
                    "tag": {"type": "string"},
                    "grant_filtered": {"type": "boolean", "description": "Present and true only for a scoped caller whose page was bound to its repository grant through ContainerImage-[:BUILT_FROM]->Repository. Such a page is refilled to limit visible rows, so count below limit means the history ended or the per-request read cap was reached, not that rows were withheld from this window. On a cap-reached page (truncated true with count below limit) the shortfall does describe the scanned span, which is a constant 800 raw rows: count 0 there means every one of them was withheld. That is a count, not an identity -- the span always starts at a row you were shown or at the sealed frontier of your previous capped page, and the withheld rows themselves are never named."},
                    "next_cursor": {"type": "string", "description": "Present when truncated is true and the server holds a cursor sealing key. An encrypted keyset continuation token; pass it back as cursor and do not parse or synthesize it. Its plaintext names one row's first_observed_at and uid: on a normal page the last row this page returned, and on a cap-reached page that returned no rows at all the last raw row the scan reached, which for a grant-filtered caller may be a row withheld from that caller -- which is why the token is sealed rather than readable. It is omitted on a truncated grant-filtered page when no sealing key is configured; truth.reason says so and names the variable."}
                  },
                  "required": ["tag_history", "count", "limit", "truncated", "image_ref", "repository_id", "tag"]
                }
              }
            }
          },
          "400": {"$ref": "#/components/responses/BadRequest"},
          "403": {"$ref": "#/components/responses/Forbidden"},
          "500": {"$ref": "#/components/responses/InternalError"},
          "501": {"$ref": "#/components/responses/NotImplemented"},
          "503": {"$ref": "#/components/responses/ServiceUnavailable"}
        }
      }
    },
`
