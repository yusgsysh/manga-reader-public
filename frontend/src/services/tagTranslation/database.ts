const DB_NAME = "manga-reader-tag-translation";
const STORE_NAME = "database";
const KEY = "db";

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    if (typeof indexedDB === "undefined") {
      reject(new Error("IndexedDB not available"));
      return;
    }
    const request = indexedDB.open(DB_NAME, 1);
    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(STORE_NAME)) {
        db.createObjectStore(STORE_NAME);
      }
    };
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error ?? new Error("open IndexedDB failed"));
  });
}

export async function readCachedDb(): Promise<unknown | undefined> {
  try {
    const db = await openDb();
    const result = await new Promise<unknown | undefined>((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, "readonly");
      const request = tx.objectStore(STORE_NAME).get(KEY);
      request.onsuccess = () => resolve(request.result as unknown | undefined);
      request.onerror = () =>
        reject(request.error ?? new Error("read IndexedDB failed"));
    });
    db.close();
    return result;
  } catch (error) {
    console.warn("Failed to read cached tag translation database", error);
    return undefined;
  }
}

export async function writeCachedDb(data: unknown): Promise<boolean> {
  try {
    const db = await openDb();
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, "readwrite");
      tx.objectStore(STORE_NAME).put(data, KEY);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error ?? new Error("write IndexedDB failed"));
    });
    db.close();
    return true;
  } catch (error) {
    console.warn("Failed to cache tag translation database", error);
    return false;
  }
}

export async function clearCachedDb(): Promise<boolean> {
  try {
    const db = await openDb();
    await new Promise<void>((resolve, reject) => {
      const tx = db.transaction(STORE_NAME, "readwrite");
      tx.objectStore(STORE_NAME).delete(KEY);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error ?? new Error("clear IndexedDB failed"));
    });
    db.close();
    return true;
  } catch (error) {
    console.warn("Failed to clear tag translation cache", error);
    return false;
  }
}
