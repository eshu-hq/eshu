import { describe, expect, it } from "vitest";

import { liveE2EDevServerArgs, parseLiveE2EConsolePort } from "./liveE2EDevServer";

describe("live E2E dev server port", () => {
  it("defaults to the documented port and accepts an isolated concurrent port", () => {
    expect(parseLiveE2EConsolePort(undefined)).toBe(5180);
    expect(parseLiveE2EConsolePort(" 5182 ")).toBe(5182);
    expect(liveE2EDevServerArgs(5182)).toEqual([
      "--config",
      "apps/console/vite.config.ts",
      "--host",
      "127.0.0.1",
      "--strictPort",
      "--port",
      "5182",
    ]);
  });

  it.each(["invalid", "0", "65536", "5180.5"])("rejects invalid port %s", (value) => {
    expect(() => parseLiveE2EConsolePort(value)).toThrow(/ESHU_E2E_CONSOLE_PORT/);
  });
});
