// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package openapi assembles and serves the published OpenAPI 3.0 document
// for the Eshu Query API (Issue #6060 lane C, #6642): Spec concatenates the
// document prefix, every route family's path fragments under paths/, and
// the shared components block, substituting the running build's version
// into the __ESHU_VERSION__ placeholder. ServeSpec, ServeSwaggerUI, and
// ServeReDoc serve the assembled document and its two browser viewers.
//
// prefix.go holds the document header (info, servers, tags); components.go
// and its six components_*.go siblings hold the shared "components" block
// that this package and the paths/ leaves both draw fragments from through
// openapi/schema. The query root keeps OpenAPISpec, ServeOpenAPI,
// ServeSwaggerUI, and ServeReDoc as forwards in handler.go for existing
// callers; this package owns assembly, the root only re-exports it.
package openapi
