import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vitest/config";

const consoleRoot = fileURLToPath(new URL(".", import.meta.url));

// The dev-server proxy forwards /eshu-api to a local Eshu API. The target
// defaults to the documented local Compose API port and is overridable via
// ESHU_DEV_PROXY_TARGET so a gate (or an operator) can point the console at a
// stack bound to a non-default host port without editing this file.
const proxyTarget = process.env.ESHU_DEV_PROXY_TARGET?.trim() || "http://127.0.0.1:8080";

export default defineConfig({
  build: {
    outDir: "dist",
    // The authoritative size gate is scripts/console-bundle-budget.mjs
    // (npm run console:bundle-budget), which enforces per-chunk budgets after
    // the build. Vite's generic 500 kB warning would otherwise fire on the
    // intentionally eager main chunk and the lazy diagram libraries on every
    // build; align the warning limit with the documented main-chunk budget so
    // the build log stays signal, not noise. See
    // docs/public/reference/console-bundle-budget.md.
    chunkSizeWarningLimit: 722,
    rollupOptions: {
      output: {
        // Split stable, cacheable vendor code out of the main entry chunk so
        // first-load weight stays low and these libraries are cached across
        // deploys. Heavy diagram/graph libraries (mermaid, cytoscape, wardley,
        // katex) are already lazy-loaded on demand, so they are intentionally
        // not listed here — Rollup keeps them in their own async chunks. The
        // bundle budget gate (scripts/console-bundle-budget.mjs) enforces the
        // documented size ceilings for these chunks.
        manualChunks: {
          "react-vendor": ["react", "react-dom", "react-router-dom"],
          d3: ["d3"],
          icons: ["lucide-react"]
        }
      }
    }
  },
  plugins: [react()],
  root: consoleRoot,
  server: {
    proxy: {
      "/eshu-api": {
        rewrite: (path) => path.replace(/^\/eshu-api/, ""),
        target: proxyTarget
      }
    }
  },
  test: {
    environment: "jsdom",
    environmentOptions: {
      jsdom: {
        url: "http://localhost:5174/"
      }
    },
    include: ["src/**/*.test.{ts,tsx}", "e2e/**/*.test.ts"],
    globals: true,
    setupFiles: "src/test/setup.ts"
  }
});
