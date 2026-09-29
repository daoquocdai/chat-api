const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const test = require("node:test");
const vm = require("node:vm");
const realtime = require("./realtime-core.js");

function element() {
  return {
    hidden: false, disabled: false, value: "", textContent: "", innerHTML: "",
    scrollHeight: 0, scrollTop: 0, clientHeight: 500, dataset: {}, elements: {},
    listeners: {},
    addEventListener(name, callback) { this.listeners[name] = callback; },
    replaceChildren() {}, append() {}, setAttribute() {}, focus() {}, reset() {},
    querySelectorAll() { return []; },
    getBoundingClientRect() { return { top: 0, bottom: 500 }; },
  };
}

function makeApp() {
  const nodes = new Map();
  const document = {
    visibilityState: "visible", listeners: {},
    querySelector(selector) {
      if (!nodes.has(selector)) nodes.set(selector, element());
      return nodes.get(selector);
    },
    createElement: element,
    addEventListener(name, callback) { this.listeners[name] = callback; },
  };
  const window = { listeners: {}, addEventListener(name, callback) { this.listeners[name] = callback; } };
  const storage = new Map();
  const sessionStorage = {
    getItem: (key) => storage.get(key) || null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  };
  const sockets = [];
  class FakeWebSocket {
    constructor(url) { this.url = url; this.closed = false; sockets.push(this); }
    open() { this.onopen?.(); }
    message(data) { this.onmessage?.({ data: JSON.stringify(data) }); }
    close() { this.closed = true; this.onclose?.(); }
  }
  const timers = new Map();
  let nextTimer = 1;
  const bobID = "00000000-0000-4000-8000-000000000002";
  const aliceID = "00000000-0000-4000-8000-000000000001";
  const threadID = "00000000-0000-4000-8000-000000000003";
  const history = Array.from({ length: 10 }, (_, i) => ({
    id: `message-${i+1}`, thread_id: threadID, sender_id: aliceID,
    seq: i+1, kind: "text", content_format: "plaintext", content: `tin ${i+1}`,
    created_at: "2026-09-28T00:00:00Z",
  }));
  const cursors = [];
  let tickets = 0;
  const response = (body, status = 200) => ({ ok: status < 400, status, json: async () => body });
  async function fetch(url) {
    if (url === "/auth/ws-ticket") {
      return response({ ticket: `ticket-${++tickets}`, ws_url: "ws://localhost:8081/ws" });
    }
    if (url === "/threads/direct") {
      return response({ id: threadID, peer: { id: aliceID, username: "alice" }, last_read_seq: 0, peer_last_read_seq: 0 });
    }
    if (url === "/threads") {
      return response([{ id: threadID, peer: { id: aliceID, username: "alice" }, last_read_seq: 0, peer_last_read_seq: 0, unread_count: 0 }]);
    }
    if (url.startsWith(`/threads/${threadID}/messages?`)) {
      const query = new URL(url, "http://localhost").searchParams;
      const before = query.has("before_seq") ? Number(query.get("before_seq")) : null;
      cursors.push(before);
      const older = history.filter((message) => before === null || message.seq < before).sort((a, b) => b.seq - a.seq);
      const messages = older.slice(0, Number(query.get("limit")));
      return response({ messages, next_cursor: older.length > messages.length ? messages.at(-1).seq : null });
    }
    throw new Error(`unexpected fetch: ${url}`);
  }
  const sandbox = {
    document, window, sessionStorage, fetch, WebSocket: FakeWebSocket,
    MiniHermesRealtime: realtime, Headers, URL, URLSearchParams, AbortController, atob,
    crypto: { randomUUID: () => "new-message-id" },
    requestAnimationFrame: () => 1, cancelAnimationFrame: () => {},
    setTimeout(callback) {
      const id = nextTimer++;
      timers.set(id, () => { timers.delete(id); callback(); });
      return id;
    },
    clearTimeout(id) { timers.delete(id); },
  };
  const context = vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(path.join(__dirname, "app.js"), "utf8"), context);
  const state = vm.runInContext("state", context);
  state.sessionVersion = 1;
  state.token = "test-access-token";
  state.currentUserID = bobID;
  state.currentUsername = "bob";
  state.users = [{ id: bobID, username: "bob" }, { id: aliceID, username: "alice" }];
  nodes.get("#chat-view").hidden = false;
  return { context, state, sockets, timers, history, cursors, bobID, aliceID, threadID };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 15));

test("every app.js DOM selector exists in index.html", () => {
  const script = fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
  const html = fs.readFileSync(path.join(__dirname, "index.html"), "utf8");
  for (const [, id] of script.matchAll(/document\.querySelector\("#([\w-]+)"\)/g)) {
    assert.ok(html.includes(`id="${id}"`), `missing #${id} in index.html`);
  }
});

test("Alice event reaches Bob immediately; offline Bob reconnects and backfills every page", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  assert.equal(app.state.messages.size, 10);
  await vm.runInContext("connectWebSocket()", app.context);
  assert.equal(app.sockets.length, 1);
  app.sockets[0].open();
  await settle();

  const aliceMessage = { id: "message-11", thread_id: app.threadID, sender_id: app.aliceID,
    seq: 11, kind: "text", content_format: "plaintext", content: "Alice vừa gửi",
    created_at: "2026-09-28T00:00:00Z" };
  app.history.push(aliceMessage); // REST POST has committed before the gateway event.
  app.sockets[0].message({ ...aliceMessage, type: "message.created", message_id: aliceMessage.id, recipient_id: app.bobID });
  assert.equal(app.state.messages.get(11).content, "Alice vừa gửi");
  app.sockets[0].message({ ...aliceMessage, type: "message.created", message_id: aliceMessage.id, recipient_id: app.bobID });
  assert.equal(app.state.messages.size, 11);

  app.sockets[0].close();
  for (let seq = 12; seq <= 80; seq++) app.history.push({ ...aliceMessage, id: `message-${seq}`, seq });
  assert.equal(app.timers.size, 1);
  const reconnect = [...app.timers.values()][0];
  reconnect();
  await settle();
  assert.equal(app.sockets.length, 2);
  app.sockets[1].open();
  await settle();
  assert.equal(app.state.messages.size, 80);
  assert.equal(app.state.messageIDs.size, 80);
  assert.equal(app.state.syncedSeq, 80);
  assert.deepEqual(app.cursors.slice(-3), [null, 51, 21]);

  vm.runInContext("clearSession()", app.context);
  assert.equal(app.sockets[1].closed, true);
  assert.equal(app.timers.size, 0);
  assert.equal(app.state.token, "");
});

test("an online seq gap triggers REST catch-up without a periodic request", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  for (let seq = 11; seq <= 45; seq++) {
    app.history.push({ ...app.history[0], id: `message-${seq}`, seq });
  }
  const gapEvent = {
    type: "message.created", message_id: "message-45", thread_id: app.threadID,
    sender_id: app.aliceID, recipient_id: app.bobID, seq: 45,
    kind: "text", content_format: "plaintext", content: "tin 45",
    created_at: "2026-09-28T00:00:00Z",
  };
  vm.runInContext("handleSocketMessage", app.context)(JSON.stringify(gapEvent));
  await settle();
  assert.equal(app.state.syncedSeq, 45);
  assert.equal(app.state.messages.size, 45);
  assert.deepEqual(app.cursors.slice(-2), [null, 16]);
});

test("logout cancels a pending reconnect and stale callback cannot open a socket", async () => {
  const app = makeApp();
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  app.sockets[0].close();
  const staleReconnect = [...app.timers.values()][0];
  vm.runInContext("clearSession()", app.context);
  assert.equal(app.timers.size, 0);
  staleReconnect();
  await settle();
  assert.equal(app.sockets.length, 1);
});
