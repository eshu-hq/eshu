import { describe, expect, it, vi } from "vitest";

import type { EshuApiClient } from "./client";
import { loadVulnerabilityStatusPage } from "./eshuConsoleVulnerabilities";

function clientEchoing(profile: string | undefined): {
  client: EshuApiClient;
  get: ReturnType<typeof vi.fn>;
} {
  const get = vi.fn(async () => ({
    data: {
      findings: [{ finding_id: "f-1", cve_id: "CVE-2026-1" }],
      truncated: false,
      ...(profile === undefined ? {} : { detection_profile: profile }),
    },
    error: null,
    truth: null,
  }));
  return { client: { get } as unknown as EshuApiClient, get };
}

function paramsOf(get: ReturnType<typeof vi.fn>, call = 0): URLSearchParams {
  const [path] = get.mock.calls[call] as unknown as [string];
  return new URL(path, "http://x").searchParams;
}

describe("impact finding page detection profile", () => {
  it("sends profile=comprehensive for possibly_affected on the first page", async () => {
    const { client, get } = clientEchoing("comprehensive");
    const page = await loadVulnerabilityStatusPage(client, new Map(), "possibly_affected");
    expect(paramsOf(get).get("profile")).toBe("comprehensive");
    expect(paramsOf(get).get("impact_status")).toBe("possibly_affected");
    expect(page.detectionProfile).toBe("comprehensive");
  });

  it("keeps profile=comprehensive on a cursor read", async () => {
    const { client, get } = clientEchoing("comprehensive");
    await loadVulnerabilityStatusPage(client, new Map(), "possibly_affected", "cursor-1");
    expect(paramsOf(get).get("after_finding_id")).toBe("cursor-1");
    expect(paramsOf(get).get("profile")).toBe("comprehensive");
  });

  it("sends the comprehensive profile for exact and derived lists when the filter asks for it", async () => {
    const { client, get } = clientEchoing("comprehensive");
    await loadVulnerabilityStatusPage(
      client,
      new Map(),
      "affected_exact",
      null,
      {},
      {
        key: "service_id",
        value: "svc",
        profile: "comprehensive",
      },
    );
    expect(paramsOf(get).get("profile")).toBe("comprehensive");
    expect(paramsOf(get).get("service_id")).toBe("svc");
  });

  it("does not send a profile for a plain precise list", async () => {
    const { client, get } = clientEchoing("precise");
    const page = await loadVulnerabilityStatusPage(client, new Map(), "affected_exact");
    expect(paramsOf(get).has("profile")).toBe(false);
    expect(page.detectionProfile).toBe("precise");
  });

  it("fails when the server answers on a different profile than requested", async () => {
    const { client } = clientEchoing("precise");
    await expect(
      loadVulnerabilityStatusPage(client, new Map(), "possibly_affected"),
    ).rejects.toThrow(/comprehensive.*precise|precise.*comprehensive/);
  });

  it("fails when a comprehensive request gets no profile echo", async () => {
    const { client } = clientEchoing(undefined);
    await expect(
      loadVulnerabilityStatusPage(client, new Map(), "possibly_affected"),
    ).rejects.toThrow(/did not report/);
  });

  it("fails when a precise request is answered comprehensively", async () => {
    const { client } = clientEchoing("comprehensive");
    await expect(loadVulnerabilityStatusPage(client, new Map(), "affected_exact")).rejects.toThrow(
      /precise/,
    );
  });
});
