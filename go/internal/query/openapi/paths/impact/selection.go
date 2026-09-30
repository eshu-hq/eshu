// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impact

// selection.go holds the trace-deployment-chain request and response
// properties added by #7174 (evidence_detail, sections, section_detail),
// spliced into Routes. The sections enum must equal
// deployment.SectionNames(); go/internal/mcp tests assert that.

// traceDeploymentSelectionRequestProperties are the evidence_detail and
// sections request properties, the tail of the request property list.
const traceDeploymentSelectionRequestProperties = `
                  "evidence_detail": {"type": "string", "enum": ["full", "handles"], "default": "full", "description": "How emitted family rows are shaped. full (the HTTP default when absent) returns every row in full. handles projects primary-family rows to their identity keys (the same key names as the full row) and, when sections is absent, omits the families derived from them (delivery_paths, deployment_facts, controller_driven_paths, k8s_relationships, topology_edges, artifact_lineage, network_paths, entrypoints). The MCP trace_deployment_chain tool defaults to handles when sections is absent and to full when sections is named. Counts and overviews are always computed from the full lists. Unknown values return 400 invalid_argument."},
                  "sections": {"type": "array", "items": {"type": "string", "enum": ["instances", "topology_edges", "provisioned_platforms", "deployment_sources", "cloud_resources", "uncorrelated_cloud_resources", "k8s_resources", "k8s_relationships", "image_registry_truth", "deployment_facts", "controller_driven_paths", "delivery_paths", "deployment_evidence", "artifact_lineage", "hostnames", "entrypoints", "network_paths", "api_surface", "dependents", "consumer_repositories", "provisioning_source_chains", "story", "overviews"]}, "description": "Which families to emit; absent means the evidence_detail mode's default set. Identity keys, image_refs, deployment_fact_summary, every *_limits and *_truncated key, drilldowns, evidence_boundaries, evidence_detail, and section_detail are always emitted. overviews covers story_sections and the deployment, controller, gitops, runtime, provenance, documentation, and support overviews. An unselected family's key is absent (never an empty list) and is reported in section_detail and truth.omissions. Unknown values return 400 invalid_argument."}`

// traceDeploymentSelectionResponseProperties are the evidence_detail and
// section_detail response properties; each line ends with a comma because
// the instances property follows.
const traceDeploymentSelectionResponseProperties = `                    "evidence_detail": {"type": "string", "enum": ["full", "handles"], "description": "The evidence_detail the response was shaped with. Under handles, primary-family rows carry only their identity keys, so the per-row required lists below describe full rows."},
                    "section_detail": {
                      "type": "object",
                      "description": "One entry per selectable family (the sections enum). detail is full, handles, or omitted; returned is the rows emitted and total the rows the route held before the cut (existing *_limits objects still report query caps). A family not returned in full names the trace_deployment_chain drilldown that returns it in full. The same non-full families appear in truth.omissions.",
                      "additionalProperties": {
                        "type": "object",
                        "required": ["detail", "returned", "total"],
                        "properties": {
                          "detail": {"type": "string", "enum": ["full", "handles", "omitted"]},
                          "returned": {"type": "integer"},
                          "total": {"type": "integer"},
                          "drilldown_tool": {"type": "string", "enum": ["trace_deployment_chain"]},
                          "drilldown_arguments": {
                            "type": "object",
                            "properties": {
                              "service_name": {"type": "string"},
                              "sections": {"type": "array", "items": {"type": "string"}},
                              "evidence_detail": {"type": "string", "enum": ["full"]}
                            }
                          }
                        }
                      }
                    },`
