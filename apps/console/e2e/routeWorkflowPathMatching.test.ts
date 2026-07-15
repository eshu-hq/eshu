import { describe, expect, it } from "vitest";

import {
  matchesExpectedResponse,
  matchesExpectedResponsePrefix,
  matchesWorkflowResponse,
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

  it.each([
    "/api/v0/services/service-one/neighbor/story",
    "/api/v0/services/service-one/story/neighbor",
    "/api/v0/investigations/services/service-one/neighbor",
  ])("rejects a dynamic workflow response containing nested segments: %s", (path) => {
    const expectation = path.startsWith("/api/v0/investigations")
      ? {
          pathPrefix: "/api/v0/investigations/services/",
          pathSuffix: "",
          method: "GET" as const,
          acceptedStatuses: [200],
        }
      : {
          pathPrefix: "/api/v0/services/",
          pathSuffix: "/story",
          method: "GET" as const,
          acceptedStatuses: [200],
        };

    expect(matchesWorkflowResponse(response(path) as never, expectation)).toBe(false);
  });

  it("accepts one non-empty encoded dynamic segment", () => {
    expect(
      matchesWorkflowResponse(response("/api/v0/services/team%2Fgateway/story") as never, {
        pathPrefix: "/api/v0/services/",
        pathSuffix: "/story",
        method: "GET",
        acceptedStatuses: [200],
      }),
    ).toBe(true);
  });
});
