// appBoot.test.ts — boot-phase helper behavior.
import { afterEach, describe, expect, it, vi } from "vitest";

vi.mock("./api/authSession", () => ({
  loadCurrentSession: vi.fn()
}));
vi.mock("./api/eshuConsoleLive", () => ({
  loadConsoleSnapshot: vi.fn()
}));
vi.mock("./api/repoCatalog", () => ({
  loadRepositories: vi.fn(async () => [])
}));
vi.mock("./config/environment", () => ({
  saveConsoleEnvironment: vi.fn()
}));
vi.mock("./console/liveModel", () => ({
  modelFromSnapshot: vi.fn(() => ({}))
}));

import { bootFromKey } from "./appBoot";
import { loadCurrentSession } from "./api/authSession";
import { loadConsoleSnapshot } from "./api/eshuConsoleLive";
import { saveConsoleEnvironment } from "./config/environment";

describe("bootFromKey", () => {
  afterEach(() => {
    vi.clearAllMocks();
  });

  it("returns null without reading data when no key and no session exist (#3685 P2)", async () => {
    vi.mocked(loadCurrentSession).mockResolvedValue(null);

    const result = await bootFromKey("https://api.example/", "");

    expect(result).toBeNull();
    // Must NOT read the snapshot unauthenticated — that would 401 and strand
    // the user in an error state instead of routing to login.
    expect(loadConsoleSnapshot).not.toHaveBeenCalled();
    // The selected base is persisted so login renders for this deployment.
    expect(saveConsoleEnvironment).toHaveBeenCalledWith(
      expect.objectContaining({ mode: "private", apiBaseUrl: "https://api.example/", apiKey: "" })
    );
  });

  it("loads data when an existing cookie session is found for an empty key", async () => {
    vi.mocked(loadCurrentSession).mockResolvedValue({
      auth: { mode: "browser_session", all_scopes: true }
    });
    vi.mocked(loadConsoleSnapshot).mockResolvedValue({} as never);

    const result = await bootFromKey("https://api.example/", "");

    expect(result).not.toBeNull();
    expect(loadConsoleSnapshot).toHaveBeenCalledTimes(1);
  });
});
