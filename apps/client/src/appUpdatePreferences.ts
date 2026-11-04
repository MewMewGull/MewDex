export type UpdateChannel = "stable" | "preview";
export interface UpdatePreferences {
  automatic: boolean;
  channel: UpdateChannel;
  ignoredTag: string;
  lastAttempt: number;
}
export const updateStorageKey = "mewdex.app-updates";
export const defaultUpdatePreferences: UpdatePreferences = {
  automatic: true,
  channel: "stable",
  ignoredTag: "",
  lastAttempt: 0,
};

export function readUpdatePreferences(raw: string | null): UpdatePreferences {
  try {
    const value = JSON.parse(raw ?? "{}") ?? {};
    return {
      automatic:
        typeof value.automatic === "boolean"
          ? value.automatic
          : defaultUpdatePreferences.automatic,
      channel: value.channel === "preview" ? "preview" : "stable",
      ignoredTag: typeof value.ignoredTag === "string" ? value.ignoredTag : "",
      lastAttempt:
        typeof value.lastAttempt === "number" &&
        Number.isFinite(value.lastAttempt)
          ? Math.max(0, value.lastAttempt)
          : 0,
    };
  } catch {
    return { ...defaultUpdatePreferences };
  }
}

export function updateCheckDue(
  preferences: UpdatePreferences,
  now: number,
  failed = false,
) {
  const elapsed = now - preferences.lastAttempt;
  const interval = failed ? 60 * 60 * 1000 : 24 * 60 * 60 * 1000;
  return preferences.automatic && (elapsed < 0 || elapsed >= interval);
}
