import { describe, expect, it } from "vitest";

import {
  matchesExpectedResponse,
  matchesExpectedResponsePrefix,
} from "./routeWorkflowProbeSupport";
import type { ResponseStub } from "./routeWorkflowProbesTestSupport";

function response(path: string): ResponseStub {
  return {
    request: () => ({ method: () => "GET" }),
    status: () => 200,
    url: () => `http://host${path}`,
  };
}

describe("route workflow response path matching", () => {
  it.each([
    "/api/v0/auth/admin/sign-in-policy",
    "/eshu-api/api/v0/auth/admin/sign-in-policy",
  ])("accepts the exact endpoint after optional proxy normalization: %s", (path) => {
    expect(
      matchesExpectedResponse(
        response(path) as never,
        "/api/v0/auth/admin/sign-in-policy",
        "GET",
        [200],
      ),
    ).toBe(true);
  });

  it.each([
    "/wrong/api/v0/auth/admin/sign-in-policy",
    "/api/v0/auth/admin/sign-in-policy-preview",
    "/eshu-api-extra/api/v0/auth/admin/sign-in-policy",
  ])("rejects an exact-endpoint collision: %s", (path) => {
    expect(
      matchesExpectedResponse(
        response(path) as never,
        "/api/v0/auth/admin/sign-in-policy",
        "GET",
        [200],
      ),
    ).toBe(false);
  });

  it.each([
    "/api/v0/repositories/repo-1/tree",
    "/eshu-api/api/v0/repositories/repo-1/tree/src/main.ts",
  ])("accepts an exact or segment-descendant dynamic route: %s", (path) => {
    expect(
      matchesExpectedResponsePrefix(
        response(path) as never,
        "/api/v0/repositories/repo-1/tree",
        "GET",
        [200],
      ),
    ).toBe(true);
  });

  it.each([
    "/api/v0/repositories/repo-1/tree-old",
    "/eshu-api/api/v0/repositories/repo-1/treehouse",
    "/wrong/api/v0/repositories/repo-1/tree",
  ])("rejects a dynamic-prefix collision: %s", (path) => {
    expect(
      matchesExpectedResponsePrefix(
        response(path) as never,
        "/api/v0/repositories/repo-1/tree",
        "GET",
        [200],
      ),
    ).toBe(false);
  });
});
