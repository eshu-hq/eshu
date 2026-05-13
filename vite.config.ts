import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  build: {
    outDir: "site-dist"
  },
  plugins: [react()],
  test: {
    environment: "jsdom",
    exclude: ["apps/console/**", "node_modules/**", "tests/fixtures/**"],
    globals: true,
    setupFiles: "./src/test/setup.ts"
  }
});
