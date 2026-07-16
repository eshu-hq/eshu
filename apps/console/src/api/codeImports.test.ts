import type { EshuApiClient } from "./client";
import { loadCodeImportCycles } from "./codeImports";

describe("loadCodeImportCycles", () => {
  it("shares only an identical in-flight request and fetches fresh truth after settle", async () => {
    let calls = 0;
    let resolveRequest: ((value: { data: object; error: null; truth: null }) => void) | undefined;
    const client = {
      post: async () => {
        calls += 1;
        return new Promise<{ data: object; error: null; truth: null }>((resolve) => {
          resolveRequest = resolve;
        });
      },
    } as unknown as EshuApiClient;

    const first = loadCodeImportCycles(client, "repository:r1", 6);
    const second = loadCodeImportCycles(client, "repository:r1", 6);

    expect(calls).toBe(1);
    resolveRequest?.({ data: { cycles: [] }, error: null, truth: null });
    await Promise.all([first, second]);

    const third = loadCodeImportCycles(client, "repository:r1", 6);
    expect(calls).toBe(2);
    resolveRequest?.({ data: { cycles: [] }, error: null, truth: null });
    await third;
  });

  it("does not share requests across repositories or clients", async () => {
    let calls = 0;
    const client = {
      post: async () => {
        calls += 1;
        return { data: { cycles: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;
    const otherClient = {
      post: async () => {
        calls += 1;
        return { data: { cycles: [] }, error: null, truth: null };
      },
    } as unknown as EshuApiClient;

    await Promise.all([
      loadCodeImportCycles(client, "repository:r1", 6),
      loadCodeImportCycles(client, "repository:r2", 6),
      loadCodeImportCycles(otherClient, "repository:r1", 6),
    ]);

    expect(calls).toBe(3);
  });
});
