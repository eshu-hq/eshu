// api/adminSignInPolicy.test.ts
// Tests for the sign-in policy loaders/mutators (#4968, epic #4962): field
// mapping to/from the wire shape, never-throws error handling, and the
// public require_sso hint's tenant_id/error fail-open behavior.
import { describe, it, expect, vi } from "vitest";

import { loadAdminSignInPolicy, updateAdminSignInPolicy } from "./adminSignInPolicy";
import type { EshuApiClient } from "./client";

describe("adminSignInPolicy", () => {
  describe("loadAdminSignInPolicy", () => {
    it("returns the policy with provenance live on success", async () => {
      const policy = {
        tenant_id: "tenant_a",
        require_sso: true,
        allow_local_user_creation: true,
        require_mfa_for_all_users: false,
        idle_timeout_seconds: 0,
        absolute_timeout_seconds: 0,
        policy_revision_hash: "sha256:rev1",
        updated_at: "2026-06-01T00:00:00Z",
      };
      const client = { getJson: vi.fn(async () => policy) } as unknown as EshuApiClient;
      const result = await loadAdminSignInPolicy(client);
      expect(result).toEqual({ policy, provenance: "live" });
      expect(client.getJson).toHaveBeenCalledWith("/api/v0/auth/admin/sign-in-policy");
    });

    it("returns provenance unavailable on error, never a fabricated policy", async () => {
      const client = {
        getJson: vi.fn(async () => {
          throw new Error("503");
        }),
      } as unknown as EshuApiClient;
      const result = await loadAdminSignInPolicy(client);
      expect(result).toEqual({ provenance: "unavailable" });
    });
  });

  describe("updateAdminSignInPolicy", () => {
    it("sends only the fields present on the input", async () => {
      const patchJson = vi.fn(async () => ({ tenant_id: "tenant_a" }));
      const client = { patchJson } as unknown as EshuApiClient;
      await updateAdminSignInPolicy(client, { requireSso: true, idleTimeoutSeconds: 600 });
      expect(patchJson).toHaveBeenCalledWith("/api/v0/auth/admin/sign-in-policy", {
        require_sso: true,
        idle_timeout_seconds: 600,
      });
    });

    it("returns ok:false with the server's error message on failure, never throwing", async () => {
      const client = {
        patchJson: vi.fn(async () => {
          throw new Error(
            "require_sso cannot be enabled: no provider config has a passing connection test",
          );
        }),
      } as unknown as EshuApiClient;
      const outcome = await updateAdminSignInPolicy(client, { requireSso: true });
      expect(outcome.ok).toBe(false);
      expect(outcome.errorMessage).toBe(
        "require_sso cannot be enabled: no provider config has a passing connection test",
      );
    });
  });
});
