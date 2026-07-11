// api/queryPlaybooks.test.ts
// Verifies queryPlaybooks:
//   - listPlaybooks normalizes the wire catalog (snake_case -> camelCase)
//   - listPlaybooks resolves to an "empty" provenance when the catalog has no
//     playbooks, and "unavailable" on a request failure, without throwing
//   - resolvePlaybook normalizes a resolved plan and propagates the envelope
//     truth alongside it
//   - resolvePlaybook throws (does not swallow) on an envelope error, since a
//     resolve failure is a specific, user-actionable error the page must show
import { describe, expect, it } from "vitest";

import type { EshuApiClient } from "./client";
import { EshuEnvelopeError } from "./envelope";
import { listPlaybooks, resolvePlaybook } from "./queryPlaybooks";

// The mock functions below are intentionally typed against a concrete (non-
// generic) signature rather than EshuApiClient["get"]/["post"] directly: those
// methods are generic over the response payload, and TypeScript cannot assign
// a fixed-payload mock function to a generic function type. The outer
// `as unknown as EshuApiClient` cast is how every other page/api test in this
// codebase mocks the client (see CapabilityMatrixPage.test.tsx).
function clientWithGet(get: (path: string) => Promise<unknown>): EshuApiClient {
  return { get } as unknown as EshuApiClient;
}

function clientWithPost(post: (path: string, body: unknown) => Promise<unknown>): EshuApiClient {
  return { post } as unknown as EshuApiClient;
}

describe("listPlaybooks", () => {
  it("normalizes the wire catalog to the console view model", async () => {
    const client = clientWithGet(async () => ({
      data: {
        playbooks: [
          {
            id: "service_story_citation",
            name: "Service story with citation packet",
            version: "1.0.0",
            prompt_family: "service.story",
            description: "Answer a service story prompt with evidence.",
            required_inputs: [
              {
                name: "service_name",
                type: "identifier",
                required: true,
                description: "Service to describe.",
              },
              { name: "environment", type: "string", required: false },
            ],
            steps: [
              {
                id: "service_dossier",
                tool: "get_service_story",
                expected_truth: "deterministic",
                evidence_expected: "one-call service dossier",
                drilldowns: [{ tool: "get_service_context", reason: "drill into raw context" }],
              },
            ],
            failure_modes: [
              {
                condition: "service not found",
                meaning: "no matching service",
                fallback: "resolve_entity",
              },
            ],
          },
        ],
        versions: [{ id: "service_story_citation", version: "1.0.0" }],
        count: 1,
      },
      error: null,
      truth: {
        capability: "query.playbooks",
        level: "exact",
        profile: "production",
        freshness: { state: "fresh" },
      },
    }));

    const page = await listPlaybooks(client);

    expect(page.provenance).toBe("live");
    expect(page.count).toBe(1);
    expect(page.truth?.capability).toBe("query.playbooks");
    expect(page.playbooks).toHaveLength(1);
    const playbook = page.playbooks[0];
    expect(playbook.id).toBe("service_story_citation");
    expect(playbook.promptFamily).toBe("service.story");
    expect(playbook.requiredInputs).toEqual([
      {
        name: "service_name",
        type: "identifier",
        required: true,
        description: "Service to describe.",
      },
      { name: "environment", type: "string", required: false, description: "" },
    ]);
    expect(playbook.steps[0]).toEqual({
      id: "service_dossier",
      tool: "get_service_story",
      expectedTruth: "deterministic",
      evidenceExpected: "one-call service dossier",
      drilldowns: [{ tool: "get_service_context", reason: "drill into raw context" }],
    });
    expect(playbook.failureModes).toEqual([
      {
        condition: "service not found",
        meaning: "no matching service",
        fallback: "resolve_entity",
      },
    ]);
  });

  it("resolves to an empty provenance when the catalog has no playbooks", async () => {
    const client = clientWithGet(async () => ({
      data: { playbooks: [], versions: [], count: 0 },
      error: null,
      truth: {
        capability: "query.playbooks",
        level: "exact",
        profile: "production",
        freshness: { state: "fresh" },
      },
    }));

    const page = await listPlaybooks(client);

    expect(page.provenance).toBe("empty");
    expect(page.playbooks).toEqual([]);
  });

  it("resolves to an unavailable provenance on a request failure, without throwing", async () => {
    const client = clientWithGet(async () => {
      throw new Error("HTTP 503");
    });

    const page = await listPlaybooks(client);

    expect(page.provenance).toBe("unavailable");
    expect(page.playbooks).toEqual([]);
    expect(page.truth).toBeNull();
  });

  it("resolves to an unavailable provenance on an envelope error", async () => {
    const client = clientWithGet(async () => ({
      data: null,
      error: { code: "internal_error", message: "boom" },
      truth: null,
    }));

    const page = await listPlaybooks(client);

    expect(page.provenance).toBe("unavailable");
  });
});

describe("resolvePlaybook", () => {
  it("normalizes a resolved plan and carries the envelope truth", async () => {
    let capturedBody: unknown;
    const client = clientWithPost(async (path: string, body: unknown) => {
      capturedBody = body;
      expect(path).toBe("/api/v0/query-playbooks/resolve");
      return {
        data: {
          resolved: {
            playbook_id: "service_story_citation",
            version: "1.0.0",
            prompt_family: "service.story",
            calls: [
              {
                step_id: "service_dossier",
                tool: "get_service_story",
                arguments: { service_name: "checkout-service" },
                expected_truth: "deterministic",
                evidence_expected: "one-call service dossier",
              },
            ],
            failure_modes: [
              {
                condition: "service not found",
                meaning: "no matching service",
                fallback: "resolve_entity",
              },
            ],
          },
        },
        error: null,
        truth: {
          capability: "query.playbooks",
          level: "exact",
          profile: "production",
          freshness: { state: "fresh" },
        },
      };
    });

    const result = await resolvePlaybook(client, {
      playbookId: "service_story_citation",
      inputs: { service_name: "checkout-service" },
    });

    expect(capturedBody).toEqual({
      playbook_id: "service_story_citation",
      inputs: { service_name: "checkout-service" },
    });
    expect(result.truth?.level).toBe("exact");
    expect(result.resolved.playbookId).toBe("service_story_citation");
    expect(result.resolved.calls).toEqual([
      {
        stepId: "service_dossier",
        tool: "get_service_story",
        arguments: { service_name: "checkout-service" },
        expectedTruth: "deterministic",
        evidenceExpected: "one-call service dossier",
        drilldowns: [],
      },
    ]);
    expect(result.resolved.failureModes).toEqual([
      {
        condition: "service not found",
        meaning: "no matching service",
        fallback: "resolve_entity",
      },
    ]);
  });

  it("throws on an envelope error instead of swallowing it", async () => {
    const client = clientWithPost(async () => ({
      data: null,
      error: { code: "invalid_argument", message: 'required input "service_name" is missing' },
      truth: null,
    }));

    await expect(
      resolvePlaybook(client, { playbookId: "service_story_citation", inputs: {} }),
    ).rejects.toBeInstanceOf(EshuEnvelopeError);
  });

  it("throws on an HTTP failure instead of swallowing it", async () => {
    const client = clientWithPost(async () => {
      throw new Error("HTTP 404");
    });

    await expect(resolvePlaybook(client, { playbookId: "missing", inputs: {} })).rejects.toThrow(
      "HTTP 404",
    );
  });
});
