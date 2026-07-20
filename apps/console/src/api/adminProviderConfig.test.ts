// api/adminProviderConfig.test.ts — TDD coverage for the #4966 provider-config
// CRUD loaders/mutators consumed by the #4967 admin Identity & Access UI.
import { describe, it, expect, vi } from "vitest";

import {
  loadProviderConfigs,
  loadProviderConfigRevisions,
  createProviderConfig,
  updateProviderConfig,
  enableProviderConfig,
  disableProviderConfig,
  testProviderConfigConnection,
  toWireBody,
  toFormKind,
  newClientProviderConfigId,
  oidcRedirectUri,
  githubCallbackUri,
  samlAcsUrl,
  samlServiceProviderEntityId,
  deriveProviderLabel,
} from "./adminProviderConfig";
import type { AdminProviderConfigItem } from "./adminProviderConfig";
import type { EshuApiClient } from "./client";

describe("loadProviderConfigs", () => {
  it("returns live items on success", async () => {
    const client = {
      getJson: vi.fn(async () => ({
        provider_configs: [
          {
            provider_config_id: "pc_1",
            provider_kind: "external_oidc",
            status: "active",
            configuration: { issuer: "https://idp.example.test" },
            has_secret: true,
            shadowed_by_environment: false,
            managed_by: "database",
          },
        ],
        truncated: false,
      })),
    } as unknown as EshuApiClient;
    const result = await loadProviderConfigs(client);
    expect(result.provenance).toBe("live");
    expect(result.items).toHaveLength(1);
    expect(client.getJson).toHaveBeenCalledWith("/api/v0/auth/admin/provider-configs");
  });

  it("returns unavailable with an empty list on failure (never fabricated rows)", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new Error("503");
      }),
    } as unknown as EshuApiClient;
    const result = await loadProviderConfigs(client);
    expect(result.provenance).toBe("unavailable");
    expect(result.items).toEqual([]);
  });
});

describe("loadProviderConfigRevisions", () => {
  it("fetches the revisions path for the given id", async () => {
    const client = {
      getJson: vi.fn(async () => ({
        revisions: [{ revision_id: "rev_1", status: "active", has_secret: true }],
      })),
    } as unknown as EshuApiClient;
    const result = await loadProviderConfigRevisions(client, "pc_1");
    expect(client.getJson).toHaveBeenCalledWith(
      "/api/v0/auth/admin/provider-configs/pc_1/revisions",
    );
    expect(result.revisions).toHaveLength(1);
  });

  it("returns unavailable on failure", async () => {
    const client = {
      getJson: vi.fn(async () => {
        throw new Error("boom");
      }),
    } as unknown as EshuApiClient;
    const result = await loadProviderConfigRevisions(client, "pc_1");
    expect(result.provenance).toBe("unavailable");
    expect(result.revisions).toEqual([]);
  });
});

describe("toWireBody", () => {
  it("maps an OIDC form input to the exact backend field names", () => {
    const body = toWireBody({
      kind: "oidc",
      providerConfigId: "pc_1",
      issuer: "https://idp.example.test",
      clientId: "client-1",
      clientSecret: "s3cret",
      scopes: ["openid", "email"],
      groupClaim: "groups",
      redirectUrl: "https://eshu.example.test/api/v0/auth/oidc/callback",
    });
    expect(body).toEqual({
      provider_kind: "oidc",
      provider_config_id: "pc_1",
      issuer: "https://idp.example.test",
      client_id: "client-1",
      client_secret: "s3cret",
      scopes: ["openid", "email"],
      group_claim: "groups",
      redirect_url: "https://eshu.example.test/api/v0/auth/oidc/callback",
    });
  });

  it("maps a SAML form input to the exact backend field names", () => {
    const body = toWireBody({
      kind: "saml",
      metadataUrl: "https://idp.example.test/metadata",
      metadataXml: "",
      entityId: "https://idp.example.test/entity",
      groupAttribute: "group",
      serviceProviderEntityId: "https://eshu.example.test/api/v0/auth/saml/providers/pc_2",
      serviceProviderAcsUrl: "https://eshu.example.test/api/v0/auth/saml/providers/pc_2/acs",
      spPrivateKey: "priv",
      spCertificate: "cert",
    });
    expect(body).toEqual({
      provider_kind: "saml",
      metadata_url: "https://idp.example.test/metadata",
      metadata_xml: "",
      entity_id: "https://idp.example.test/entity",
      group_attribute: "group",
      service_provider_entity_id: "https://eshu.example.test/api/v0/auth/saml/providers/pc_2",
      service_provider_acs_url: "https://eshu.example.test/api/v0/auth/saml/providers/pc_2/acs",
      sp_private_key: "priv",
      sp_certificate: "cert",
    });
  });

  it("maps a GitHub form input to the exact backend field names (issue #5166)", () => {
    const body = toWireBody({
      kind: "github",
      providerConfigId: "pc_gh",
      clientId: "gh-client-1",
      clientSecret: "gh-s3cret",
      baseUrl: "",
      apiBaseUrl: "",
      scopes: ["read:org", "user:email"],
      allowedOrgs: ["my-org"],
      redirectUrl: "https://eshu.example.test/api/v0/auth/github/callback",
    });
    expect(body).toEqual({
      provider_kind: "github",
      provider_config_id: "pc_gh",
      client_id: "gh-client-1",
      client_secret: "gh-s3cret",
      base_url: "",
      api_base_url: "",
      scopes: ["read:org", "user:email"],
      allowed_orgs: ["my-org"],
      redirect_url: "https://eshu.example.test/api/v0/auth/github/callback",
    });
  });

  it("omits provider_config_id when absent (update path relies on the URL id)", () => {
    const body = toWireBody({
      kind: "oidc",
      issuer: "https://idp.example.test",
      clientId: "c",
      clientSecret: "s",
      scopes: [],
      groupClaim: "",
      redirectUrl: "",
    });
    expect(body).not.toHaveProperty("provider_config_id");
  });
});

describe("toFormKind", () => {
  it("maps external_saml to saml, external_github to github, and everything else to oidc", () => {
    expect(toFormKind("external_saml")).toBe("saml");
    expect(toFormKind("external_github")).toBe("github");
    expect(toFormKind("external_oidc")).toBe("oidc");
    expect(toFormKind(undefined)).toBe("oidc");
  });
});

describe("githubCallbackUri", () => {
  it("builds the fixed deployment-wide GitHub callback path", () => {
    expect(githubCallbackUri("https://eshu.example.test/")).toBe(
      "https://eshu.example.test/api/v0/auth/github/callback",
    );
  });
});

describe("write mutators", () => {
  it("createProviderConfig posts to the collection path and returns ok:true", async () => {
    const postJson = vi.fn(async () => ({
      provider_config_id: "pc_1",
      revision_id: "rev_1",
      status: "draft",
      changed: true,
    }));
    const client = { postJson } as unknown as EshuApiClient;
    const outcome = await createProviderConfig(client, {
      kind: "oidc",
      issuer: "https://idp.example.test",
      clientId: "c",
      clientSecret: "s",
      scopes: [],
      groupClaim: "",
      redirectUrl: "",
    });
    expect(outcome.ok).toBe(true);
    expect(outcome.result?.provider_config_id).toBe("pc_1");
    expect(postJson).toHaveBeenCalledWith(
      "/api/v0/auth/admin/provider-configs",
      expect.objectContaining({ provider_kind: "oidc" }),
    );
  });

  it("createProviderConfig returns ok:false with a message on failure, never throws", async () => {
    const client = {
      postJson: vi.fn(async () => {
        throw new Error("a provider config already exists for this tenant, kind, and identity key");
      }),
    } as unknown as EshuApiClient;
    const outcome = await createProviderConfig(client, {
      kind: "oidc",
      issuer: "i",
      clientId: "c",
      clientSecret: "s",
      scopes: [],
      groupClaim: "",
      redirectUrl: "",
    });
    expect(outcome.ok).toBe(false);
    expect(outcome.errorMessage).toContain("already exists");
  });

  it("updateProviderConfig posts to the id path", async () => {
    const postJson = vi.fn(async () => ({
      provider_config_id: "pc_1",
      revision_id: "rev_2",
      status: "draft",
      changed: true,
    }));
    const client = { postJson } as unknown as EshuApiClient;
    await updateProviderConfig(client, "pc_1", {
      kind: "oidc",
      issuer: "i",
      clientId: "c",
      clientSecret: "s",
      scopes: [],
      groupClaim: "",
      redirectUrl: "",
    });
    expect(postJson).toHaveBeenCalledWith(
      "/api/v0/auth/admin/provider-configs/pc_1",
      expect.objectContaining({ provider_kind: "oidc" }),
    );
  });

  it("enableProviderConfig and disableProviderConfig post to the right action paths", async () => {
    const postJson = vi.fn(async () => ({
      provider_config_id: "pc_1",
      revision_id: "rev_1",
      status: "active",
      changed: true,
    }));
    const client = { postJson } as unknown as EshuApiClient;
    await enableProviderConfig(client, "pc_1");
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/provider-configs/pc_1/enable", {});
    await disableProviderConfig(client, "pc_1");
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/provider-configs/pc_1/disable", {});
  });

  it("URL-encodes the provider_config_id path segment", async () => {
    const postJson = vi.fn(async () => ({
      provider_config_id: "pc/1",
      revision_id: "rev_1",
      status: "draft",
      changed: true,
    }));
    const client = { postJson } as unknown as EshuApiClient;
    await disableProviderConfig(client, "pc/1");
    expect(postJson).toHaveBeenCalledWith("/api/v0/auth/admin/provider-configs/pc%2F1/disable", {});
  });
});

describe("testProviderConfigConnection", () => {
  it("reports ran:true and the server's ok/detail on success", async () => {
    const client = {
      postJson: vi.fn(async () => ({
        provider_config_id: "pc_1",
        ok: true,
        detail: "discovery reachable",
      })),
    } as unknown as EshuApiClient;
    const result = await testProviderConfigConnection(client, "pc_1");
    expect(result).toEqual({ ok: true, detail: "discovery reachable", ran: true });
  });

  it("reports ran:false and never throws when the call itself fails", async () => {
    const client = {
      postJson: vi.fn(async () => {
        throw new Error("network error");
      }),
    } as unknown as EshuApiClient;
    const result = await testProviderConfigConnection(client, "pc_1");
    expect(result.ok).toBe(false);
    expect(result.ran).toBe(false);
    expect(result.detail).toBe("network error");
  });
});

describe("newClientProviderConfigId", () => {
  it("generates a unique pc_-prefixed id", () => {
    const a = newClientProviderConfigId();
    const b = newClientProviderConfigId();
    expect(a).toMatch(/^pc_/);
    expect(a).not.toBe(b);
  });
});

describe("endpoint URI helpers", () => {
  it("oidcRedirectUri is deployment-wide (no provider id in the path)", () => {
    expect(oidcRedirectUri("https://eshu.example.test/")).toBe(
      "https://eshu.example.test/api/v0/auth/oidc/callback",
    );
    expect(oidcRedirectUri("https://eshu.example.test")).toBe(
      "https://eshu.example.test/api/v0/auth/oidc/callback",
    );
  });

  it("samlAcsUrl and samlServiceProviderEntityId are scoped by provider_config_id", () => {
    expect(samlAcsUrl("https://eshu.example.test", "pc_1")).toBe(
      "https://eshu.example.test/api/v0/auth/saml/providers/pc_1/acs",
    );
    expect(samlServiceProviderEntityId("https://eshu.example.test", "pc_1")).toBe(
      "https://eshu.example.test/api/v0/auth/saml/providers/pc_1",
    );
  });
});

describe("deriveProviderLabel", () => {
  function item(overrides: Partial<AdminProviderConfigItem>): AdminProviderConfigItem {
    return {
      provider_config_id: "pc_1",
      provider_kind: "external_oidc",
      status: "active",
      configuration: {},
      has_secret: true,
      shadowed_by_environment: false,
      managed_by: "database",
      ...overrides,
    };
  }

  it("uses the issuer for an OIDC provider", () => {
    expect(
      deriveProviderLabel(item({ configuration: { issuer: "https://idp.example.test" } })),
    ).toBe("https://idp.example.test");
  });

  it("uses the entity id for a SAML provider", () => {
    expect(
      deriveProviderLabel(
        item({
          provider_kind: "external_saml",
          configuration: { entity_id: "https://idp.example.test/entity" },
        }),
      ),
    ).toBe("https://idp.example.test/entity");
  });

  it("uses the base URL for a GitHub Enterprise Server provider (issue #5166)", () => {
    expect(
      deriveProviderLabel(
        item({
          provider_kind: "external_github",
          configuration: { base_url: "https://github.example.com" },
        }),
      ),
    ).toBe("https://github.example.com");
  });

  it("falls back to the opaque id for a github.com provider with a blank base URL", () => {
    expect(
      deriveProviderLabel(
        item({ provider_kind: "external_github", configuration: { base_url: "" } }),
      ),
    ).toBe("pc_1");
  });

  it("falls back to the opaque id when configuration carries no usable label", () => {
    expect(deriveProviderLabel(item({ configuration: {} }))).toBe("pc_1");
  });
});
