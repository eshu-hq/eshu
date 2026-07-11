import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import {
  createLocaleMessages,
  localeCatalogs,
  supportedLocales,
  translatedMessageIds,
} from "./locales";
import { defaultLocale, defaultMessages, type MessageId } from "./messages";
import { NAV_GROUPS } from "./navigation";
import { ConsoleI18nProvider, FormattedMessage, createConsoleIntl } from "./provider";
import { formatApiUnavailableMessage, shellMessageDescriptors } from "./shellMessages";

const intl = createConsoleIntl(defaultMessages);

describe("console i18n framework", () => {
  it("addresses overview nav labels by stable message IDs", () => {
    const overview = NAV_GROUPS[0];

    expect(defaultLocale).toBe("en");
    expect(overview.messageId).toBe("app.nav.group.overview");
    expect(intl.formatMessage({ id: overview.messageId })).toBe("Overview");
    expect(Object.hasOwn(overview, "label")).toBe(false);

    const firstOverviewItems = overview.items.slice(0, 7);
    expect(firstOverviewItems.map((item) => item.messageId)).toEqual([
      "app.nav.item.status",
      "app.nav.item.dashboard",
      "app.nav.item.ask",
      "app.nav.item.semanticSearch",
      "app.nav.item.impact",
      "app.nav.item.exposurePath",
      "app.nav.item.changedSince",
    ]);
    expect(firstOverviewItems.map((item) => Object.hasOwn(item, "label"))).toEqual([
      false,
      false,
      false,
      false,
      false,
      false,
      false,
    ]);
    expect(firstOverviewItems.map((item) => intl.formatMessage({ id: item.messageId }))).toEqual([
      "Status",
      "Dashboard",
      "Ask Eshu",
      "Semantic Search",
      "Impact",
      "Exposure Path",
      "Changed Since",
    ]);
  });

  it("formats the shell API unavailable banner through the i18n layer", () => {
    expect(shellMessageDescriptors.apiUnavailable.id).toBe("app.shell.error.apiUnavailable");
    expect(
      formatApiUnavailableMessage(intl, {
        base: "/eshu-api/",
        detail: "unreachable",
      }),
    ).toBe("Eshu API unavailable at /eshu-api/ · unreachable.");
    expect(formatApiUnavailableMessage(intl, { base: "/eshu-api/", detail: "" })).toBe(
      "Eshu API unavailable at /eshu-api/.",
    );
  });

  it("renders rich formatted messages through the active provider catalog", () => {
    const translatedMessages = {
      ...defaultMessages,
      "app.shell.error.apiUnavailable": "API no disponible en {base}{detail}.",
    } satisfies Readonly<Record<MessageId, string>>;

    const { container } = render(
      <ConsoleI18nProvider messages={translatedMessages}>
        <FormattedMessage
          {...shellMessageDescriptors.apiUnavailable}
          values={{
            base: <strong>servicio privado</strong>,
            detail: " · revise la conexión",
          }}
        />
      </ConsoleI18nProvider>,
    );

    expect(container.textContent).toBe(
      "API no disponible en servicio privado · revise la conexión.",
    );
    expect(screen.getByText("servicio privado").tagName).toBe("STRONG");
  });

  it("keeps referenced message IDs present in the default catalog", () => {
    const referencedMessageIds = new Set<MessageId>([
      ...NAV_GROUPS.flatMap((group) => [
        group.messageId,
        ...group.items.map((item) => item.messageId),
      ]),
      ...Object.values(shellMessageDescriptors).map((descriptor) => descriptor.id),
    ]);
    const missingMessageIds = [...referencedMessageIds].filter(
      (messageId) => defaultMessages[messageId] === undefined,
    );

    expect(missingMessageIds).toEqual([]);
  });

  it("declares exactly five supported locale catalogs", () => {
    expect(supportedLocales).toEqual(["en", "zh", "ja", "de", "es"]);
    expect(Object.keys(localeCatalogs).sort()).toEqual([...supportedLocales].sort());
  });

  it("keeps top translated shell and nav message keys in parity for every locale", () => {
    expect(translatedMessageIds).toHaveLength(20);

    for (const locale of supportedLocales) {
      expect(Object.keys(localeCatalogs[locale]).sort()).toEqual([...translatedMessageIds].sort());
    }
  });

  it("formats a translated nav message through the provider catalog", () => {
    const { container } = render(
      <ConsoleI18nProvider messages={createLocaleMessages("de")}>
        <FormattedMessage id="app.nav.group.overview" />
      </ConsoleI18nProvider>,
    );

    expect(container.textContent).toBe("Übersicht");
  });
});
