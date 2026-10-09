(function (root) {
  "use strict";

  // Only ciphertext retries live here. Account keys and epoch backups come from the API.
  function outbox(apiURL, userID) {
    const prefix = "mini-hermes:outbox:v2:" + JSON.stringify([new URL(apiURL).origin, userID]) + ":";
    const pending = new Map();
    try {
      for (let index = 0; index < root.localStorage.length; index += 1) {
        const key = root.localStorage.key(index);
        if (!key?.startsWith(prefix)) continue;
        try {
          const item = JSON.parse(root.localStorage.getItem(key));
          if (item?.message_id && item.context?.sender_id === userID && typeof item.body === "string") {
            pending.set(item.message_id, item);
          }
        } catch (_) { /* An invalid cache entry never becomes an account recovery source. */ }
      }
    } catch (_) { /* Storage is optional, including in a private browsing session. */ }
    return {
      values: () => [...pending.values()],
      get: (id) => pending.get(id),
      put(item) {
        pending.set(item.message_id, item);
        try { root.localStorage.setItem(prefix + item.message_id, JSON.stringify(item)); }
        catch (_) { /* The exact retry remains in this tab's RAM. */ }
      },
      remove(id) {
        pending.delete(id);
        try { root.localStorage.removeItem(prefix + id); } catch (_) {}
      },
      close() { pending.clear(); },
    };
  }

  root.MiniHermesE2EEStorage = Object.freeze({ outbox });
})(globalThis);
