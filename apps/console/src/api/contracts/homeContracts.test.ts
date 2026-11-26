import { readFileSync } from "node:fs";
import { resolve } from "node:path";

import { describe, expect, it } from "vitest";

import { contractsJsonPath, contractTypesDir, renderContractTypes } from "../../../../../scripts/console-contract-types.mjs";
import { demoHomeResponse, demoHomeRoutes } from "../demoHomeFixtures";
import { HOME_ROUTE_PATHS } from "../demoRouter";
import { validateShape } from "./contractShape.testSupport";

// homeContracts.json is pinned to the served spec by the Go test
// TestConsoleHomeContractsSnapshotIsCurrent. These tests close the loop on the
// console side: generated wire types match the snapshot, and every demo Home
// fixture is a valid instance of its route's 200-response contract.
const snapshot = JSON.parse(readFileSync(contractsJsonPath, "utf8")) as Record<string, unknown>;

describe("Home OpenAPI contracts", () => {
  it("keeps the generated wire types in step with the contract snapshot", () => {
    for (const [file, source] of Object.entries(renderContractTypes(snapshot))) {
      const committed = readFileSync(resolve(contractTypesDir, file), "utf8");
      expect(committed, `${file} is stale: run node scripts/console-contract-types.mjs`).toBe(source);
    }
  });

  it("routes exactly the snapshotted Home reads to the Home fixtures", () => {
    const routes = Object.keys(snapshot).map((route) => route.replace(/^GET /, ""));
    expect([...demoHomeRoutes].sort()).toEqual([...routes].sort());
    expect([...HOME_ROUTE_PATHS].sort()).toEqual([...routes].sort());
  });

  it.each(Object.keys(snapshot))("demo fixture for %s is a valid contract instance", (route) => {
    const path = route.replace(/^GET /, "");
    const result = demoHomeResponse("GET", path);
    expect(result).not.toBeNull();
    expect(validateShape(result?.data, snapshot[route])).toEqual([]);
  });
});

describe("contract validator", () => {
  const schema = {
    type: "object",
    required: ["id"],
    properties: {
      id: { type: "string" },
      state: { type: "string", enum: ["a", "b"] },
      tags: { type: "array", items: { type: "string" } },
      extra: { type: ["object", "null"], additionalProperties: {} },
    },
  };

  it("accepts a conforming instance", () => {
    expect(validateShape({ id: "x", state: "a", tags: ["t"], extra: null }, schema)).toEqual([]);
  });

  it("reports missing, undocumented, mistyped and out-of-enum values", () => {
    const problems = validateShape({ state: "z", tags: [1], surprise: true }, schema).map((i) => i.path);
    expect(problems).toEqual(["$.id", "$.state", "$.tags[0]", "$.surprise"]);
  });
});
