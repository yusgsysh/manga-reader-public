#!/usr/bin/env node

import { existsSync, mkdirSync, readFileSync, writeFileSync, renameSync, unlinkSync, statSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const OUTPUT_DIR = join(__dirname, "..", "public");
const OUTPUT = join(OUTPUT_DIR, "db.text.js");
const URL =
  process.env.VITE_TAG_TRANSLATION_REMOTE_URL ||
  "https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.js";

mkdirSync(OUTPUT_DIR, { recursive: true });

console.log("Downloading latest EhTagTranslation database...");
console.log(`  URL: ${URL}`);
console.log(`  -> ${OUTPUT}`);

const tmp = `${OUTPUT}.tmp`;

try {
  const res = await fetch(URL, { redirect: "follow" });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status} ${res.statusText}`);
  }
  const text = await res.text();

  if (!text.includes("load_ehtagtranslation_db_text")) {
    console.error("Error: downloaded file is not a valid db.text.js (callback not found).");
    process.exit(1);
  }

  writeFileSync(tmp, text);

  if (existsSync(OUTPUT)) {
    const existing = readFileSync(OUTPUT, "utf8");
    if (existing === text) {
      console.log("Database is already up to date. Nothing to do.");
      unlinkSync(tmp);
      process.exit(0);
    }
  }

  renameSync(tmp, OUTPUT);

  const size = statSync(OUTPUT).size;
  const formatted = size > 1024 * 1024
    ? `${(size / 1024 / 1024).toFixed(1)} MB`
    : `${(size / 1024).toFixed(1)} kB`;
  console.log(`Done: ${OUTPUT} (${formatted})`);
  console.log("Restart the dev server or rebuild to pick up the new database.");
} catch (err) {
  console.error("Error: download failed.", err.message);
  try { unlinkSync(tmp); } catch {}
  process.exit(1);
}
