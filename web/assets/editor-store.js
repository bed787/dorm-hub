"use strict";
// One recovery record per account. Blobs survive a reload while an upload is
// pending; records are cleared after a successful save or account deletion.
(function (root) {
  let database;
  async function open() {
    if (!root.indexedDB) return null;
    if (!database)
      database = new Promise((resolve, reject) => {
        const request = root.indexedDB.open("dormhub-editor", 1);
        request.onupgradeneeded = () =>
          request.result.createObjectStore("editors");
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error);
      });
    return database;
  }
  async function operate(key, action, value) {
    const db = await open();
    if (!db) {
      if (action === "put") throw new Error("Browser storage unavailable");
      return null;
    }
    return new Promise((resolve, reject) => {
      const tx = db.transaction(
        "editors",
        action === "get" ? "readonly" : "readwrite",
      );
      const request = tx
        .objectStore("editors")
        [action](...(action === "put" ? [value, key] : [key]));
      let result;
      request.onsuccess = () => {
        result = request.result;
      };
      tx.oncomplete = () => resolve(result ?? null);
      tx.onerror = () => reject(tx.error);
      tx.onabort = () => reject(tx.error);
    });
  }
  root.DormHubEditorStore = {
    get: (key) => operate(key, "get"),
    put: (key, value) => operate(key, "put", value),
    remove: (key) => operate(key, "delete"),
  };
})(globalThis);
