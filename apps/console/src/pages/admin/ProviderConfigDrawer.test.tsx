// pages/admin/ProviderConfigDrawer.test.tsx
// TDD coverage for the Add/Edit provider drawer (#4967): read-only endpoint
// URIs, the save-draft -> test -> save-and-enable flow, write-only secret
// fields never being pre-filled or echoed back, and the OIDC/SAML kind
// toggle in create mode.
import { render, screen, fireEvent } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

import { ProviderConfigDrawer } from "./ProviderConfigDrawer";
import type { AdminProviderConfigItem } from "../../api/adminProviderConfig";
import type { EshuApiClient } from "../../api/client";

const PROVIDER_CONFIGS_PATH = "/api/v0/auth/admin/provider-configs";

function makeClient(postJson: (path: string, body: unknown) => Promise<unknown>): EshuApiClient {
  return { postJson: vi.fn(postJson) } as unknown as EshuApiClient;
}

function fillOidcRequired(): void {
  fireEvent.change(screen.getByLabelText("Issuer"), {
    target: { value: "https://idp.example.test" },
  });
  fireEvent.change(screen.getByLabelText("Client ID"), { target: { value: "client-1" } });
  fireEvent.change(screen.getByLabelText("Client secret"), { target: { value: "s3cret" } });
}

describe("ProviderConfigDrawer — focus and keyboard behavior", () => {
  it("focuses the close button on mount", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Close" })).toHaveFocus();
  });

  it("closes on Escape", () => {
    const client = makeClient(async () => ({}));
    const onClose = vi.fn();
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={onClose}
        onSaved={vi.fn()}
      />,
    );
    fireEvent.keyDown(window, { key: "Escape" });
    expect(onClose).toHaveBeenCalled();
  });

  it("is an aria-modal dialog", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect(screen.getByRole("dialog")).toHaveAttribute("aria-modal", "true");
  });
});

describe("ProviderConfigDrawer — create mode, OIDC (default)", () => {
  it("renders the deployment-wide OIDC redirect URI read-only", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    const redirectInput = screen.getByLabelText("OIDC redirect URI") as HTMLInputElement;
    expect(redirectInput.value).toBe("https://eshu.example.test/api/v0/auth/oidc/callback");
    expect(redirectInput).toHaveAttribute("readonly");
  });

  it("Save and Run test sign-in are disabled until required fields are filled", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Run test sign-in" })).toBeDisabled();
  });

  it("switching to SAML shows the per-provider ACS URL and SP entity ID read-only", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("radio", { name: "SAML" }));
    const acs = screen.getByLabelText("SAML ACS URL") as HTMLInputElement;
    const spEntity = screen.getByLabelText("SAML SP entity ID") as HTMLInputElement;
    expect(acs.value).toMatch(
      /^https:\/\/eshu\.example\.test\/api\/v0\/auth\/saml\/providers\/pc_[^/]+\/acs$/,
    );
    expect(spEntity.value).toMatch(
      /^https:\/\/eshu\.example\.test\/api\/v0\/auth\/saml\/providers\/pc_[^/]+$/,
    );
    expect(acs).toHaveAttribute("readonly");
    expect(spEntity).toHaveAttribute("readonly");
  });

  it("Run test sign-in saves a draft first, then calls test-connection, and surfaces the result", async () => {
    const calls: string[] = [];
    const client = makeClient(async (path) => {
      calls.push(path);
      if (path === PROVIDER_CONFIGS_PATH) {
        return {
          provider_config_id: "pc_test",
          revision_id: "rev_1",
          status: "draft",
          changed: true,
        };
      }
      if (path.endsWith("/test-connection")) {
        return { provider_config_id: "pc_test", ok: true, detail: "discovery reachable" };
      }
      throw new Error(`unexpected path ${path}`);
    });
    const onSaved = vi.fn();
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={onSaved}
      />,
    );
    fillOidcRequired();
    fireEvent.click(screen.getByRole("button", { name: "Run test sign-in" }));

    expect(await screen.findByText(/Test sign-in passed/)).toBeInTheDocument();
    expect(calls[0]).toBe(PROVIDER_CONFIGS_PATH);
    expect(calls[1]).toMatch(/\/test-connection$/);
    expect(onSaved).toHaveBeenCalled();
  });

  it("after a passing test, Save persists the draft and enables the provider", async () => {
    const calls: string[] = [];
    const client = makeClient(async (path) => {
      calls.push(path);
      if (path === PROVIDER_CONFIGS_PATH) {
        return {
          provider_config_id: "pc_test",
          revision_id: "rev_1",
          status: "draft",
          changed: true,
        };
      }
      if (path.endsWith("/test-connection")) {
        return { provider_config_id: "pc_test", ok: true, detail: "discovery reachable" };
      }
      if (path.endsWith("/enable")) {
        return {
          provider_config_id: "pc_test",
          revision_id: "rev_1",
          status: "active",
          changed: true,
        };
      }
      // Second save call (update) once the row already exists.
      return {
        provider_config_id: "pc_test",
        revision_id: "rev_1",
        status: "draft",
        changed: false,
      };
    });
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    fillOidcRequired();
    fireEvent.click(screen.getByRole("button", { name: "Run test sign-in" }));
    await screen.findByText(/Test sign-in passed/);

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Saved and enabled/)).toBeInTheDocument();
    expect(calls.some((p) => p.endsWith("/enable"))).toBe(true);
  });

  it("editing a field after a passing test clears the test result, so Save only saves a draft", async () => {
    const client = makeClient(async (path) => {
      if (path.endsWith("/test-connection")) {
        return { provider_config_id: "pc_test", ok: true, detail: "ok" };
      }
      if (path.endsWith("/enable")) {
        throw new Error("enable should not be called without a fresh passing test");
      }
      return {
        provider_config_id: "pc_test",
        revision_id: "rev_1",
        status: "draft",
        changed: true,
      };
    });
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    fillOidcRequired();
    fireEvent.click(screen.getByRole("button", { name: "Run test sign-in" }));
    await screen.findByText(/Test sign-in passed/);

    // Edit a field after the passing test — this invalidates the test result.
    fireEvent.change(screen.getByLabelText("Group claim"), { target: { value: "groups" } });

    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText(/Saved as draft/)).toBeInTheDocument();
  });
});

describe("ProviderConfigDrawer — create mode, GitHub (issue #5166, F-5)", () => {
  function switchToGithub(): void {
    fireEvent.click(screen.getByRole("radio", { name: "GitHub" }));
  }

  it("switching to GitHub shows the read-only OAuth2 callback URL", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    switchToGithub();
    const callback = screen.getByLabelText("GitHub callback URL") as HTMLInputElement;
    expect(callback.value).toBe("https://eshu.example.test/api/v0/auth/github/callback");
    expect(callback).toHaveAttribute("readonly");
  });

  it("Save/Test stay disabled until client id, secret, AND a non-empty allowed-orgs list are filled", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    switchToGithub();
    const save = () => screen.getByRole("button", { name: "Save" });
    expect(save()).toBeDisabled();

    fireEvent.change(screen.getByLabelText("GitHub client ID"), { target: { value: "gh-client" } });
    fireEvent.change(screen.getByLabelText("GitHub client secret"), {
      target: { value: "gh-secret" },
    });
    // Client id + secret filled but allowed-orgs still empty — must stay disabled
    // (mirrors the backend's mandatory non-empty allowed_orgs).
    expect(save()).toBeDisabled();

    fireEvent.change(screen.getByLabelText("Allowed organizations"), {
      target: { value: "my-org" },
    });
    expect(save()).toBeEnabled();
  });

  it("a whitespace/comma-only allowed-orgs value does not satisfy the required check", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    switchToGithub();
    fireEvent.change(screen.getByLabelText("GitHub client ID"), { target: { value: "gh-client" } });
    fireEvent.change(screen.getByLabelText("GitHub client secret"), {
      target: { value: "gh-secret" },
    });
    fireEvent.change(screen.getByLabelText("Allowed organizations"), {
      target: { value: " , , " },
    });
    expect(screen.getByRole("button", { name: "Save" })).toBeDisabled();
  });

  it("Run test sign-in posts a github create payload with provider_kind, allowed_orgs, and the callback redirect", async () => {
    let createBody: Record<string, unknown> | null = null;
    const client = makeClient(async (path, body) => {
      if (path === PROVIDER_CONFIGS_PATH) {
        createBody = body as Record<string, unknown>;
        return {
          provider_config_id: "pc_gh",
          revision_id: "rev_1",
          status: "draft",
          changed: true,
        };
      }
      if (path.endsWith("/test-connection")) {
        return { provider_config_id: "pc_gh", ok: true, detail: "github api reachable" };
      }
      throw new Error(`unexpected path ${path}`);
    });
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    switchToGithub();
    fireEvent.change(screen.getByLabelText("GitHub client ID"), { target: { value: "gh-client" } });
    fireEvent.change(screen.getByLabelText("GitHub client secret"), {
      target: { value: "gh-secret" },
    });
    fireEvent.change(screen.getByLabelText("Allowed organizations"), {
      target: { value: "My-Org, other-org" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Run test sign-in" }));

    expect(await screen.findByText(/Test sign-in passed/)).toBeInTheDocument();
    expect(createBody).toMatchObject({
      provider_kind: "github",
      client_id: "gh-client",
      client_secret: "gh-secret",
      allowed_orgs: ["My-Org", "other-org"],
      redirect_url: "https://eshu.example.test/api/v0/auth/github/callback",
    });
  });

  it("never pre-fills the write-only client secret when editing an existing github provider", () => {
    const client = makeClient(async () => ({}));
    const existingGithub: AdminProviderConfigItem = {
      provider_config_id: "pc_gh_existing",
      provider_kind: "external_github",
      status: "active",
      configuration: {
        client_id: "gh-client",
        base_url: "https://github.example.com",
        allowed_orgs: ["my-org"],
        scopes: ["read:org", "user:email"],
      },
      has_secret: true,
      secret_fingerprint: "ghfp123",
      shadowed_by_environment: false,
      managed_by: "database",
    };
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        existing={existingGithub}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect((screen.getByLabelText("GitHub client ID") as HTMLInputElement).value).toBe("gh-client");
    expect((screen.getByLabelText("Allowed organizations") as HTMLInputElement).value).toBe(
      "my-org",
    );
    expect((screen.getByLabelText("GitHub client secret") as HTMLInputElement).value).toBe("");
    // The kind toggle is not rendered in edit mode, so GitHub fields render
    // directly from the existing provider's kind.
    expect(screen.queryByRole("radio", { name: "GitHub" })).not.toBeInTheDocument();
    expect(document.body.innerHTML).not.toContain("ghfp123");
  });
});

describe("ProviderConfigDrawer — edit mode", () => {
  const existing: AdminProviderConfigItem = {
    provider_config_id: "pc_existing",
    provider_kind: "external_oidc",
    status: "active",
    configuration: {
      issuer: "https://idp.example.test",
      client_id: "client-1",
      scopes: ["openid", "email"],
      group_claim: "groups",
    },
    has_secret: true,
    secret_fingerprint: "abc123",
    shadowed_by_environment: false,
    managed_by: "database",
  };

  it("seeds non-secret fields but never pre-fills the write-only secret", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        existing={existing}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect((screen.getByLabelText("Issuer") as HTMLInputElement).value).toBe(
      "https://idp.example.test",
    );
    expect((screen.getByLabelText("Client secret") as HTMLInputElement).value).toBe("");
    const body = document.body.innerHTML;
    expect(body).not.toContain("abc123");
  });

  it("does not render the OIDC/SAML kind toggle when editing an existing provider", () => {
    const client = makeClient(async () => ({}));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        existing={existing}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect(screen.queryByRole("radio", { name: "SAML" })).not.toBeInTheDocument();
  });

  it("editing and saving an already-active provider shows Draft, not a stale Active (backend resets status on every update)", async () => {
    // Regression: go/internal/storage/postgres identity_provider_config_writes.go's
    // UpdateProviderConfig returns the PRE-transaction status even though the
    // same transaction's activateProviderConfigActiveRevisionQuery always
    // resets the row's actual status to 'draft'. The write result here
    // deliberately echoes the stale "active" value a real backend response
    // could carry, to prove the drawer does not trust it.
    const client = makeClient(async () => ({
      provider_config_id: "pc_existing",
      revision_id: "rev_2",
      status: "active",
      changed: true,
    }));
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        existing={existing}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />,
    );
    expect(screen.getByText("active")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Client secret"), { target: { value: "new-secret" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));
    expect(await screen.findByText("draft")).toBeInTheDocument();
  });

  it("calls onClose when the close button is clicked", () => {
    const client = makeClient(async () => ({}));
    const onClose = vi.fn();
    render(
      <ProviderConfigDrawer
        client={client}
        baseUrl="https://eshu.example.test"
        existing={existing}
        onClose={onClose}
        onSaved={vi.fn()}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalled();
  });
});
