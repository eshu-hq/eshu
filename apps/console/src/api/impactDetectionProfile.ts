// Detection profile handling shared by the impact reads. The server answers on
// the profile it applied and echoes it as detection_profile. A read that asked
// for comprehensive evidence but was answered on precise silently drops
// possibly-affected rows, so a mismatch is an error, never a clean result.

export type DetectionProfile = "precise" | "comprehensive";

// verifyEchoedProfile returns the profile the server reported, or throws when it
// differs from the requested one. A missing echo is an error when requireEcho
// is set or when comprehensive evidence was requested; only a plain precise read
// of a route that has always answered precise may omit it.
export function verifyEchoedProfile(
  requested: DetectionProfile,
  echoed: unknown,
  requireEcho: boolean,
): string | null {
  const reported = typeof echoed === "string" ? echoed.trim() : "";
  if (reported === "") {
    if (requireEcho || requested === "comprehensive") {
      throw new Error(
        `The server did not report a detection profile; requested ${requested}. No result is shown.`,
      );
    }
    return null;
  }
  if (reported !== requested) {
    throw new Error(
      `Requested ${requested} detection profile but the server answered ${reported}. No result is shown.`,
    );
  }
  return reported;
}
