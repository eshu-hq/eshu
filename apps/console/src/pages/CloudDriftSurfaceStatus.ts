export type CloudDriftSurfaceStatus = "not_requested" | "loading" | "loaded" | "unavailable";

export function cloudDriftSurfaceStatus(
  enabled: boolean,
  loaded: boolean,
  error: string,
): CloudDriftSurfaceStatus {
  if (!enabled) return "not_requested";
  if (error !== "") return "unavailable";
  if (loaded) return "loaded";
  return "loading";
}

export function cloudDriftSurfaceLabel(status: CloudDriftSurfaceStatus): string {
  switch (status) {
    case "not_requested":
      return "not requested";
    case "loading":
      return "loading";
    case "loaded":
      return "loaded";
    case "unavailable":
      return "unavailable";
  }
}

export function cloudDriftSurfaceValue(
  status: CloudDriftSurfaceStatus,
  loadedValue: number,
): number | "—" {
  return status === "loaded" ? loadedValue : "—";
}
