// theme/theme.ts
// Light/dark preference for the console. Light is the default for everyone;
// the choice is remembered per browser. applyStoredTheme runs before React
// renders so the first paint already uses the right palette.
import { useCallback, useEffect, useState } from "react";

export type ThemeName = "light" | "dark";

const THEME_STORAGE_KEY = "eshu.console.theme";

export function storedTheme(): ThemeName {
  try {
    return window.localStorage.getItem(THEME_STORAGE_KEY) === "dark" ? "dark" : "light";
  } catch {
    return "light";
  }
}

export function applyTheme(theme: ThemeName): void {
  document.documentElement.dataset.theme = theme;
}

export function applyStoredTheme(): void {
  applyTheme(storedTheme());
}

export function useTheme(): readonly [ThemeName, () => void] {
  const [theme, setTheme] = useState<ThemeName>(storedTheme);
  useEffect(() => {
    applyTheme(theme);
    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, theme);
    } catch {
      // Storage can be unavailable; the theme still applies for this session.
    }
  }, [theme]);
  const toggle = useCallback(() => setTheme((t) => (t === "light" ? "dark" : "light")), []);
  return [theme, toggle] as const;
}
