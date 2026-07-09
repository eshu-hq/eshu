// AuthGate.test.tsx — TDD tests for the login-vs-setup-wizard routing gate
// (#4965).
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it, vi } from "vitest";

import { AuthGate } from "./AuthGate";
import type { EshuApiClient } from "../api/client";

function makeClient(getJsonImpl: (path: string) => Promise<unknown>): EshuApiClient {
  return {
    getJson: vi.fn(getJsonImpl),
  } as unknown as EshuApiClient;
}

describe("AuthGate", () => {
  it("renders SetupPage when needs_setup is true", async () => {
    const client = makeClient(async () => ({ needs_setup: true, bootstrap_mode: "generated" }));
    render(
      <MemoryRouter>
        <AuthGate client={client} onSuccess={vi.fn()} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(screen.getByText(/claim this instance/i)).toBeInTheDocument());
  });

  it("renders LoginPage when needs_setup is false", async () => {
    const client = makeClient(async (path) => {
      if (path === "/api/v0/auth/setup-state")
        return { needs_setup: false, bootstrap_mode: "generated" };
      return { providers: [] };
    });
    render(
      <MemoryRouter>
        <AuthGate client={client} onSuccess={vi.fn()} />
      </MemoryRouter>,
    );
    await waitFor(() =>
      expect(screen.getByText("Sign in", { selector: "h1" })).toBeInTheDocument(),
    );
  });

  it("falls back to LoginPage when the setup-state check fails", async () => {
    const client = makeClient(async () => {
      throw new Error("network error");
    });
    render(
      <MemoryRouter>
        <AuthGate client={client} onSuccess={vi.fn()} />
      </MemoryRouter>,
    );
    await waitFor(() =>
      expect(screen.getByText("Sign in", { selector: "h1" })).toBeInTheDocument(),
    );
  });
});
