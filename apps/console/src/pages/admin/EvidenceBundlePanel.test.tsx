// pages/admin/EvidenceBundlePanel.test.tsx
// Verifies EvidenceBundlePanel (issue #4045): the Admin console surface for
// GET /api/v0/evidence/bundle.
import { render, screen, waitFor } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

import { EvidenceBundlePanel } from "./EvidenceBundlePanel";
import { EshuApiHttpError } from "../../api/client";
import type { EshuApiClient } from "../../api/client";
import type { EvidenceBundleWire } from "../../api/evidenceBundle";

function fixtureBundle(overrides: Partial<EvidenceBundleWire> = {}): EvidenceBundleWire {
  return {
    schema_version: "evidence_bundle.v1",
    bundle_id: "sha256:deadbeef",
    identity: { scope_id: "stack", profile: "live", created_at: "2026-08-11T00:00:00Z" },
    redaction: { profile: "default", rules: ["screened_private_endpoints"] },
    contents: {
      pipeline_state: {
        repository_count: 42,
        health_state: "healthy",
        queue: {
          total: 10,
          outstanding: 2,
          overdue_claims: 0,
          oldest_outstanding_age_seconds: 5,
          pending: 1,
          in_flight: 1,
          retrying: 0,
          succeeded: 8,
          failed: 0,
          dead_letter: 1,
        },
        queue_blocked_count: 0,
      },
      semantic_provider_state: {
        state: "available",
        provider_configured: true,
      },
    },
    missing_evidence: [
      { family: "fact_counts", reason: "no status endpoint exposes per-kind fact counts" },
    ],
    validation: { status: "passed", checks: ["schema", "redaction"] },
    ...overrides,
  };
}

describe("EvidenceBundlePanel", () => {
  it("renders repository count, health, queue, semantic provider state, and missing-evidence rows", async () => {
    const client = { getJson: vi.fn(async () => fixtureBundle()) } as unknown as EshuApiClient;
    render(<EvidenceBundlePanel client={client} />);

    expect(await screen.findByText("42")).toBeInTheDocument();
    expect(screen.getByText("healthy")).toBeInTheDocument();
    expect(screen.getByText("available")).toBeInTheDocument();
    expect(screen.getByText(/fact_counts/)).toBeInTheDocument();
    expect(screen.getByText(/no status endpoint exposes per-kind fact counts/)).toBeInTheDocument();
  });

  it("renders 'no gaps' when missing_evidence is empty (no fabrication)", async () => {
    const client = {
      getJson: vi.fn(async () => fixtureBundle({ missing_evidence: [] })),
    } as unknown as EshuApiClient;
    render(<EvidenceBundlePanel client={client} />);

    expect(await screen.findByText(/no missing-evidence gaps reported/i)).toBeInTheDocument();
  });

  it("renders the operator-scope note on a 403 (forbidden — not an error)", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new EshuApiHttpError(403);
      }),
    } as unknown as EshuApiClient;
    render(<EvidenceBundlePanel client={client} />);

    expect(await screen.findByRole("status")).toHaveTextContent(/stack-wide/i);
  });

  it("renders 'unavailable' on a real error (no fabricated data)", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new EshuApiHttpError(503);
      }),
    } as unknown as EshuApiClient;
    render(<EvidenceBundlePanel client={client} />);

    expect(await screen.findByText(/unavailable from this source/)).toBeInTheDocument();
  });

  it("renders 'unavailable' when no client is provided", async () => {
    render(<EvidenceBundlePanel client={undefined} />);
    expect(await screen.findByText(/unavailable from this source/)).toBeInTheDocument();
  });

  it("does not crash on a malformed/empty payload and shows 'not reported' instead of fabricating data", async () => {
    const client = {
      getJson: vi.fn(async () => ({}) as EvidenceBundleWire),
    } as unknown as EshuApiClient;
    render(<EvidenceBundlePanel client={client} />);

    expect(await screen.findAllByText(/not reported/i)).not.toHaveLength(0);
  });

  it("never states the bundle is 'guaranteed' safe (redaction screens, it does not certify)", async () => {
    const client = { getJson: vi.fn(async () => fixtureBundle()) } as unknown as EshuApiClient;
    render(<EvidenceBundlePanel client={client} />);

    await screen.findByText("42");
    expect(screen.queryByText(/guarantee/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/no private/i)).not.toBeInTheDocument();
  });

  describe("download", () => {
    let createObjectURL: ReturnType<typeof vi.fn>;
    let revokeObjectURL: ReturnType<typeof vi.fn>;

    beforeEach(() => {
      createObjectURL = vi.fn(() => "blob:mock-url");
      revokeObjectURL = vi.fn();
      vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL });
    });

    afterEach(() => {
      vi.unstubAllGlobals();
    });

    it("offers a download of the exact bundle JSON once loaded", async () => {
      const bundle = fixtureBundle();
      const client = { getJson: vi.fn(async () => bundle) } as unknown as EshuApiClient;
      const RealBlob = globalThis.Blob;
      // jsdom's Blob does not implement .text()/.arrayBuffer(), so capture the
      // constructor arguments directly instead of round-tripping through the
      // Blob instance API.
      const blobCalls: { parts: BlobPart[]; options: BlobPropertyBag | undefined }[] = [];
      class SpyBlob extends RealBlob {
        constructor(parts: BlobPart[], options?: BlobPropertyBag) {
          super(parts, options);
          blobCalls.push({ parts, options });
        }
      }
      vi.stubGlobal("Blob", SpyBlob);

      render(<EvidenceBundlePanel client={client} />);

      const button = await screen.findByRole("button", { name: /download/i });
      button.click();

      expect(createObjectURL).toHaveBeenCalledTimes(1);
      expect(blobCalls).toHaveLength(1);
      expect(blobCalls[0].options?.type).toBe("application/json");
      // The panel writes one JSON string part; narrow it rather than relying on
      // default stringification, which would silently pass for an object part.
      const [part] = blobCalls[0].parts;
      expect(typeof part).toBe("string");
      expect(JSON.parse(part as string)).toEqual(bundle);
    });

    it("does not render a download control when the bundle is forbidden or unavailable", async () => {
      const client = {
        getJson: vi.fn(async () => {
          throw new EshuApiHttpError(403);
        }),
      } as unknown as EshuApiClient;
      render(<EvidenceBundlePanel client={client} />);

      await screen.findByRole("status");
      expect(screen.queryByRole("button", { name: /download/i })).not.toBeInTheDocument();
    });
  });

  it("cancels a stale load when the client swaps mid-flight (no committed state from a superseded source)", async () => {
    let resolveFirst: (bundle: EvidenceBundleWire) => void = () => {};
    const first = {
      getJson: vi.fn(
        () =>
          new Promise<EvidenceBundleWire>((resolve) => {
            resolveFirst = resolve;
          }),
      ),
    } as unknown as EshuApiClient;
    const second = {
      getJson: vi.fn(async () => fixtureBundle({ bundle_id: "sha256:second" })),
    } as unknown as EshuApiClient;

    const view = render(<EvidenceBundlePanel client={first} />);
    view.rerender(<EvidenceBundlePanel client={second} />);
    await waitFor(() => expect(screen.getByText("sha256:second")).toBeInTheDocument());

    // The first (superseded) client's request resolves after the second
    // client's already committed. A missing cancellation guard would let this
    // stale response clobber the current, correct bundle_id.
    resolveFirst(fixtureBundle({ bundle_id: "sha256:stale" }));
    await new Promise((r) => setTimeout(r, 0));
    expect(screen.getByText("sha256:second")).toBeInTheDocument();
    expect(screen.queryByText("sha256:stale")).not.toBeInTheDocument();
  });
});
