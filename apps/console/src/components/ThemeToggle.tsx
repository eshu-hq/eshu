// components/ThemeToggle.tsx
// Top-bar switch between the default light theme and the dark theme.
import { Moon, Sun } from "lucide-react";

import { useConsoleIntl } from "../i18n/provider";
import { shellMessageDescriptors } from "../i18n/shellMessages";
import { useTheme } from "../theme/theme";

export function ThemeToggle(): React.JSX.Element {
  const intl = useConsoleIntl();
  const [theme, toggle] = useTheme();
  const label = intl.formatMessage(
    theme === "light" ? shellMessageDescriptors.themeToDark : shellMessageDescriptors.themeToLight,
  );
  return (
    <button
      aria-label={label}
      className="topbar-btn theme-toggle"
      title={label}
      type="button"
      onClick={toggle}
    >
      {theme === "light" ? <Moon aria-hidden /> : <Sun aria-hidden />}
    </button>
  );
}
