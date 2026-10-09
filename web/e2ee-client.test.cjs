// Run after building the matching Go/WASM artifacts: node --test web/e2ee-client.test.cjs
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const nativeFetch = globalThis.fetch;

globalThis.crypto ||= require("node:crypto").webcrypto;
globalThis.isSecureContext = true;
globalThis.localStorage = (() => {
  const values = new Map();
  return { get length() { return values.size; }, key: (index) => [...values.keys()][index],
    getItem: (key) => values.get(key) ?? null, setItem: (key, value) => values.set(key, value),
    removeItem: (key) => values.delete(key), clear: () => values.clear() };
})();
globalThis.fetch = async (url) => {
  assert.equal(url, "/e2ee.wasm");
  const bytes = fs.readFileSync(path.join(__dirname, "e2ee.wasm"));
  return { ok: true, arrayBuffer: async () => bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) };
};
require("./wasm_exec.js");
require("./e2ee-wasm.js");
require("./e2ee-state.js");
require("./e2ee.js");

const A = "11111111-1111-4111-8111-111111111111";
const B = "22222222-2222-4222-8222-222222222222";
const T = "33333333-3333-4333-8333-333333333333";
const OFFLINE = "44444444-4444-4444-8444-444444444444";
const toB = (id = T) => ({ id, kind: "direct", peer: { id: B, username: "bob" } });
const toA = (id = T) => ({ id, kind: "direct", peer: { id: A, username: "alice" } });
const clone = (value) => structuredClone(value);

// This API fixture implements the agreed wire contract and atomic epoch/backup selection.
// Crypto always runs through the real Go/WASM bridge.
function server(accounts) {
  const epochs = new Map();
  const current = new Map();
  const messages = new Map();
  const claims = new Map([...accounts].map(([id, value]) => [id, [...value.public_bundle.one_time_prekeys]]));
  const log = [];
  let loseMessageReply = false;
  let failBackup = false;
  function response(record, owner) {
    return { epoch_id: record.epoch_id, thread_id: record.thread_id, sender_id: record.sender_id,
      recipient_id: record.recipient_id, bootstrap: record.bootstrap, key_backup: record.backups.get(owner) || null };
  }
  function error(status, body) { return Object.assign(new Error(body.error), { status, details: body }); }
  const request = (owner) => async (url, options = {}) => {
    const method = options.method || "GET";
    const body = options.body ? JSON.parse(options.body) : null;
    log.push({ owner, url, method, body, raw: options.body });
    if (url === "/e2ee/account") {
      const account = accounts.get(owner);
      return clone({ user_id: owner, username: account.username, kdf: account.kdf,
        public_bundle: account.public_bundle, account_vault: account.account_vault });
    }
    let match = url.match(/^\/e2ee\/bundles\/([^/]+)\/claim$/);
    if (match && method === "POST") {
      const bundle = accounts.get(match[1]).public_bundle;
      return clone({ user_id: match[1], identity_public_key: bundle.identity_public_key,
        signed_prekey: bundle.signed_prekey, one_time_prekey: claims.get(match[1]).shift() || null });
    }
    match = url.match(/^\/threads\/([^/]+)\/epochs$/);
    if (match) {
      const threadID = match[1], active = current.get(threadID) || null;
      if (method === "GET") return clone({ epochs: [...epochs.values()].filter((item) => item.thread_id === threadID)
        .map((item) => response(item, owner)), current_epoch_id: active });
      if (body.previous_epoch_id !== active) throw error(409, { error: "epoch conflict", epoch: response(epochs.get(active), owner) });
      const header = JSON.parse(body.bootstrap);
      const record = { ...body, thread_id: threadID, sender_id: header.sender_id, recipient_id: header.recipient_id,
        backups: new Map([[owner, body.key_backup]]) };
      epochs.set(body.epoch_id, record);
      current.set(threadID, body.epoch_id);
      return clone(response(record, owner));
    }
    match = url.match(/^\/threads\/([^/]+)\/epochs\/([^/]+)\/key$/);
    if (match && method === "PUT") {
      if (failBackup) { failBackup = false; throw error(503, { error: "backup unavailable" }); }
      const record = epochs.get(match[2]);
      assert.equal(record.thread_id, match[1]);
      if (!record.backups.has(owner)) record.backups.set(owner, body.key_backup);
      return clone({ key_backup: record.backups.get(owner) });
    }
    match = url.match(/^\/threads\/([^/]+)\/messages$/);
    if (match && method === "POST") {
      if (!messages.has(body.message_id)) messages.set(body.message_id, {
        id: body.message_id, thread_id: match[1], sender_id: owner, seq: messages.size + 1, kind: "text",
        content_format: body.content_format, content: body.content, created_at: new Date().toISOString() });
      if (loseMessageReply) { loseMessageReply = false; throw new Error("response lost after commit"); }
      return clone(messages.get(body.message_id));
    }
    throw new Error("Unexpected API request: " + url);
  };
  return { request, log, epochs, messages, current,
    loseNextMessageReply() { loseMessageReply = true; }, failNextBackup() { failBackup = true; } };
}

test("recoverable multi-device epochs, concurrent initialization, replies, rekey and exact retries", { timeout: 300000 }, async () => {
  const alice = await MiniHermesE2EEClient.createAccount("alice", "Alice portable password!");
  const bob = await MiniHermesE2EEClient.createAccount("bob", "Bob portable password!");
  const api = server(new Map([[A, alice], [B, bob]]));
  const make = async (id, account, credential = account) => {
    const client = MiniHermesE2EEClient.create({ apiURL: "https://chat.test", userID: id,
      username: account.username, vaultKey: credential.vault_key, request: api.request(id) });
    await client.start();
    assert.equal(client.canRead(), true);
    return client;
  };
  const a1 = await make(A, alice), a2 = await make(A, alice), b1 = await make(B, bob);
  const [first, reply] = await Promise.all([a1.send(toB(), "alice first"), b1.send(toA(), "bob reply")]);
  assert.equal(api.epochs.size, 1, "concurrent proposals must select one committed epoch");
  assert.equal(JSON.parse(first.content).epoch_id, JSON.parse(reply.content).epoch_id);
  assert.equal((await a2.receive(toB(), { ...first, recipient_id: A })).plaintext, "alice first",
    "sender delivery target must not be mistaken for crypto recipient");
  assert.equal((await a1.receive(toB(), reply)).plaintext, "bob reply");
  assert.equal((await b1.receive(toA(), first)).plaintext, "alice first");
  const claimsBefore = api.log.filter((item) => item.url.endsWith("/claim")).length;
  const concurrent = await Promise.all([a1.send(toB(), "from device one"), a2.send(toB(), "from device two")]);
  assert.equal(new Set(concurrent.map((item) => item.id)).size, 2);
  assert.equal(api.log.filter((item) => item.url.endsWith("/claim")).length, claimsBefore,
    "messages in an existing epoch must not claim prekeys or repeat X3DH");

  const offline = await a1.send(toB(OFFLINE), "recipient has not opened this thread");
  const offlineEpoch = api.epochs.get(JSON.parse(offline.content).epoch_id);
  assert.equal(offlineEpoch.backups.has(B), false);
  localStorage.clear(); // Fresh profile / ended private-browsing session.
  const restoredBob = await MiniHermesE2EEClient.deriveCredentials("bob", "Bob portable password!", bob.kdf);
  const bFresh = await make(B, bob, restoredBob);
  api.failNextBackup();
  assert.equal((await bFresh.receive(toA(OFFLINE), offline)).status, "error",
    "recipient must not accept SK before its backup is durably acknowledged");
  assert.equal((await bFresh.receive(toA(OFFLINE), offline)).plaintext, "recipient has not opened this thread");
  assert.equal(offlineEpoch.backups.has(B), true);

  await b1.rekey(toA());
  assert.equal(api.epochs.size, 3);
  const afterRekey = await b1.send(toA(), "new epoch message");
  assert.notEqual(JSON.parse(afterRekey.content).epoch_id, JSON.parse(first.content).epoch_id);
  localStorage.clear();
  const restoredAlice = await MiniHermesE2EEClient.deriveCredentials("alice", "Alice portable password!", alice.kdf);
  const aFresh = await make(A, alice, restoredAlice);
  assert.equal((await aFresh.receive(toB(), first)).plaintext, "alice first", "sent history survives an empty profile");
  assert.equal((await aFresh.receive(toB(), reply)).plaintext, "bob reply", "received old epoch survives rekey");
  assert.equal((await aFresh.receive(toB(), afterRekey)).plaintext, "new epoch message");
  assert.equal((await aFresh.receive(toB(OFFLINE), offline)).plaintext, "recipient has not opened this thread");
  assert.equal(bFresh.account.one_time_prekeys.length, 20, "immutable archived private OPKs must remain recoverable");

  api.loseNextMessageReply();
  await assert.rejects(() => a2.send(toB(), "retry unchanged"), /response lost/);
  const pending = a2.snapshot().pending[0];
  const original = api.log.filter((item) => item.body?.message_id === pending.message_id).at(-1).raw;
  const retried = await a2.retry(pending.message_id);
  assert.equal(retried.id, pending.message_id);
  assert.equal(api.log.filter((item) => item.body?.message_id === pending.message_id).at(-1).raw, original);
  assert.equal(a2.snapshot().pending.length, 0);
  assert.equal((await bFresh.receive(toA(), retried)).plaintext, "retry unchanged");
  const corrupted = { ...retried, content: retried.content.replace(/ciphertext":"./, 'ciphertext":"!') };
  assert.equal((await bFresh.receive(toA(), corrupted)).status, "error");
  assert.equal(JSON.stringify(api.log).includes("Alice portable password!"), false);
  assert.equal(JSON.stringify(api.log).includes(alice.vault_key), false);
  assert.equal(JSON.stringify(api.log).includes(a1.account.identity.private_key), false);
  await Promise.all([a1, a2, b1, bFresh, aFresh].map((client) => client.close()));
});

test("UI registration/login never send password, reload recovers the vault, groups remain plaintext", { timeout: 300000 }, async () => {
  const vm = require("node:vm");
  const password = "One password for ui account!";
  const userID = crypto.randomUUID();
  const groupID = crypto.randomUUID();
  const logs = [], groupMessages = [];
  let registered;
  const jwt = "header." + Buffer.from(JSON.stringify({ sub: userID })).toString("base64url") + ".signature";
  const group = { id: groupID, kind: "group", name: "UI group", role: "admin", member_count: 2,
    joined_seq: 1, last_seq: 0, last_read_seq: 0, unread_count: 0 };
  async function fetchAPI(url, options = {}) {
    const body = options.body ? JSON.parse(options.body) : null;
    logs.push({ url, method: options.method || "GET", body, authorization: options.headers.get("Authorization") });
    const ok = (data, status = 200) => ({ ok: true, status, json: async () => clone(data) });
    if (url === "/auth/register") {
      assert.deepEqual(Object.keys(body).sort(), ["account_vault", "auth_credential", "kdf", "public_bundle", "username"]);
      registered = body;
      return ok({ id: userID, username: body.username }, 201);
    }
    if (url.startsWith("/auth/params?")) return ok({ username: registered.username, kdf: registered.kdf });
    if (url === "/auth/login") {
      assert.deepEqual(Object.keys(body).sort(), ["auth_credential", "username"]);
      if (body.auth_credential !== registered.auth_credential) {
        return { ok: false, status: 401, json: async () => ({ error: "invalid credentials" }) };
      }
      return ok({ access_token: jwt, token_type: "Bearer", user_id: userID });
    }
    assert.equal(options.headers.get("Authorization"), "Bearer " + jwt);
    if (url === "/users") return ok([{ id: userID, username: registered.username }, { id: B, username: "bob" }]);
    if (url === "/e2ee/account") return ok({ ...registered, auth_credential: undefined, user_id: userID });
    if (url === "/threads") return ok([group]);
    if (url === "/auth/ws-ticket") return ok({ ws_url: "wss://chat.test/ws", ticket: "test-ticket" });
    if (url.startsWith("/threads/" + groupID + "/messages?")) return ok({ messages: groupMessages, next_cursor: null });
    if (url === "/threads/" + groupID + "/messages") {
      assert.equal(body.content_format, undefined);
      const message = { id: body.message_id, thread_id: groupID, sender_id: userID, seq: groupMessages.length + 1,
        kind: "text", content_format: "plaintext", content: body.content, created_at: new Date().toISOString() };
      groupMessages.push(message);
      group.last_seq = message.seq;
      return ok(message);
    }
    throw new Error("Unexpected UI request: " + url);
  }
  function browser(cache = new Map()) {
    function element() {
      return { hidden: false, disabled: false, value: "", textContent: "", dataset: {}, children: [], options: [], events: {},
        scrollHeight: 100, scrollTop: 0, clientHeight: 100, elements: {}, classList: { add() {} },
        addEventListener(name, listener) { this.events[name] = listener; }, append(...items) { this.children.push(...items); },
        replaceChildren(...items) { this.children = items; }, reset() {
          for (const item of Object.values(this.elements)) item.value = "";
        }, focus() {}, setAttribute() {}, querySelectorAll() { return []; },
        querySelector(selector) { return selector === ".message:last-child" ?
          this.children.filter((item) => item.dataset?.seq).at(-1) : null; },
        getBoundingClientRect() { return { top: 0, bottom: 100, left: 0, right: 100 }; } };
    }
    const nodes = new Map([...fs.readFileSync(path.join(__dirname, "index.html"), "utf8").matchAll(/\bid="([^"]+)"/g)]
      .map((match) => [match[1], element()]));
    for (const name of ["register", "login"]) nodes.get(name + "-form").elements = {
      username: nodes.get(name + "-username"), password: nodes.get(name + "-password") };
    class Socket { static OPEN = 1; readyState = 0; close() { this.readyState = 3; this.onclose?.(); } }
    const context = vm.createContext({ console, crypto, WebAssembly, isSecureContext: true,
      MiniHermesWASM, MiniHermesE2EEStorage, Headers, AbortController, AbortSignal, URL, URLSearchParams,
      atob, btoa, structuredClone, WebSocket: Socket, innerWidth: 100, innerHeight: 100,
      location: { origin: "https://chat.test" }, fetch: fetchAPI,
      setTimeout: () => 1, clearTimeout() {}, requestAnimationFrame: () => 1, cancelAnimationFrame() {},
      sessionStorage: { getItem: (key) => cache.get(key) ?? null, setItem: (key, value) => cache.set(key, value),
        removeItem: (key) => cache.delete(key) },
      window: { addEventListener() {}, innerWidth: 100, innerHeight: 100 },
      document: { visibilityState: "visible", querySelector: (selector) => nodes.get(selector.slice(1)) || null,
        createElement: element, addEventListener() {} } });
    for (const file of ["e2ee.js", "realtime-core.js", "app.js"]) {
      vm.runInContext(fs.readFileSync(path.join(__dirname, file), "utf8"), context, { filename: file });
    }
    return { context, nodes, cache, run: (code) => vm.runInContext(code, context) };
  }
  const first = browser();
  first.nodes.get("register-username").value = " Ui_User ";
  first.nodes.get("register-password").value = password;
  await first.nodes.get("register-form").events.submit({ preventDefault() {} });
  assert.equal(first.run("state.e2ee?.canRead()"), true);
  assert.equal(first.nodes.get("chat-view").hidden, false);
  assert.equal(first.nodes.get("register-password").value, "");
  assert.equal(registered.username, "ui_user");
  assert.equal(first.cache.has("mini-hermes.vault-key.v2"), true);
  assert.equal([...first.cache.values()].includes(password), false);
  first.context.groupID = groupID;
  await first.run("openThread(groupID)");
  first.nodes.get("content").value = "group stays plaintext";
  await first.nodes.get("message-form").events.submit({ preventDefault() {} });
  assert.equal(groupMessages[0].content, "group stays plaintext");
  assert.equal(first.run("state.currentCache.pendingSend"), null);

  const reloadLogs = logs.length;
  const reload = browser(new Map(first.cache));
  while (reload.run("state.authBusy")) await new Promise(setImmediate);
  assert.equal(reload.run("state.e2ee?.canRead()"), true);
  assert.equal(logs.slice(reloadLogs).some((item) => item.url === "/e2ee/account"), true);
  assert.equal(logs.slice(reloadLogs).some((item) => item.url === "/auth/login"), false);
  assert.equal(logs.slice(reloadLogs).some((item) => item.url === "/auth/register"), false);

  const fresh = browser(); // No token, key, IndexedDB or prior tab required.
  fresh.nodes.get("login-username").value = "ui_user";
  fresh.nodes.get("login-password").value = "wrong password";
  await fresh.nodes.get("login-form").events.submit({ preventDefault() {} });
  assert.equal(fresh.run("state.e2ee"), null);
  assert.equal(fresh.nodes.get("chat-view").hidden, true);
  fresh.nodes.get("login-password").value = password;
  await fresh.nodes.get("login-form").events.submit({ preventDefault() {} });
  assert.equal(fresh.run("state.e2ee.canRead()"), true);
  assert.equal(fresh.nodes.get("login-password").value, "");
  assert.equal(fresh.run("state.e2ee.fingerprint"), first.run("state.e2ee.fingerprint"));
  assert.equal(JSON.stringify(logs).includes(password), false);
  assert.equal(JSON.stringify(logs).includes(first.run("state.vaultKey")), false);
  assert.equal(first.run("state.currentCache.messages.get(1).content"), "group stays plaintext");
  for (const instance of [first, reload, fresh]) instance.nodes.get("logout-button").events.click();
});

test("real HTTP/Redis/WebSocket round trip and password-only history recovery", {
  skip: !process.env.CHAT_API_TEST_URL, timeout: 300000,
}, async () => {
  const base = process.env.CHAT_API_TEST_URL.replace(/\/$/, "");
  const suffix = Date.now().toString(36);
  const password = "fetest account password 2026!";
  const sockets = [], clients = [];
  const calls = [];
  async function call(url, options = {}, token) {
    const headers = new Headers(options.headers || {});
    if (token) headers.set("Authorization", "Bearer " + token);
    if (options.body) headers.set("Content-Type", "application/json");
    calls.push({ url, body: options.body });
    const response = await nativeFetch(base + url, { ...options, headers });
    const body = await response.json();
    if (!response.ok) throw Object.assign(new Error("API " + response.status + " " + (options.method || "GET") + " " + url),
      { status: response.status, details: body });
    return body;
  }
  async function register(name) {
    const username = "fetest_" + suffix + "_" + name;
    const account = await MiniHermesE2EEClient.createAccount(username, password);
    await call("/auth/register", { method: "POST", body: JSON.stringify({
      username, auth_credential: account.auth_credential, kdf: account.kdf,
      public_bundle: account.public_bundle, account_vault: account.account_vault }) });
    return username;
  }
  async function login(username) {
    const params = await call("/auth/params?username=" + encodeURIComponent(username));
    const credential = await MiniHermesE2EEClient.deriveCredentials(username, password, params.kdf);
    const login = await call("/auth/login", { method: "POST",
      body: JSON.stringify({ username, auth_credential: credential.auth_credential }) });
    const request = (url, options) => call(url, options, login.access_token);
    const client = MiniHermesE2EEClient.create({ apiURL: base, userID: login.user_id, username,
      vaultKey: credential.vault_key, request });
    clients.push(client);
    await client.start();
    return { client, request, id: login.user_id };
  }
  async function connect(actor) {
    const ticket = await actor.request("/auth/ws-ticket", { method: "POST" });
    const url = new URL(ticket.ws_url);
    url.searchParams.set("ticket", ticket.ticket);
    const socket = new WebSocket(url);
    sockets.push(socket);
    const events = [];
    socket.addEventListener("message", (event) => {
      try { const value = JSON.parse(event.data); if (value.type === "message.created") events.push(value); } catch (_) {}
    });
    await new Promise((resolve, reject) => {
      const timeout = setTimeout(() => reject(new Error("gateway connect timeout")), 5000);
      socket.addEventListener("open", () => { clearTimeout(timeout); resolve(); }, { once: true });
      socket.addEventListener("error", () => { clearTimeout(timeout); reject(new Error("gateway connect failed")); }, { once: true });
    });
    return events;
  }
  async function eventFor(events, id) {
    const deadline = Date.now() + 5000;
    while (Date.now() < deadline) {
      const value = events.find((event) => event.message_id === id);
      if (value) return value;
      await new Promise((resolve) => setTimeout(resolve, 20));
    }
    throw new Error("gateway message delivery timeout");
  }
  try {
    const aliceName = await register("a"), bobName = await register("b"), offlineName = await register("c");
    const a1 = await login(aliceName), a2 = await login(aliceName), b1 = await login(bobName);
    const [eventsA1, eventsA2, eventsB] = await Promise.all([connect(a1), connect(a2), connect(b1)]);
    const thread = await a1.request("/threads/direct", { method: "POST", body: JSON.stringify({ peer_id: b1.id }) });
    const ab = { id: thread.id, kind: "direct", peer: { id: b1.id } };
    const ba = { id: thread.id, kind: "direct", peer: { id: a1.id } };
    const [sent, replied] = await Promise.all([a1.client.send(ab, "real A first"), b1.client.send(ba, "real B first")]);
    assert.ok(JSON.parse(sent.content).epoch_id === JSON.parse(replied.content).epoch_id);
    const epochs = await a1.request("/threads/" + thread.id + "/epochs");
    assert.equal(epochs.epochs.length, 1);
    const senderCopy = await eventFor(eventsA2, sent.id);
    assert.ok(senderCopy.recipient_id === a1.id);
    assert.equal((await a2.client.receive(ab, senderCopy)).plaintext, "real A first");
    assert.equal((await b1.client.receive(ba, await eventFor(eventsB, sent.id))).plaintext, "real A first");
    assert.equal((await a1.client.receive(ab, await eventFor(eventsA1, replied.id))).plaintext, "real B first");
    await Promise.all([a1.client.send(ab, "real device one"), a2.client.send(ab, "real device two")]);
    assert.equal((await a1.request("/threads/" + thread.id + "/epochs")).epochs.length, 1);

    // C has never unlocked the account or accepted an epoch before its first message.
    const users = await a1.request("/users");
    const offlineID = users.find((user) => user.username === offlineName).id;
    const ac = await a1.request("/threads/direct", { method: "POST", body: JSON.stringify({ peer_id: offlineID }) });
    const offlineMessage = await a1.client.send({ id: ac.id, kind: "direct", peer: { id: offlineID } }, "real offline recipient");
    localStorage.clear();
    const cFresh = await login(offlineName);
    const ca = { id: ac.id, kind: "direct", peer: { id: a1.id } };
    const cHistory = await cFresh.request("/threads/" + ac.id + "/messages?limit=100");
    assert.equal((await cFresh.client.receive(ca, cHistory.messages.find((message) => message.id === offlineMessage.id))).plaintext,
      "real offline recipient");

    await b1.client.rekey(ba);
    const newest = await b1.client.send(ba, "real new epoch");
    localStorage.clear();
    const aFresh = await login(aliceName);
    const history = await aFresh.request("/threads/" + thread.id + "/messages?limit=100");
    for (const message of history.messages) {
      const view = await aFresh.client.receive(ab, message);
      assert.ok(view.status === "ready", "fresh browser must read both directions and every retained epoch");
    }
    assert.ok(history.messages.some((message) => message.id === newest.id));
    assert.equal((await aFresh.request("/threads/" + thread.id + "/epochs")).epochs.length, 2);

    const group = await a1.request("/threads/group", { method: "POST",
      body: JSON.stringify({ name: "fetest group", member_ids: [b1.id] }) });
    const groupMessage = await a1.request("/threads/" + group.id + "/messages", { method: "POST",
      body: JSON.stringify({ message_id: crypto.randomUUID(), content: "real group plaintext" }) });
    assert.equal(groupMessage.content_format, "plaintext");
    assert.equal(groupMessage.content, "real group plaintext");
    const groupHistory = await b1.request("/threads/" + group.id + "/messages?limit=100");
    assert.ok(groupHistory.messages.some((message) => message.id === groupMessage.id && message.content === "real group plaintext"));
    assert.ok((await eventFor(eventsA2, groupMessage.id)).recipient_id === a1.id);
    assert.equal(JSON.stringify(calls).includes(password), false);
    assert.equal(calls.some((item) => item.body?.includes('"vault_key"')), false);
  } finally {
    for (const socket of sockets) socket.close();
    await Promise.all(clients.map((client) => client.close()));
  }
});
