import { render, screen, waitFor } from "@testing-library/react";

import { ImagesPage } from "./ImagesPage";
import type { EshuApiClient } from "../api/client";

// ImagesPage renders the bounded (:ContainerImage) inventory. It must:
// - show a loading state until the first page resolves
// - render image rows (repository, tag, digest, media type, size)
// - never render a "deploying workloads" column (no workload edges in the graph)
// - render an explicit unavailable state when the endpoint fails
describe("ImagesPage", () => {
  it("shows the loading state until the first page resolves", () => {
    const client = { get: () => new Promise(() => {}) } as unknown as EshuApiClient;
    render(<ImagesPage client={client} />);
    expect(screen.getByText("Loading container images…")).toBeInTheDocument();
  });

  it("renders image rows without a deploying-workloads column", async () => {
    const client = {
      get: async () => ({
        data: {
          images: [
            {
              id: "oci-image://reg/team/api@sha256:aaa", digest: "sha256:aaadeadbeef0123456789",
              repository_id: "oci-registry://reg/team/api", registry: "reg", repository: "team/api",
              name: "api", tag: "1.2.3", media_type: "application/vnd.oci.image.manifest.v1+json",
              size_bytes: 28475610, source_system: "oci_registry"
            }
          ],
          count: 1, limit: 50, offset: 0, truncated: false
        },
        error: null,
        truth: { profile: "production", level: "exact", capability: "platform_impact.container_image_list", freshness: { state: "fresh" } }
      })
    } as unknown as EshuApiClient;

    render(<ImagesPage client={client} />);

    expect(await screen.findByText("Images loaded")).toBeInTheDocument();
    expect(screen.getByText("Tagged")).toBeInTheDocument();
    expect(await screen.findByText("team/api")).toBeInTheDocument();
    expect(screen.getByText("1.2.3")).toBeInTheDocument();
    expect(screen.getByText("28.48 MB")).toBeInTheDocument();
    // No fabricated deployment surface.
    expect(screen.queryByText(/deploying workloads/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/workloads/i)).not.toBeInTheDocument();
  });

  it("renders an explicit unavailable state when the endpoint fails", async () => {
    const client = { get: async () => { throw new Error("HTTP 503"); } } as unknown as EshuApiClient;
    render(<ImagesPage client={client} />);
    await waitFor(() =>
      expect(screen.getByText(/Container image inventory unavailable/)).toBeInTheDocument()
    );
  });
});
