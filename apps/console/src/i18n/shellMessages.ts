import { defaultMessages, type MessageId } from "./messages";
import type { ConsoleIntl, ConsoleMessageDescriptor } from "./provider";

function descriptor(id: MessageId): ConsoleMessageDescriptor {
  return { id, defaultMessage: defaultMessages[id] };
}

export const shellMessageDescriptors = {
  title: descriptor("app.shell.title"),
  brandSubtitle: descriptor("app.shell.brandSubtitle"),
  subtitle: descriptor("app.shell.subtitle"),
  sourceDemoFixtures: descriptor("app.shell.source.demoFixtures"),
  sourceLive: descriptor("app.shell.source.live"),
  sourceConnecting: descriptor("app.shell.source.connecting"),
  sourceLiveOffline: descriptor("app.shell.source.liveOffline"),
  sourceNotConnected: descriptor("app.shell.source.notConnected"),
  sourceDemoShort: descriptor("app.shell.source.demoShort"),
  sourceLiveShort: descriptor("app.shell.source.liveShort"),
  sourceRepositoryCountOne: descriptor("app.shell.source.repositoryCount.one"),
  sourceRepositoryCountOther: descriptor("app.shell.source.repositoryCount.other"),
  searchButton: descriptor("app.shell.search.button"),
  searchInput: descriptor("app.shell.search.input"),
  searchPlaceholder: descriptor("app.shell.search.placeholder"),
  verifiedOnlyToggle: descriptor("app.shell.verifiedOnly.toggle"),
  noNotifications: descriptor("app.shell.notifications.none"),
  signOut: descriptor("app.shell.signOut"),
  demoBannerTitle: descriptor("app.shell.demoBanner.title"),
  demoBannerBody: descriptor("app.shell.demoBanner.body"),
  verifiedBanner: descriptor("app.shell.verifiedBanner"),
  apiUnavailable: descriptor("app.shell.error.apiUnavailable"),
  sessionDataUnavailable: descriptor("app.shell.error.sessionDataUnavailable"),
  unreachable: descriptor("app.shell.error.unreachable"),
  logoutFailed: descriptor("app.shell.error.logoutFailed"),
  editDataSource: descriptor("app.shell.editDataSource"),
} as const;

export function formatApiUnavailableMessage(
  intl: ConsoleIntl,
  params: { readonly base: string; readonly detail: string },
): string {
  return intl.formatMessage(shellMessageDescriptors.apiUnavailable, {
    base: params.base,
    detail: params.detail.length > 0 ? ` · ${params.detail}` : "",
  });
}

export function formatRepositoryCount(intl: ConsoleIntl, count: number): string {
  const descriptor =
    count === 1
      ? shellMessageDescriptors.sourceRepositoryCountOne
      : shellMessageDescriptors.sourceRepositoryCountOther;
  return intl.formatMessage(descriptor, { count });
}
