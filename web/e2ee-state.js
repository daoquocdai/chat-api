(function (root) {
  "use strict";

  const databaseName = "mini-hermes-e2ee";
  const storeName = "profiles";
  const keyPath = ["api_url", "user_id"];
  const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

  function failure(code = "storage_failed") {
    const error = new Error(code === "storage_blocked"
      ? "Bộ nhớ khóa đang bị tab khác chặn. Hãy đóng tab đó và thử lại."
      : "Không thể đọc hoặc lưu trạng thái E2EE trên trình duyệt.");
    error.code = code;
    return error;
  }

  function normalizeAPIURL(value) {
    try {
      if (typeof value !== "string" || !value.trim()) throw failure();
      const url = new URL(value);
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password) throw failure();
      url.search = "";
      url.hash = "";
      return url.href.replace(/\/+$/, "");
    } catch (_) {
      throw failure("invalid_scope");
    }
  }

  function userIDValid(value) {
    return typeof value === "string" && uuidPattern.test(value)
      && value !== "00000000-0000-0000-0000-000000000000";
  }

  function clone(value) {
    if (typeof root.structuredClone !== "function") throw failure();
    return root.structuredClone(value);
  }

  function connection(database) {
    let closed = false;
    function close() {
      closed = true;
      database.close();
    }
    database.onversionchange = close;
    database.onclose = () => { closed = true; };

    function transact(mode, operation) {
      return new Promise((resolve, reject) => {
        if (closed) { reject(failure()); return; }
        let transaction;
        let result = null;
        let failed = false;
        try {
          transaction = database.transaction(storeName, mode);
          transaction.oncomplete = () => {
            if (failed) reject(failure());
            else resolve(result);
          };
          transaction.onerror = () => { failed = true; };
          // Reject only when the transaction has settled, so the controller
          // can safely release ownership after awaiting its task queue.
          transaction.onabort = () => reject(failure());
          operation(transaction.objectStore(storeName), (value) => { result = value; });
        } catch (_) {
          failed = true;
          if (!transaction) { reject(failure()); return; }
          try { transaction.abort(); }
          catch (_) { reject(failure()); }
        }
      });
    }

    return Object.freeze({
      load(apiURL, userID) {
        let scope;
        try {
          scope = normalizeAPIURL(apiURL);
          if (!userIDValid(userID)) throw failure("invalid_scope");
        } catch (_) { return Promise.reject(failure("invalid_scope")); }
        return transact("readonly", (store, done) => {
          const request = store.get([scope, userID]);
          request.onsuccess = () => { done(request.result === undefined ? null : request.result); };
        });
      },
      save(profile) {
        let snapshot;
        try {
          // Capture before the caller can mutate its RAM state. Exactly one
          // put commits all key/pending/OPK changes in this profile together.
          snapshot = clone(profile);
          if (!snapshot || snapshot.version !== 1 || !userIDValid(snapshot.user_id)
            || snapshot.api_url !== normalizeAPIURL(snapshot.api_url)) throw failure();
        } catch (_) { return Promise.reject(failure()); }
        return transact("readwrite", (store) => { store.put(snapshot); });
      },
      close,
    });
  }

  function open() {
    return new Promise((resolve, reject) => {
      if (!root.indexedDB || typeof root.structuredClone !== "function") {
        reject(failure());
        return;
      }
      let request;
      let settled = false;
      function rejectOpen(code) {
        if (settled) return;
        settled = true;
        reject(failure(code));
      }
      try { request = root.indexedDB.open(databaseName, 1); }
      catch (_) { rejectOpen(); return; }
      request.onblocked = () => rejectOpen("storage_blocked");
      request.onerror = () => rejectOpen();
      request.onupgradeneeded = (event) => {
        try {
          if (event.oldVersion !== 0) throw failure();
          request.result.createObjectStore(storeName, { keyPath });
        } catch (_) {
          request.transaction.abort();
        }
      };
      request.onsuccess = () => {
        const database = request.result;
        if (settled) { database.close(); return; }
        try {
          if (database.version !== 1 || database.objectStoreNames.length !== 1
            || !database.objectStoreNames.contains(storeName)) throw failure();
          const store = database.transaction(storeName, "readonly").objectStore(storeName);
          if (!Array.isArray(store.keyPath) || store.keyPath.length !== keyPath.length
            || store.keyPath.some((part, index) => part !== keyPath[index]) || store.autoIncrement) {
            throw failure();
          }
          settled = true;
          resolve(connection(database));
        } catch (_) {
          database.close();
          rejectOpen();
        }
      };
    });
  }

  root.MiniHermesE2EEStorage = Object.freeze({ normalizeAPIURL, open });
})(globalThis);
