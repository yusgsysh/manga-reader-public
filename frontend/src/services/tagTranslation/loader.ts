const DB_SCRIPT_URL =
  import.meta.env.VITE_TAG_TRANSLATION_DB_URL ||
  "https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.js";

const CALLBACK_NAME = "load_ehtagtranslation_db_text";

interface GlobalWithCallback {
  [CALLBACK_NAME]?: (data: unknown) => void;
}

export function loadDbHtmlJs(): Promise<unknown> {
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
    script.src = DB_SCRIPT_URL;
    script.async = true;
    script.onerror = () => {
      if (settled) return;
      settled = true;
      cleanup();
      reject(new Error("Failed to load EhTagTranslation database script"));
    };
    document.head.appendChild(script);
  });
}
