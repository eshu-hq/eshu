import { defaultMessages, type MessageId } from "./messages";

export const supportedLocales = ["en", "zh", "ja", "de", "es"] as const;

export type SupportedLocale = (typeof supportedLocales)[number];

export const translatedMessageIds = [
  "app.nav.group.start",
  "app.nav.group.services",
  "app.nav.group.change",
  "app.nav.group.security",
  "app.nav.group.infrastructure",
  "app.nav.group.operations",
  "app.nav.group.explore",
  "app.nav.item.home",
  "app.nav.group.system",
  "app.nav.item.status",
  "app.nav.item.dashboard",
  "app.nav.item.ask",
  "app.nav.item.impact",
  "app.nav.item.exposurePath",
  "app.nav.item.changedSince",
  "app.nav.item.graphExplorer",
  "app.nav.item.relationships",
  "app.shell.title",
  "app.shell.brandSubtitle",
  "app.shell.subtitle",
  "app.shell.source.live",
  "app.shell.search.button",
  "app.shell.search.input",
  "app.shell.error.apiUnavailable",
] as const satisfies readonly MessageId[];

export type TranslatedMessageId = (typeof translatedMessageIds)[number];

export type TranslationCatalog = Readonly<Record<TranslatedMessageId, string>>;

const zhCatalog = {
  "app.nav.group.start": "开始",
  "app.nav.group.services": "服务与代码",
  "app.nav.group.change": "变更与影响",
  "app.nav.group.security": "安全",
  "app.nav.group.infrastructure": "基础设施",
  "app.nav.group.operations": "事件与监控",
  "app.nav.group.explore": "探索图谱",
  "app.nav.item.home": "首页",
  "app.nav.group.system": "系统",
  "app.nav.item.status": "状态",
  "app.nav.item.dashboard": "仪表板",
  "app.nav.item.ask": "询问 Eshu",
  "app.nav.item.impact": "影响",
  "app.nav.item.exposurePath": "暴露路径",
  "app.nav.item.changedSince": "变更时间",
  "app.nav.item.graphExplorer": "图谱浏览器",
  "app.nav.item.relationships": "关系",
  "app.shell.title": "Eshu 控制台",
  "app.shell.brandSubtitle": "上下文图谱",
  "app.shell.subtitle": "组织知识库：代码、服务、基础设施与负责人",
  "app.shell.source.live": "实时",
  "app.shell.search.button": "搜索",
  "app.shell.search.input": "搜索 Eshu",
  "app.shell.error.apiUnavailable": "Eshu API 在 {base}{detail} 不可用。",
} satisfies TranslationCatalog;

const jaCatalog = {
  "app.nav.group.start": "はじめに",
  "app.nav.group.services": "サービスとコード",
  "app.nav.group.change": "変更と影響",
  "app.nav.group.security": "セキュリティ",
  "app.nav.group.infrastructure": "インフラ",
  "app.nav.group.operations": "インシデントと監視",
  "app.nav.group.explore": "グラフを探索",
  "app.nav.item.home": "ホーム",
  "app.nav.group.system": "システム",
  "app.nav.item.status": "ステータス",
  "app.nav.item.dashboard": "ダッシュボード",
  "app.nav.item.ask": "Eshu に質問",
  "app.nav.item.impact": "影響",
  "app.nav.item.exposurePath": "露出パス",
  "app.nav.item.changedSince": "変更履歴",
  "app.nav.item.graphExplorer": "グラフエクスプローラー",
  "app.nav.item.relationships": "関係",
  "app.shell.title": "Eshu コンソール",
  "app.shell.brandSubtitle": "コンテキストグラフ",
  "app.shell.subtitle": "組織のナレッジベース：コード、サービス、インフラ、オーナー",
  "app.shell.source.live": "ライブ",
  "app.shell.search.button": "検索",
  "app.shell.search.input": "Eshu を検索",
  "app.shell.error.apiUnavailable": "Eshu APIは {base}{detail} で利用できません。",
} satisfies TranslationCatalog;

const deCatalog = {
  "app.nav.group.start": "Start",
  "app.nav.group.services": "Services & Code",
  "app.nav.group.change": "Änderungen & Auswirkungen",
  "app.nav.group.security": "Sicherheit",
  "app.nav.group.infrastructure": "Infrastruktur",
  "app.nav.group.operations": "Vorfälle & Monitoring",
  "app.nav.group.explore": "Graph erkunden",
  "app.nav.item.home": "Startseite",
  "app.nav.group.system": "System",
  "app.nav.item.status": "Status",
  "app.nav.item.dashboard": "Dashboard",
  "app.nav.item.ask": "Eshu fragen",
  "app.nav.item.impact": "Auswirkungen",
  "app.nav.item.exposurePath": "Expositionspfad",
  "app.nav.item.changedSince": "Änderungen seit",
  "app.nav.item.graphExplorer": "Graph-Explorer",
  "app.nav.item.relationships": "Beziehungen",
  "app.shell.title": "Eshu-Konsole",
  "app.shell.brandSubtitle": "Kontextgraph",
  "app.shell.subtitle":
    "Die Wissensbasis Ihrer Organisation: Code, Services, Infrastruktur und Verantwortliche",
  "app.shell.source.live": "Live",
  "app.shell.search.button": "Suchen",
  "app.shell.search.input": "Eshu durchsuchen",
  "app.shell.error.apiUnavailable": "Eshu-API unter {base}{detail} nicht verfügbar.",
} satisfies TranslationCatalog;

const esCatalog = {
  "app.nav.group.start": "Inicio",
  "app.nav.group.services": "Servicios y código",
  "app.nav.group.change": "Cambios e impacto",
  "app.nav.group.security": "Seguridad",
  "app.nav.group.infrastructure": "Infraestructura",
  "app.nav.group.operations": "Incidentes y monitoreo",
  "app.nav.group.explore": "Explorar el grafo",
  "app.nav.item.home": "Inicio",
  "app.nav.group.system": "Sistema",
  "app.nav.item.status": "Estado",
  "app.nav.item.dashboard": "Panel",
  "app.nav.item.ask": "Preguntar a Eshu",
  "app.nav.item.impact": "Impacto",
  "app.nav.item.exposurePath": "Ruta de exposición",
  "app.nav.item.changedSince": "Cambios desde",
  "app.nav.item.graphExplorer": "Explorador de grafos",
  "app.nav.item.relationships": "Relaciones",
  "app.shell.title": "Consola de Eshu",
  "app.shell.brandSubtitle": "Grafo de contexto",
  "app.shell.subtitle":
    "La base de conocimiento de tu organización: código, servicios, infraestructura y responsables",
  "app.shell.source.live": "En vivo",
  "app.shell.search.button": "Buscar",
  "app.shell.search.input": "Buscar en Eshu",
  "app.shell.error.apiUnavailable": "La API de Eshu no está disponible en {base}{detail}.",
} satisfies TranslationCatalog;

export const localeCatalogs: Readonly<Record<SupportedLocale, TranslationCatalog>> = {
  en: selectDefaultMessages(translatedMessageIds),
  zh: zhCatalog,
  ja: jaCatalog,
  de: deCatalog,
  es: esCatalog,
};

export function createLocaleMessages(locale: SupportedLocale): Readonly<Record<MessageId, string>> {
  return {
    ...defaultMessages,
    ...localeCatalogs[locale],
  };
}

function selectDefaultMessages(messageIds: readonly TranslatedMessageId[]): TranslationCatalog {
  return Object.fromEntries(
    messageIds.map((messageId) => [messageId, defaultMessages[messageId]] as const),
  ) as TranslationCatalog;
}
