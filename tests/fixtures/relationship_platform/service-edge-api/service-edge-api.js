const runtime = {
  services: ["service-worker-jobs"],
  hostnames: ["api.example.test", "api-modern.internal.test"],
};

export function handler() {
  return runtime;
}
