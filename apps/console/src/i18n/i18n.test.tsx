import { describe, expect, it } from "vitest";

import { defaultLocale, defaultMessages, type MessageId } from "./messages";
import { NAV_GROUPS } from "./navigation";
import { createConsoleIntl } from "./provider";
import { formatApiUnavailableMessage, shellMessageDescriptors } from "./shellMessages";

const intl = createConsoleIntl(defaultMessages);

describe("console i18n framework", () => {
  it("addresses overview nav labels by stable message IDs", () => {
    const overview = NAV_GROUPS[0];

    expect(defaultLocale).toBe("en");
    expect(overview.messageId).toBe("app.nav.group.overview");
    expect(intl.formatMessage({ id: overview.messageId })).toBe("Overview");
    expect(Object.hasOwn(overview, "label")).toBe(false);

    const firstOverviewItems = overview.items.slice(0, 6);
    expect(firstOverviewItems.map((item) => item.messageId)).toEqual([
      "app.nav.item.status",
      "app.nav.item.dashboard",
      "app.nav.item.ask",
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
    ]);
    expect(firstOverviewItems.map((item) => intl.formatMessage({ id: item.messageId }))).toEqual([
      "Status",
      "Dashboard",
      "Ask Eshu",
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
});
