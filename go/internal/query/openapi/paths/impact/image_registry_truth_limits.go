// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// ImageRegistryTruthLimits is the shared OpenAPI schema fragment for the
// trace_deployment_chain response's image_registry_truth_limits block
// (#6590): bound and completeness disclosure for the OCI registry-truth
// read, spliced into routes.go beside image_registry_truth.
const ImageRegistryTruthLimits = `
                    "image_registry_truth_limits": {
                      "type": "object",
                      "description": "Bound and completeness disclosure for the OCI registry-truth read (issue #6590). Every OCI tag-observation and image-by-digest statement carries LIMIT $row_limit; a ref withheld by that bound never gets a placeholder row in image_registry_truth.",
                      "required": ["max_keys_per_statement", "statement_row_limit", "image_registry_truth_complete", "truncated_image_ref_count"],
                      "properties": {
                        "max_keys_per_statement": {"type": "integer", "description": "Per-statement IN-list key bound (image_refs or digests batch size)."},
                        "statement_row_limit": {"type": "integer", "description": "Per-statement LIMIT $row_limit on every OCI tag-observation and image-by-digest read."},
                        "image_registry_truth_complete": {"type": "boolean", "description": "Always present. False only when at least one image ref was withheld because a statement hit statement_row_limit for its key, or, for a tag ref, for a digest one of its observations resolved to; true otherwise."},
                        "truncated_image_ref_count": {"type": "integer", "description": "Always present. Count of image refs withheld by the row-limit bound."},
                        "truncated_image_refs": {"type": "array", "items": {"type": "string"}, "description": "Present only when image_registry_truth_complete is false. Sorted, unique image refs withheld from image_registry_truth."},
                        "image_registry_truth_incomplete_reason": {"type": "string", "description": "Machine-readable reason paired with image_registry_truth_complete=false. Present only when the read was incomplete.", "enum": ["oci_registry_truth_row_limit_reached"]}
                      }
                    },`
