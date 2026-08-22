// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitdocs turns repository documentation files into content and
// documentation facts.
//
// It covers markdown and text, notebooks, OOXML (docx/pptx/xlsx), spreadsheets,
// diagrams (mermaid, d2, plantuml, drawio, excalidraw, svg), API contracts
// (openapi/swagger/asyncapi, GraphQL SDL), and archives. Format routing decides
// which extractor runs; gitrepo's fact stream calls the emitters.
package gitdocs
