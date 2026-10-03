import { readCachedDb, writeCachedDb } from "./database";
import { nextIdle } from "../../lib/idle";

const LOCAL_DB_URL =
  import.meta.env.VITE_TAG_TRANSLATION_DB_URL || "/db.text.js";

const REMOTE_DB_URL =
  import.meta.env.VITE_TAG_TRANSLATION_REMOTE_URL ||
  "https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.js";

const CALLBACK_NAME = "load_ehtagtranslation_db_text";

interface GlobalWithCallback {
  [CALLBACK_NAME]?: (data: unknown) => void;
}

function loadViaJsonp(url: string): Promise<unknown> {
  return new Promise((resolve, reject) => {
    let settled = false;
    const globalObj = window as unknown as GlobalWithCallback;

    const previousCallback = globalObj[CALLBACK_NAME];
    const cleanup = () => {
      if (previousCallback !== undefined) {
        globalObj[CALLBACK_NAME] = previousCallback;
      } else {
        delete globalObj[CALLBACK_NAME];
      }
    };

    globalObj[CALLBACK_NAME] = (data: unknown) => {
      if (settled) return;
      settled = true;
      cleanup();
      resolve(data);
    };

    const script = document.createElement("script");
    script.src = url;
    script.async = true;
    script.onerror = () => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(new Error("Failed to load tag translation database script"));
    };
    document.head.appendChild(script);
  });
}

export async function loadDb(): Promise<unknown> {
  // Reading IndexedDB (structured-clone of ~3 MB) and executing the inflate
  // script are both main-thread blocks; hold both back until the browser is
  // idle so they never land inside a route transition or a scroll.
  await nextIdle();
  const cached = await readCachedDb();
  if (cached !== undefined) return cached;
  return loadViaJsonp(LOCAL_DB_URL);
}

export async function updateDb(): Promise<unknown> {
  const data = await loadViaJsonp(REMOTE_DB_URL);
  await writeCachedDb(data);
  return data;
}
