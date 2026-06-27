import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const i18nDir = path.dirname(fileURLToPath(import.meta.url));
const messagesPath = path.join(i18nDir, "messages.ts");

function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) return walk(entryPath);
    if (!entry.isFile()) return [];
    if (!/\.(ts|tsx)$/.test(entry.name)) return [];
    if (/\.(test|spec)\.tsx?$/.test(entry.name)) return [];
    return [entryPath];
  });
}

function readExistingFile(filePath) {
  statSync(filePath);
  return readFileSync(filePath, "utf8");
}

const defaultCatalog = readExistingFile(messagesPath);
const defaultMessageIds = new Set(
  [...defaultCatalog.matchAll(/"(app\.[^"]+)"\s*:/g)].map((match) => match[1]),
);
const referencedMessageIds = new Set();

for (const filePath of walk(i18nDir)) {
  if (filePath === messagesPath) continue;
  if (filePath.endsWith("check-catalog.mjs")) continue;
  const source = readExistingFile(filePath);
  for (const match of source.matchAll(/\b(?:id|messageId):\s*"(app\.[^"]+)"/g)) {
    referencedMessageIds.add(match[1]);
  }
  for (const match of source.matchAll(/\bdescriptor\("(app\.[^"]+)"\)/g)) {
    referencedMessageIds.add(match[1]);
  }
}

const missing = [...referencedMessageIds].filter((id) => !defaultMessageIds.has(id)).sort();

if (missing.length > 0) {
  console.error(`Missing ${missing.length} default i18n message(s):`);
  for (const id of missing) console.error(`- ${id}`);
  process.exit(1);
}

console.log(`Validated ${referencedMessageIds.size} i18n message references.`);
