import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { CloudDriftPage } from "./CloudDriftPage";
import type { EshuApiClient } from "../api/client";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

function envelope(data: unknown) {
  return {
    data,
    error: null,
    truth: {
      capability: "cloud_runtime_drift.readback.list",
      freshness: { state: "fresh" },
      level: "exact",
      profile: "production",
    },
  };
}

function page(path: string) {
  if (path === "/api/v0/cloud/runtime-drift/findings") {
    return envelope({
      drift_findings: [
        {
          fact_id: "fact:multi:1",
          cloud_resource_uid: "cloud-resource:s3:payments-prod",
          provider: "aws",
          safety_gate: { outcome: "read_only_allowed" },
        },
      ],
      limit: 50,
      next_offset: 50,
      offset: 0,
      total_findings_count: 51,
      truncated: true,
    });
  }
  if (path === "/api/v0/iac/unmanaged-resources") {
    return envelope({
      findings: [
        {
          id: "finding:aws:1",
          arn: "arn:aws:s3:::payments-prod",
          account_id: "123456789012",
          safety_gate: { outcome: "read_only_allowed" },
        },
      ],
    });
  }
  return envelope({ drift_findings: [] });
}

function clientWithOverrides(overrides: {
  readonly explanation?: Promise<unknown>;
  readonly nextPage?: Promise<unknown>;
  readonly packet?: Promise<unknown>;
}): EshuApiClient {
  return {
    get: vi.fn(async () => overrides.packet ?? envelope({})),
    post: vi.fn(async (path: string, body: unknown) => {
      if (path === "/api/v0/iac/management-status/explain" && overrides.explanation) {
        return overrides.explanation;
      }
      if (
        path === "/api/v0/cloud/runtime-drift/findings" &&
        (body as { readonly offset?: number }).offset === 50 &&
        overrides.nextPage
      ) {
        return overrides.nextPage;
      }
      return page(path);
    }),
  } as unknown as EshuApiClient;
}

function renderScoped(client: EshuApiClient): void {
  render(
    <MemoryRouter initialEntries={["/cloud-drift?account_id=123456789012&provider=aws"]}>
      <CloudDriftPage client={client} />
    </MemoryRouter>,
  );
}

describe("CloudDriftPage stale responses", () => {
  it("starts a fresh load when the applied filters are submitted unchanged", async () => {
    const client = clientWithOverrides({});
    renderScoped(client);

    expect(await screen.findByText("cloud-resource:s3:payments-prod")).toBeInTheDocument();
    const findingsCalls = (): number =>
      vi
        .mocked(client.post)
        .mock.calls.filter(
          ([path, body]) =>
            path === "/api/v0/cloud/runtime-drift/findings" &&
            (body as { readonly offset?: number }).offset === 0,
        ).length;
    expect(findingsCalls()).toBe(1);

    fireEvent.click(screen.getByRole("button", { name: "Load drift findings" }));

    await waitFor(() => expect(findingsCalls()).toBe(2));
  });

  it("discards explanation and packet responses after their scope is reset", async () => {
    const explanation = deferred<unknown>();
    const packet = deferred<unknown>();
    renderScoped(clientWithOverrides({ explanation: explanation.promise, packet: packet.promise }));

    fireEvent.click(
      await screen.findByRole("button", {
        name: "Explain status for arn:aws:s3:::payments-prod",
      }),
    );
    fireEvent.click(screen.getByRole("button", { name: "Load drift evidence packet" }));
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    await act(async () => {
      explanation.resolve(envelope({ story: "STALE SCOPE EXPLANATION" }));
      packet.reject(new Error("STALE SCOPE PACKET ERROR"));
      await Promise.allSettled([explanation.promise, packet.promise]);
    });

    expect(
      screen.getByText("Enter a scope or account to load drift evidence."),
    ).toBeInTheDocument();
    expect(screen.queryByText("STALE SCOPE EXPLANATION")).not.toBeInTheDocument();
    expect(screen.queryByText("STALE SCOPE PACKET ERROR")).not.toBeInTheDocument();
    expect(screen.queryByText("Loading packet...")).not.toBeInTheDocument();
  });

  it("discards a previous scope's pagination response after reset", async () => {
    const nextPage = deferred<unknown>();
    renderScoped(clientWithOverrides({ nextPage: nextPage.promise }));

    fireEvent.click(await screen.findByRole("button", { name: "Next multi-cloud drift page" }));
    fireEvent.click(screen.getByRole("button", { name: "Reset" }));
    await act(async () => {
      nextPage.resolve(
        envelope({
          drift_findings: [{ cloud_resource_uid: "STALE PAGINATION RESOURCE" }],
        }),
      );
      await nextPage.promise;
    });

    expect(
      screen.getByText("Enter a scope or account to load drift evidence."),
    ).toBeInTheDocument();
    expect(screen.queryByText("STALE PAGINATION RESOURCE")).not.toBeInTheDocument();
  });

  it("clears prior evidence when an empty scope is applied", async () => {
    renderScoped(clientWithOverrides({}));

    expect(await screen.findByText("cloud-resource:s3:payments-prod")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Account ID filter"), { target: { value: "" } });
    fireEvent.change(screen.getByLabelText("Provider filter"), { target: { value: "" } });
    fireEvent.click(screen.getByRole("button", { name: "Load drift findings" }));

    expect(
      await screen.findByText("Enter a scope or account to load drift evidence."),
    ).toBeInTheDocument();
    expect(screen.queryByText("cloud-resource:s3:payments-prod")).not.toBeInTheDocument();
  });
});
