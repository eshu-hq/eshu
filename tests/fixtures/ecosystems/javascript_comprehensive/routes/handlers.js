// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// #5361 route query-proof matrix: cribs the Express named-handler shape
// proven by internal/parser/engine_javascript_route_handler_test.go.
const express = require("express");
const app = express();

app.get("/catalog/items", listItems);
app.post("/catalog/items", createItem);

function listItems(req, res) {
  res.json([]);
}

function createItem(req, res) {
  res.status(201).json({});
}
