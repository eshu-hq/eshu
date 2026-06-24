import { render, screen, fireEvent } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";

import { VulnerabilitiesPage } from "./VulnerabilitiesPage";
import { demoModel } from "../console/demoModel";
import type { ConsoleModel } from "../console/types";

function renderPage(model: ConsoleModel): void {
  render(
    <MemoryRouter>
      <VulnerabilitiesPage model={model} />
    </MemoryRouter>
  );
}

describe("VulnerabilitiesPage", () => {
  it("separates reachable findings from the known-intelligence catalog", () => {
    renderPage(demoModel);

    expect(screen.getByRole("heading", { name: "Vulnerabilities" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Reachable in services" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "Known intelligence (catalog)" })).toBeInTheDocument();

    // Reachable tab is default: the impact-finding advisory renders.
    expect(screen.getByRole("link", { name: "CVE-2024-0001" })).toBeInTheDocument();
    // Services column shows the human service name from the model.
    expect(screen.getAllByText("checkout-service").length).toBeGreaterThan(0);
  });

  it("shows the human service name carried by the model, not a raw repo id", () => {
    // The adapter resolves repository ids to catalog names; the page renders
    // those names verbatim. Guard against a raw graph id leaking into the
    // Services column.
    const model: ConsoleModel = {
      ...demoModel,
      source: "live",
      vulnerabilities: [
        {
          id: "GHSA-zzzz",
          package: "axios",
          severity: "high",
          cvss: 7.5,
          kev: false,
          fixedVersion: null,
          services: ["catalog-api"]
        }
      ]
    };

    renderPage(model);

    expect(screen.getAllByText("catalog-api").length).toBeGreaterThan(0);
    expect(screen.queryByText(/^repository[:_]/)).not.toBeInTheDocument();
  });

  it("renders a supply-chain impact path with evidence state for each hop", () => {
    renderPage(demoModel);

    const path = screen.getByRole("region", { name: "Supply-chain impact path" });
    expect(path).toHaveTextContent("CVE-2024-0001");
    expect(path).toHaveTextContent("sample-lib");
    expect(path).toHaveTextContent("SBOM");
    expect(path).toHaveTextContent("sha256:abc123");
    expect(path).toHaveTextContent("workload:checkout");
    expect(path).toHaveTextContent("checkout-service");
    expect(path).toHaveTextContent("Owner evidence missing");
    expect(path).toHaveTextContent("admitted impact");
    expect(path).toHaveTextContent("SBOM correlation missing");
    expect(screen.getByRole("link", { name: "Raw advisory evidence" })).toHaveAttribute("href", "/vulnerabilities/CVE-2024-0001");
    expect(screen.getByRole("link", { name: "SBOM evidence" })).toHaveAttribute("href", "/sbom");
    expect(screen.getByRole("link", { name: "Image inventory" })).toHaveAttribute("href", "/images");
  });

  it("keeps partial supply-chain paths explicit when SBOM and image hops are missing", () => {
    const partial: ConsoleModel = {
      ...demoModel,
      images: [],
      sbom: null,
      vulnerabilities: [
        {
          id: "CVE-2026-PARTIAL",
          package: "partial-lib",
          severity: "high",
          cvss: 7.8,
          kev: false,
          fixedVersion: "1.2.3",
          services: ["checkout-service"]
        }
      ]
    };

    renderPage(partial);

    const path = screen.getByRole("region", { name: "Supply-chain impact path" });
    expect(path).toHaveTextContent("partial-lib");
    expect(path).toHaveTextContent("SBOM evidence missing");
    expect(path).toHaveTextContent("Image evidence missing");
    expect(path).toHaveTextContent("not proven");
  });

  it("does not use unscoped SBOM and image inventory as exact impact path evidence", () => {
    const unscopedInventory: ConsoleModel = {
      ...demoModel,
      images: [
        {
          artifactType: "",
          configDigest: "sha256:cfg-unrelated",
          digest: "sha256:unrelated",
          id: "oci-image://registry.example/sample/unrelated@sha256:unrelated",
          mediaType: "application/vnd.oci.image.manifest.v1+json",
          name: "unrelated",
          registry: "registry.example",
          repository: "sample/unrelated",
          repositoryId: "oci-registry://registry.example/sample/unrelated",
          sizeBytes: 1024,
          sourceSystem: "oci_registry",
          tag: "9.9.9"
        }
      ],
      sbom: { total: 9, verified: 9, sbomCount: 9, attestationCount: 0 }
    };

    renderPage(unscopedInventory);

    const path = screen.getByRole("region", { name: "Supply-chain impact path" });
    expect(path).toHaveTextContent("SBOM correlation missing");
    expect(path).toHaveTextContent("Image evidence missing");
    expect(path).not.toHaveTextContent("sha256:unrelated");
    expect(path).not.toHaveTextContent("digest match");
  });

  it("requires full image tag or digest identity before marking image evidence exact", () => {
    const ambiguousImageName: ConsoleModel = {
      ...demoModel,
      images: [
        {
          artifactType: "",
          configDigest: "sha256:cfg-wrong-tag",
          digest: "sha256:wrong-tag",
          id: "oci-image://registry.example/sample/checkout@sha256:wrong-tag",
          mediaType: "application/vnd.oci.image.manifest.v1+json",
          name: "checkout",
          registry: "registry.example",
          repository: "sample/checkout",
          repositoryId: "oci-registry://registry.example/sample/checkout",
          sizeBytes: 1024,
          sourceSystem: "oci_registry",
          tag: "9.9.9"
        }
      ]
    };

    renderPage(ambiguousImageName);

    const path = screen.getByRole("region", { name: "Supply-chain impact path" });
    expect(path).toHaveTextContent("Image evidence missing");
    expect(path).not.toHaveTextContent("sha256:wrong-tag");
  });

  it("renders a no-impact state instead of fabricating a supply-chain path", () => {
    const empty: ConsoleModel = {
      ...demoModel,
      vulnerabilities: []
    };

    renderPage(empty);

    const path = screen.getByRole("region", { name: "Supply-chain impact path" });
    expect(path).toHaveTextContent("No admitted supply-chain impact path");
    expect(path).toHaveTextContent("No reachable advisory from this source.");
  });

  it("shows catalog rows linking to the existing CVE detail page", () => {
    renderPage(demoModel);

    fireEvent.click(screen.getByRole("tab", { name: "Known intelligence (catalog)" }));

    const link = screen.getByRole("link", { name: /CVE-2021-44228/ });
    expect(link).toHaveAttribute("href", "/vulnerabilities/CVE-2021-44228");
    // Catalog provenance line distinguishes it from reachable impact.
    expect(screen.getByText(/GET \/api\/v0\/supply-chain\/advisories/)).toBeInTheDocument();
  });

  it("renders the catalog empty state when no advisories and no client", () => {
    const empty: ConsoleModel = {
      ...demoModel,
      advisories: [],
      provenance: { ...demoModel.provenance, advisories: "empty" }
    };
    renderPage(empty);

    fireEvent.click(screen.getByRole("tab", { name: "Known intelligence (catalog)" }));
    expect(
      screen.getByText("No catalog advisories yet — requires the vulnerability-intelligence collector.")
    ).toBeInTheDocument();
  });

  it("renders the catalog unavailable state on provenance failure", () => {
    const failed: ConsoleModel = {
      ...demoModel,
      advisories: [],
      provenance: { ...demoModel.provenance, advisories: "unavailable" }
    };
    renderPage(failed);

    fireEvent.click(screen.getByRole("tab", { name: "Known intelligence (catalog)" }));
    expect(
      screen.getByText(/The vulnerability-intelligence catalog is unavailable/)
    ).toBeInTheDocument();
  });
});
