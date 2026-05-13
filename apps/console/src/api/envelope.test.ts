import { isEshuErrorEnvelope, unwrapEnvelope } from "./envelope";
import type { EshuEnvelope } from "./envelope";

interface StoryPayload {
  readonly story: string;
}

describe("Eshu envelope helpers", () => {
  it("unwraps successful data while preserving truth metadata", () => {
    const envelope: EshuEnvelope<StoryPayload> = {
      data: { story: "checkout deploys through ArgoCD" },
      error: null,
      truth: {
        basis: "canonical_graph",
        capability: "platform_impact.context_overview",
        freshness: { state: "fresh" },
        level: "exact",
        profile: "local_authoritative",
        reason: "resolved from graph"
      }
    };

    expect(unwrapEnvelope(envelope)).toEqual({
      data: { story: "checkout deploys through ArgoCD" },
      truth: envelope.truth
    });
  });

  it("detects structured API errors", () => {
    const envelope: EshuEnvelope<StoryPayload> = {
      data: null,
      error: {
        code: "unsupported_capability",
        message: "requires authoritative graph"
      },
      truth: null
    };

    expect(isEshuErrorEnvelope(envelope)).toBe(true);
    expect(() => unwrapEnvelope(envelope)).toThrow("unsupported_capability");
  });
});
