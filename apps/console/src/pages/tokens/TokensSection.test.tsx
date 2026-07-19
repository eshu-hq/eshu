// pages/tokens/TokensSection.test.tsx
// Component tests for the self-service token panel (issue #5164):
//   - create requires a label, reveals the raw token exactly once, and
//     refetches the list on Done
//   - rotate requires confirmation, reveals the replacement token once
//   - revoke requires confirmation and takes effect (refetch reflects it)
//   - never renders token_hash / api_token outside the just-created reveal
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";

import { TokensSection } from "./TokensSection";
import type { EshuApiClient } from "../../api/client";
import { EshuApiHttpError } from "../../api/client";
import type { APITokenItem } from "../../api/userProfile";

const NOW = "2026-06-24T10:00:00Z";

const oneToken: APITokenItem = {
  token_id: "tok-001",
  token_class: "personal",
  display_label: "owner laptop",
  issued_at: NOW,
  expires_at: undefined,
};

beforeEach(() => {
  vi.stubGlobal(
    "confirm",
    vi.fn(() => true),
  );
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("TokensSection — unavailable / empty", () => {
  it("renders unavailable note and no table when unavailable", () => {
    render(<TokensSection tokens={[]} unavailable onChanged={() => {}} />);
    expect(screen.getByText("Tokens unavailable from this source.")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("renders an empty note when there are no tokens", () => {
    render(<TokensSection tokens={[]} unavailable={false} onChanged={() => {}} />);
    expect(screen.getByText("No tokens found.")).toBeInTheDocument();
  });

  it("does not render the create control without a client (read-only view)", () => {
    render(<TokensSection tokens={[oneToken]} unavailable={false} onChanged={() => {}} />);
    expect(screen.queryByRole("button", { name: "Create token" })).not.toBeInTheDocument();
  });
});

describe("TokensSection — create", () => {
  it("requires a label: Create is disabled until one is typed", async () => {
    const client = { postJson: vi.fn() } as unknown as EshuApiClient;
    render(<TokensSection client={client} tokens={[]} unavailable={false} onChanged={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Create token" }));
    const createButton = screen.getByRole("button", { name: "Create" });
    expect(createButton).toBeDisabled();
    fireEvent.change(screen.getByLabelText("Label"), { target: { value: "laptop" } });
    expect(createButton).toBeEnabled();
  });

  it("creates a token, reveals the raw value exactly once, and refetches on Done", async () => {
    const postJson = vi.fn(async () => ({
      token_id: "tok-new",
      api_token: "raw-generated-token",
      issued_at: NOW,
    }));
    const client = { postJson } as unknown as EshuApiClient;
    const onChanged = vi.fn();
    render(<TokensSection client={client} tokens={[]} unavailable={false} onChanged={onChanged} />);

    fireEvent.click(await screen.findByRole("button", { name: "Create token" }));
    fireEvent.change(screen.getByLabelText("Label"), { target: { value: "laptop" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));

    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens", {
        token_class: "personal",
        display_label: "laptop",
      }),
    );

    const revealed = await screen.findByLabelText("API token");
    expect(revealed).toHaveValue("raw-generated-token");
    const doneButton = screen.getByRole("button", { name: "Done" });
    expect(doneButton).toBeDisabled();

    fireEvent.click(
      screen.getByRole("checkbox", {
        name: /I've saved this token and understand it won't be shown again/,
      }),
    );
    expect(doneButton).toBeEnabled();

    fireEvent.click(doneButton);
    expect(onChanged).toHaveBeenCalledTimes(1);
    // Reveal-once: dismissing must not leave the raw value on screen.
    expect(screen.queryByLabelText("API token")).not.toBeInTheDocument();
  });

  it("shows a clear message on a 403 instead of crashing", async () => {
    const client = {
      postJson: async () => {
        throw new EshuApiHttpError(403);
      },
    } as unknown as EshuApiClient;
    render(<TokensSection client={client} tokens={[]} unavailable={false} onChanged={() => {}} />);
    fireEvent.click(await screen.findByRole("button", { name: "Create token" }));
    fireEvent.change(screen.getByLabelText("Label"), { target: { value: "laptop" } });
    fireEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(
      await screen.findByText(
        "You don't have permission to create API tokens. Ask a tenant admin.",
      ),
    ).toBeInTheDocument();
  });
});

describe("TokensSection — rotate", () => {
  it("rotates a token after confirmation and reveals the replacement once", async () => {
    const postJson = vi.fn(async () => ({
      token_id: "tok-002",
      api_token: "raw-rotated-token",
      issued_at: NOW,
    }));
    const client = { postJson } as unknown as EshuApiClient;
    const onChanged = vi.fn();
    render(
      <TokensSection
        client={client}
        tokens={[oneToken]}
        unavailable={false}
        onChanged={onChanged}
      />,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Rotate" }));
    await waitFor(() =>
      expect(postJson).toHaveBeenCalledWith("/api/v0/auth/local/api-tokens/tok-001/rotate", {}),
    );
    expect(await screen.findByLabelText("API token")).toHaveValue("raw-rotated-token");
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("does nothing when the confirmation is declined", async () => {
    vi.stubGlobal(
      "confirm",
      vi.fn(() => false),
    );
    const postJson = vi.fn();
    const client = { postJson } as unknown as EshuApiClient;
    render(
      <TokensSection
        client={client}
        tokens={[oneToken]}
        unavailable={false}
        onChanged={() => {}}
      />,
    );
    fireEvent.click(await screen.findByRole("button", { name: "Rotate" }));
    expect(postJson).not.toHaveBeenCalled();
  });
});

describe("TokensSection — revoke", () => {
  it("revokes a token after confirmation and the effect is reflected on refetch", async () => {
    const postNoContent = vi.fn(async () => undefined);
    const client = { postNoContent } as unknown as EshuApiClient;
    const onChanged = vi.fn();
    render(
      <TokensSection
        client={client}
        tokens={[oneToken]}
        unavailable={false}
        onChanged={onChanged}
      />,
    );

    fireEvent.click(await screen.findByRole("button", { name: "Revoke" }));
    await waitFor(() =>
      expect(postNoContent).toHaveBeenCalledWith(
        "/api/v0/auth/local/api-tokens/tok-001/revoke",
        {},
      ),
    );
    expect(await screen.findByText("Token tok-001 revoked.")).toBeInTheDocument();
    expect(onChanged).toHaveBeenCalledTimes(1);
  });

  it("disables both actions for an already-revoked token", () => {
    const client = { postJson: vi.fn(), postNoContent: vi.fn() } as unknown as EshuApiClient;
    render(
      <TokensSection
        client={client}
        tokens={[{ ...oneToken, revoked_at: NOW }]}
        unavailable={false}
        onChanged={() => {}}
      />,
    );
    expect(screen.getByRole("button", { name: "Rotate" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Revoke" })).toBeDisabled();
  });
});

describe("TokensSection — never leaks secrets outside the just-created reveal", () => {
  it("the base table never contains token_hash or api_token", () => {
    render(<TokensSection tokens={[oneToken]} unavailable={false} onChanged={() => {}} />);
    const body = document.body.innerHTML;
    expect(body).not.toContain("token_hash");
    expect(body).not.toContain("api_token");
  });
});
