// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

const openAPISpecPrefix = `{
  "openapi": "3.0.3",
  "info": {
    "title": "Eshu API",
    "description": "Query API for the Eshu canonical knowledge graph. Provides read access to repositories, entities, code analysis, content, infrastructure, impact analysis, pipeline status, and environment comparison.",
    "version": "__ESHU_VERSION__",
    "contact": {
      "name": "Eshu"
    }
  },
  "servers": [
    {
      "url": "/api/v0",
      "description": "API v0 prefix"
    }
  ],
  "tags": [
    {"name": "health", "description": "Health check endpoints"},
    {"name": "repositories", "description": "Repository queries and context"},
    {"name": "entities", "description": "Entity resolution and relationships"},
    {"name": "code", "description": "Code search and analysis"},
    {"name": "content", "description": "Content store access"},
    {"name": "infrastructure", "description": "Infrastructure resource queries"},
    {"name": "images", "description": "Container image (OCI) inventory queries"},
    {"name": "impact", "description": "Impact analysis and dependency tracing"},
    {"name": "evidence", "description": "Evidence drilldown and provenance queries"},
    {"name": "admin", "description": "Administrative control and inspection routes"},
    {"name": "auth", "description": "Browser session and CSRF-safe dashboard authentication routes"},
    {"name": "status", "description": "Pipeline and ingester status"},
    {"name": "capabilities", "description": "Capability maturity catalog"},
    {"name": "freshness", "description": "Ingestion freshness and generation lifecycle drilldowns"},
    {"name": "compare", "description": "Environment comparison"}
  ],
  "paths": {
`
