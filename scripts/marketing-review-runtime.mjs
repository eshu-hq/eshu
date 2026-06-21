import { createServer, preview as startVitePreview } from "vite";

/** Return the browser URL for the actual bound Vite preview address. */
export function previewBaseUrlFromAddress(address, host) {
  if (
    !address ||
    typeof address === "string" ||
    !Number.isInteger(address.port) ||
    address.port <= 0
  ) {
    throw new Error("vite preview server address was not available");
  }
  return `http://${host}:${address.port}/`;
}

/** Extract and validate the evaluator function from a transformed module. */
export function evaluatorFromModule(moduleNamespace) {
  const evaluator = moduleNamespace?.evaluateMarketingReview;
  if (typeof evaluator !== "function") {
    throw new Error("marketing review evaluator did not export evaluateMarketingReview");
  }
  return evaluator;
}

/** Load the TypeScript evaluator through Vite rather than native Node TS support. */
export async function loadMarketingReviewEvaluator(repoRoot) {
  const server = await createServer({
    root: repoRoot,
    logLevel: "silent",
    server: { middlewareMode: true },
    appType: "custom"
  });
  try {
    const moduleNamespace = await server.ssrLoadModule("/src/marketingReview.ts");
    return evaluatorFromModule(moduleNamespace);
  } finally {
    await server.close();
  }
}

/** Start Vite preview on an OS-assigned local port and return its real URL. */
export async function startMarketingPreview(repoRoot, host) {
  const server = await startVitePreview({
    root: repoRoot,
    preview: {
      host,
      port: 0
    }
  });
  return {
    server,
    baseUrl: previewBaseUrlFromAddress(server.httpServer.address(), host)
  };
}

/** Stop a Vite preview server started by startMarketingPreview. */
export async function closeMarketingPreview(runtime) {
  if (!runtime?.server?.httpServer) {
    return;
  }
  await new Promise((resolve, reject) => {
    runtime.server.httpServer.close((error) => {
      if (error) {
        reject(error);
        return;
      }
      resolve();
    });
  });
}
