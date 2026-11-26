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
  it("addresses start nav labels by stable message IDs", () => {
    const start = NAV_GROUPS[0];

    expect(defaultLocale).toBe("en");
    expect(start.messageId).toBe("app.nav.group.start");
    expect(intl.formatMessage({ id: start.messageId })).toBe("Start");
    expect(Object.hasOwn(start, "label")).toBe(false);

    expect(start.items.map((item) => item.messageId)).toEqual([
      "app.nav.item.home",
      "app.nav.item.ask",
      "app.nav.item.guidedQuestions",
      "app.nav.item.semanticSearch",
      "app.nav.item.contentSearch",
    ]);
    expect(start.items.every((item) => !Object.hasOwn(item, "label"))).toBe(true);
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
    expect(translatedMessageIds).toHaveLength(24);

    for (const locale of supportedLocales) {
      expect(Object.keys(localeCatalogs[locale]).sort()).toEqual([...translatedMessageIds].sort());
    }
  });

  it("formats a translated nav message through the provider catalog", () => {
    const { container } = render(
      <ConsoleI18nProvider messages={createLocaleMessages("de")}>
        <FormattedMessage id="app.nav.group.security" />
      </ConsoleI18nProvider>,
    );

    expect(container.textContent).toBe("Sicherheit");
  });
});
