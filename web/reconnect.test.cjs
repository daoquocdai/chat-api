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
    listeners: {}, addEventListener(name, fn) { this.listeners[name] = fn; },
    replaceChildren() {}, append() {}, focus() {}, reset() {}, setAttribute() {},
    querySelectorAll() { return []; },
    getBoundingClientRect() { return { top: 0, bottom: 500 }; },
  };
}

function makeApp(options = {}) {
  const nodes = new Map();
  const document = {
    visibilityState: "visible", listeners: {},
    querySelector(selector) {
      if (!nodes.has(selector)) nodes.set(selector, element());
      return nodes.get(selector);
    },
    createElement: element,
    addEventListener(name, fn) { this.listeners[name] = fn; },
  };
  const window = { listeners: {}, addEventListener(name, fn) { this.listeners[name] = fn; } };
  const storage = new Map();
  const sessionStorage = {
    getItem: (key) => storage.get(key) || null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: (key) => storage.delete(key),
  };
  const sockets = [];
  class FakeWebSocket {
    static OPEN = 1;
    constructor(url) { this.url = url; this.readyState = 0; this.closed = false; sockets.push(this); }
    open() { this.readyState = 1; this.onopen?.(); }
    message(data) { this.onmessage?.({ data: JSON.stringify(data) }); }
    close() { this.readyState = 3; this.closed = true; this.onclose?.(); }
  }
  const timers = new Map();
  const timerDelays = new Map();
  const timerDeadlines = new Map();
  const clock = { now: 0 };
  let nextTimer = 1;
  const bobID = "00000000-0000-4000-8000-000000000002";
  const aliceID = "00000000-0000-4000-8000-000000000001";
  const currentID = options.user === "alice" ? aliceID : bobID;
  const peerID = options.user === "alice" ? bobID : aliceID;
  const threadID = "00000000-0000-4000-8000-000000000003";
  const history = options.history || Array.from({ length: 10 }, (_, i) => ({
    id: `message-${i + 1}`, thread_id: threadID, sender_id: aliceID,
    seq: i + 1, kind: "text", content_format: "plaintext", content: `tin ${i + 1}`,
    created_at: "2026-09-28T00:00:00Z",
  }));
  const cursors = [];
  const requests = [];
  const readMarkers = [];
  let serverReadSeq = options.initialReadSeq || 0;
  let peerServerReadSeq = options.peerInitialReadSeq || 0;
  let ticketCount = 0;
  let ticketAttempts = 0;
  let activeTickets = 0;
  let maxActiveTickets = 0;
  let uuidCount = 0;
  let activeHistory = 0;
  let maxActiveHistory = 0;
  const response = (body) => ({ ok: true, status: 200, json: async () => body });
  async function fetch(url, requestOptions) {
    requests.push({ method: requestOptions?.method || "GET", url });
    if (url === "/auth/ws-ticket") {
      ticketAttempts += 1;
      activeTickets += 1;
      maxActiveTickets = Math.max(maxActiveTickets, activeTickets);
      if (options.hangFirstTicket && ticketAttempts === 1) {
        return new Promise((_, reject) => requestOptions.signal.addEventListener("abort", () => {
          activeTickets -= 1;
          reject(new Error("ticket request aborted"));
        }, { once: true }));
      }
      activeTickets -= 1;
      return response({ ticket: `ticket-${++ticketCount}`, ws_url: "ws://localhost:8081/ws" });
    }
    const unreadCount = () => history.filter((m) => m.seq > serverReadSeq && m.sender_id !== currentID && m.kind !== "system").length;
    if (url === "/threads/direct") return response({ id: threadID, peer: { id: peerID, username: options.user === "alice" ? "bob" : "alice" }, last_seq: history.length, last_read_seq: serverReadSeq, peer_last_read_seq: peerServerReadSeq, unread_count: unreadCount() });
    if (url === "/threads") return response([{ id: threadID, peer: { id: peerID, username: options.user === "alice" ? "bob" : "alice" }, last_seq: history.length, last_read_seq: serverReadSeq, peer_last_read_seq: peerServerReadSeq, unread_count: unreadCount() }]);
    if (url === `/threads/${threadID}/messages` && requestOptions?.method === "POST") {
      const body = JSON.parse(requestOptions.body);
      const message = { id: body.message_id, thread_id: threadID, sender_id: currentID,
        seq: history.length + 1, kind: "text", content_format: "plaintext", content: body.content,
        created_at: "2026-09-28T00:00:00Z" };
      history.push(message);
      return response(message);
    }
    if (url === `/threads/${threadID}/read`) {
      const lastReadSeq = JSON.parse(requestOptions.body).last_read_seq;
      readMarkers.push(lastReadSeq);
      serverReadSeq = Math.max(serverReadSeq, lastReadSeq);
      return response({ last_read_seq: serverReadSeq });
    }
    if (url.startsWith(`/threads/${threadID}/messages?`)) {
      activeHistory += 1;
      maxActiveHistory = Math.max(maxActiveHistory, activeHistory);
      await Promise.resolve();
      const query = new URL(url, "http://localhost").searchParams;
      const before = query.has("before_seq") ? Number(query.get("before_seq")) : null;
      cursors.push(before);
      const older = history.filter((m) => before === null || m.seq < before).sort((a, b) => b.seq - a.seq);
      const messages = older.slice(0, Number(query.get("limit")));
      activeHistory -= 1;
      return response({ messages, next_cursor: older.length > messages.length ? messages.at(-1).seq : null });
    }
    throw new Error(`unexpected request: ${url}`);
  }
  const sandbox = {
    document, window, sessionStorage, fetch, WebSocket: FakeWebSocket,
    MiniHermesRealtime: realtime, Headers, URL, URLSearchParams, AbortController, atob,
    crypto: { randomUUID: () => `new-message-${++uuidCount}` },
    requestAnimationFrame: () => 1, cancelAnimationFrame() {},
    setTimeout(fn, delay) { const id = nextTimer++; timers.set(id, fn); timerDelays.set(id, delay); timerDeadlines.set(id, clock.now + delay); return id; },
    clearTimeout(id) { timers.delete(id); timerDelays.delete(id); timerDeadlines.delete(id); },
  };
  if (options.fakeClock) {
    sandbox.Date = class extends Date { static now() { return clock.now; } };
  }
  const context = vm.createContext(sandbox);
  vm.runInContext(fs.readFileSync(path.join(__dirname, "app.js"), "utf8"), context);
  const state = vm.runInContext("state", context);
  Object.assign(state, { sessionVersion: 1, token: "test-token", currentUserID: currentID, currentUsername: options.user || "bob", users: [{ id: bobID, username: "bob" }, { id: aliceID, username: "alice" }] });
  nodes.get("#chat-view").hidden = false;
  return { context, state, document, window, sockets, timers, timerDelays, history, cursors, readMarkers, requests,
    bobID, aliceID, peerID, threadID, setPeerReadSeq(seq) { peerServerReadSeq = seq; },
    advance(ms) {
      const target = clock.now + ms;
      while (true) {
        const next = [...timerDeadlines].filter(([, due]) => due <= target).sort((a, b) => a[1] - b[1])[0];
        if (!next) break;
        const [id, due] = next;
        clock.now = due;
        const callback = timers.get(id);
        timers.delete(id);
        timerDelays.delete(id);
        timerDeadlines.delete(id);
        callback?.();
      }
      clock.now = target;
    },
    get ticketCount() { return ticketCount; }, get ticketAttempts() { return ticketAttempts; },
    get activeTickets() { return activeTickets; }, get maxActiveTickets() { return maxActiveTickets; },
    get maxActiveHistory() { return maxActiveHistory; } };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 20));

async function runReconnectTimer(app) {
  const timerID = app.state.reconnectTimer;
  assert.notEqual(timerID, null);
  const callback = app.timers.get(timerID);
  assert.equal(typeof callback, "function");
  app.timers.delete(timerID);
  callback();
  await settle();
}

function countRequests(app, method, prefix) {
  return app.requests.filter((request) => request.method === method && request.url.startsWith(prefix)).length;
}

function countThreadSummaries(app) {
  return app.requests.filter((request) => request.method === "GET" && request.url === "/threads").length;
}

test("two stationary tabs: idle, three messages, gateway outage and one catch-up", async () => {
  const bob = makeApp({ initialReadSeq: 10 });
  const alice = makeApp({ user: "alice", history: bob.history });
  for (const app of [alice, bob]) {
    await vm.runInContext("loadThreads()", app.context);
    await vm.runInContext(`openConversation("${app.peerID}")`, app.context);
    await vm.runInContext("connectWebSocket()", app.context);
    app.sockets[0].open();
  }
  await settle();
  assert.equal([alice, bob].reduce((n, app) => n + countRequests(app, "POST", "/auth/ws-ticket"), 0), 2);
  assert.equal([alice, bob].reduce((n, app) => n + countRequests(app, "GET", "/threads/"), 0), 4);
  assert.equal([alice, bob].reduce((n, app) => n + app.requests.filter((r) => r.method === "GET" && r.url === "/threads").length, 0), 2);
  const beforeIdle = [alice, bob].map((app) => app.requests.length);
  // Eighteen five-second heartbeat frames model 90 idle seconds without tab/peer actions.
  for (let i = 0; i < 18; i++) {
    alice.sockets[0].message({ type: "heartbeat" });
    bob.sockets[0].message({ type: "heartbeat" });
  }
  await settle();
  assert.deepEqual([alice, bob].map((app) => app.requests.length), beforeIdle);
  for (const app of [alice, bob]) {
    assert.equal(app.ticketCount, 1);
    assert.equal(app.sockets.length, 1);
  }

  const bobHistoryBefore = countRequests(bob, "GET", `/threads/${bob.threadID}/messages?`);
  const bobThreadsBefore = countThreadSummaries(bob);
  const aliceThreadsBefore = countThreadSummaries(alice);
  const messageForm = vm.runInContext("messageForm", alice.context);
  const contentInput = vm.runInContext("contentInput", alice.context);
  for (let i = 0; i < 3; i++) {
    contentInput.value = `tin moi ${i + 1}`;
    await messageForm.listeners.submit({ preventDefault() {} });
    const message = bob.history.at(-1);
    bob.sockets[0].message({ ...message, type: "message.created", message_id: message.id, recipient_id: bob.bobID });
    bob.sockets[0].message({ ...message, type: "message.created", message_id: message.id, recipient_id: bob.bobID });
  }
  await settle();
  assert.equal(countRequests(alice, "POST", `/threads/${alice.threadID}/messages`), 3);
  assert.equal(countThreadSummaries(alice), aliceThreadsBefore);
  assert.equal(countThreadSummaries(bob), bobThreadsBefore);
  assert.equal(countRequests(bob, "GET", `/threads/${bob.threadID}/messages?`), bobHistoryBefore);
  assert.equal(bob.state.messages.size, 13);
  assert.equal(bob.state.threadsByPeer.get(alice.aliceID).unread_count, 3);
  assert.equal(alice.timerDelays.get(alice.state.threadSummaryTimer), 750);
  const summaryRefresh = alice.timers.get(alice.state.threadSummaryTimer);
  summaryRefresh();
  await settle();
  assert.equal(countThreadSummaries(alice), aliceThreadsBefore + 1);

  alice.sockets[0].close();
  bob.sockets[0].close();
  contentInput.value = "tin trong luc gateway tat";
  await messageForm.listeners.submit({ preventDefault() {} });
  await settle();
  assert.equal(bob.state.messages.size, 13);
  assert.equal(countRequests(bob, "GET", `/threads/${bob.threadID}/messages?`), bobHistoryBefore);
  const outageSummary = alice.timers.get(alice.state.threadSummaryTimer);
  outageSummary();
  await settle();
  assert.equal(countThreadSummaries(alice), aliceThreadsBefore + 2);
  await runReconnectTimer(alice);
  await runReconnectTimer(bob);
  assert.equal(bob.state.messages.size, 13);
  assert.equal(countRequests(bob, "GET", `/threads/${bob.threadID}/messages?`), bobHistoryBefore);
  assert.equal(countThreadSummaries(bob), bobThreadsBefore);
  alice.sockets[1].open();
  bob.sockets[1].open();
  await settle();
  assert.equal(bob.state.messages.size, 14);
  assert.equal(countRequests(bob, "GET", `/threads/${bob.threadID}/messages?`), bobHistoryBefore + 1);
  assert.equal(bob.ticketCount, 2);
  assert.equal(bob.state.threadsByPeer.get(alice.aliceID).unread_count, 4);
  assert.equal(countThreadSummaries(bob), bobThreadsBefore + 1);
  assert.equal(countThreadSummaries(alice), aliceThreadsBefore + 3);
});

test("online with a healthy socket does nothing; a stale socket only fetches after reconnect", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  for (let seq = 11; seq <= 80; seq++) app.history.push({ ...app.history[0], id: `message-${seq}`, seq });
  const priorHistoryRequests = app.cursors.length;
  // A spurious online event must not replace a healthy socket.
  app.window.listeners.online();
  assert.equal(app.sockets.length, 1);
  // Gateway is gone, but the browser has not fired onclose and readyState still says OPEN.
  app.state.socketLastActivityAt = Date.now() - 31000;
  app.document.listeners.visibilitychange();
  await settle();
  assert.equal(app.state.messages.size, 10);
  assert.equal(app.cursors.length, priorHistoryRequests);
  assert.equal(app.sockets[0].closed, false);
  assert.equal(app.timerDelays.get(app.state.socketHealthTimer), 5000);
  app.timers.get(app.state.socketHealthTimer)();
  await settle();
  assert.equal(app.sockets[0].closed, true);
  await runReconnectTimer(app);
  assert.equal(app.sockets.length, 2);
  assert.equal(app.ticketCount, 2);
  assert.equal(app.maxActiveHistory, 1);
  app.sockets[1].open();
  await settle();
  assert.equal(app.state.messages.size, 80);
  assert.equal(app.state.syncedSeq, 80);
  assert.equal(app.state.messageIDs.size, 80);
  assert.deepEqual(app.cursors.slice(-3), [null, 51, 21]);
  for (const seq of [80, 79]) {
    const message = app.history[seq - 1];
    app.sockets[1].message({ ...message, type: "message.created", message_id: message.id, recipient_id: app.bobID });
  }
  assert.equal(app.state.messages.size, 80);
  assert.deepEqual([...app.state.messages.keys()].sort((a, b) => a - b), Array.from({ length: 80 }, (_, i) => i + 1));
});

test("missing heartbeat replaces a half-open socket and backfills on new open", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  for (let seq = 11; seq <= 80; seq++) app.history.push({ ...app.history[0], id: `message-${seq}`, seq });
  const priorHistoryRequests = app.cursors.length;
  const timerID = app.state.socketHealthTimer;
  const timeout = app.timers.get(timerID);
  assert.equal(typeof timeout, "function");
  app.timers.delete(timerID);
  timeout();
  await settle();
  assert.equal(app.sockets[0].closed, true);
  await runReconnectTimer(app);
  assert.equal(app.sockets.length, 2);
  assert.equal(app.ticketCount, 2);
  assert.equal(app.cursors.length, priorHistoryRequests);
  app.sockets[1].open();
  await settle();
  assert.equal(app.state.messages.size, 80);
  assert.equal(app.state.syncedSeq, 80);
  assert.equal(app.state.messageIDs.size, 80);
  assert.equal(app.maxActiveHistory, 1);
});

test("failed reconnects keep requesting fresh tickets until the gateway returns", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  for (let seq = 11; seq <= 80; seq++) app.history.push({ ...app.history[0], id: `message-${seq}`, seq });
  const priorHistoryRequests = app.cursors.length;
  app.sockets[0].close();
  let timerID = app.state.reconnectTimer;
  let retry = app.timers.get(timerID);
  app.timers.delete(timerID);
  retry();
  await settle();
  assert.equal(app.sockets.length, 2);
  app.sockets[1].close(); // Gateway still unavailable during this handshake.
  timerID = app.state.reconnectTimer;
  retry = app.timers.get(timerID);
  app.timers.delete(timerID);
  retry();
  await settle();
  assert.equal(app.sockets.length, 3);
  assert.equal(app.ticketCount, 3);
  assert.equal(app.cursors.length, priorHistoryRequests);
  app.sockets[2].open();
  await settle();
  assert.equal(app.state.messages.size, 80);
  assert.equal(app.state.syncedSeq, 80);
  assert.equal(app.maxActiveHistory, 1);
});

test("watchdog and short-lived sockets preserve reconnect backoff until a heartbeat", async () => {
  const app = makeApp();
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  const watchdogID = app.state.socketHealthTimer;
  const watchdog = app.timers.get(watchdogID);
  app.timers.delete(watchdogID);
  app.timerDelays.delete(watchdogID);
  watchdog();
  assert.equal(app.timerDelays.get(app.state.reconnectTimer), 1000);
  await runReconnectTimer(app);
  app.sockets[1].open();
  app.sockets[1].close();
  assert.equal(app.timerDelays.get(app.state.reconnectTimer), 2000);
  await runReconnectTimer(app);
  app.sockets[2].open();
  app.sockets[2].message({ type: "heartbeat" });
  app.sockets[2].close();
  assert.equal(app.timerDelays.get(app.state.reconnectTimer), 1000);
});

test("heartbeat refreshes watchdog; a canceled timeout cannot reconnect", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  const priorHistoryRequests = app.cursors.length;
  const oldTimerID = app.state.socketHealthTimer;
  const oldTimeout = app.timers.get(oldTimerID);
  for (let i = 0; i < 3; i++) app.sockets[0].message({ type: "heartbeat" });
  assert.notEqual(app.state.socketHealthTimer, oldTimerID);
  assert.equal(app.timers.has(oldTimerID), false);
  oldTimeout();
  await settle();
  assert.equal(app.sockets.length, 1);
  assert.equal(app.ticketCount, 1);
  assert.equal(app.cursors.length, priorHistoryRequests);
});

test("90 seconds of five-second heartbeats keep one socket and one ticket", async () => {
  const app = makeApp({ fakeClock: true });
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  const requests = app.requests.length;
  for (let i = 0; i < 18; i++) {
    app.advance(5000);
    app.sockets[0].message({ type: "heartbeat" });
  }
  await settle();
  assert.equal(app.ticketCount, 1);
  assert.equal(app.sockets.length, 1);
  assert.equal(app.sockets[0].closed, false);
  assert.equal(app.requests.length, requests);
});

test("a delayed background watchdog does not replace a healthy open socket", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  const before = app.requests.length;
  app.document.visibilityState = "hidden";
  app.document.listeners.visibilitychange();
  const watchdog = app.timers.get(app.state.socketHealthTimer);
  watchdog?.(); // A timeout already queued before the tab was hidden.
  await settle();
  assert.equal(app.sockets[0].closed, false);
  assert.equal(app.ticketCount, 1);
  assert.equal(app.requests.length, before);
  app.state.socketLastActivityAt = Date.now() - 30000;
  app.document.visibilityState = "visible";
  app.document.listeners.visibilitychange();
  assert.equal(app.sockets[0].closed, false);
  app.sockets[0].message({ type: "heartbeat" });
  await settle();
  assert.equal(app.ticketCount, 1);
  assert.equal(app.sockets.length, 1);
  assert.equal(app.requests.length, before);
});

test("a hidden tab can idle for 90 seconds without a ticket and accept a queued heartbeat on resume", async () => {
  const app = makeApp({ fakeClock: true });
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  const requests = app.requests.length;
  app.document.visibilityState = "hidden";
  app.document.listeners.visibilitychange();
  app.advance(90000);
  assert.equal(app.ticketCount, 1);
  assert.equal(app.sockets[0].closed, false);
  app.document.visibilityState = "visible";
  app.document.listeners.visibilitychange();
  assert.equal(app.timerDelays.get(app.state.socketHealthTimer), 5000);
  app.advance(1000);
  app.sockets[0].message({ type: "heartbeat" });
  app.advance(4000);
  assert.equal(app.ticketCount, 1);
  assert.equal(app.sockets.length, 1);
  assert.equal(app.requests.length, requests);
});

test("a socket stuck in CONNECTING is replaced with a new ticket", async () => {
  const app = makeApp();
  await vm.runInContext("connectWebSocket()", app.context);
  assert.equal(app.sockets.length, 1);
  const timerID = app.state.socketHealthTimer;
  const timeout = app.timers.get(timerID);
  app.timers.delete(timerID);
  timeout();
  await settle();
  await runReconnectTimer(app);
  assert.equal(app.sockets.length, 2);
  assert.equal(app.sockets[0].closed, true);
  assert.equal(app.ticketCount, 2);
});

test("a hung ticket fetch is aborted before the next reconnect attempt", async () => {
  const app = makeApp({ hangFirstTicket: true });
  const firstAttempt = vm.runInContext("connectWebSocket()", app.context);
  await settle();
  assert.equal(app.ticketAttempts, 1);
  assert.equal(app.activeTickets, 1);
  const timeout = app.timers.get(app.state.socketHealthTimer);
  timeout();
  await firstAttempt;
  assert.equal(app.activeTickets, 0);
  await runReconnectTimer(app);
  assert.equal(app.ticketAttempts, 2);
  assert.equal(app.maxActiveTickets, 1);
  assert.equal(app.sockets.length, 1);
});

test("showing a hidden tab with a healthy socket does not fetch history", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  app.document.visibilityState = "hidden";
  for (let seq = 11; seq <= 80; seq++) app.history.push({ ...app.history[0], id: `message-${seq}`, seq });
  const priorHistoryRequests = app.cursors.length;
  app.document.visibilityState = "visible";
  app.document.listeners.visibilitychange();
  await settle();
  assert.equal(app.state.messages.size, 10);
  assert.equal(app.cursors.length, priorHistoryRequests);
  assert.equal(app.ticketCount, 1);
  assert.equal(app.sockets.length, 1);
});

test("showing a hidden tab replaces a stale socket and catches up after open", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  await settle();
  app.document.visibilityState = "hidden";
  for (let seq = 11; seq <= 80; seq++) app.history.push({ ...app.history[0], id: `message-${seq}`, seq });
  app.state.socketLastActivityAt = Date.now() - 31000;
  app.document.visibilityState = "visible";
  app.document.listeners.visibilitychange();
  await settle();
  assert.equal(app.state.messages.size, 10);
  assert.equal(app.timerDelays.get(app.state.socketHealthTimer), 5000);
  app.timers.get(app.state.socketHealthTimer)();
  await settle();
  await runReconnectTimer(app);
  assert.equal(app.sockets.length, 2);
  assert.equal(app.ticketCount, 2);
  app.sockets[1].open();
  await settle();
  assert.equal(app.state.messages.size, 80);
});

test("logout and account switch invalidate an old watchdog callback", async () => {
  const app = makeApp();
  await vm.runInContext("connectWebSocket()", app.context);
  app.sockets[0].open();
  const oldTimeout = app.timers.get(app.state.socketHealthTimer);
  vm.runInContext("clearSession()", app.context);
  assert.equal(app.timers.size, 0);
  app.state.token = "second-account-token";
  app.state.currentUserID = app.aliceID;
  app.state.sessionVersion += 1;
  await vm.runInContext("connectWebSocket()", app.context);
  oldTimeout();
  await settle();
  assert.equal(app.sockets.length, 2);
  assert.equal(app.ticketCount, 2);
  assert.equal(app.sockets[0].closed, true);
});

test("read marker waits for visible received messages without crossing a seq gap", async () => {
  const app = makeApp();
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  app.state.messages.delete(2);
  const visible = new Set([3]);
  const historyElement = vm.runInContext("historyElement", app.context);
  historyElement.querySelectorAll = () => [...visible].map((seq) => ({
    dataset: { seq: String(seq), kind: "text" },
    getBoundingClientRect: () => ({ top: 10, bottom: 30 }),
  }));
  vm.runInContext("recordVisibleMessages()", app.context);
  await settle();
  assert.deepEqual(app.readMarkers, []);
  visible.add(1);
  vm.runInContext("recordVisibleMessages()", app.context);
  await settle();
  assert.deepEqual(app.readMarkers, [1]);
  app.state.messages.set(2, { ...app.history[1] });
  visible.add(2);
  vm.runInContext("recordVisibleMessages()", app.context);
  await settle();
  assert.deepEqual(app.readMarkers, [1, 3]);
  assert.equal(app.state.threadsByPeer.get(app.aliceID).unread_count, 7);
  assert.equal(app.requests.filter((r) => r.method === "GET" && r.url === "/threads").length, 0);
});

test("out-of-order and duplicate events update unread once per seq", async () => {
  const app = makeApp({ initialReadSeq: 10 });
  await vm.runInContext(`openConversation("${app.aliceID}")`, app.context);
  const makeEvent = (seq) => ({ ...app.history[0], id: `message-${seq}`, message_id: `message-${seq}`,
    thread_id: app.threadID, seq, type: "message.created", recipient_id: app.bobID });
  const deliver = vm.runInContext("handleSocketMessage", app.context);
  deliver(JSON.stringify(makeEvent(12)));
  deliver(JSON.stringify(makeEvent(11)));
  deliver(JSON.stringify(makeEvent(12)));
  assert.equal(app.state.threadsByPeer.get(app.aliceID).unread_count, 2);
  assert.equal(app.state.threadsByPeer.get(app.aliceID).last_seq, 12);
  assert.equal(app.requests.filter((r) => r.method === "GET" && r.url === "/threads").length, 0);
});

test("a burst of sends refreshes peer read status once", async () => {
  const app = makeApp({ user: "alice" });
  await vm.runInContext(`openConversation("${app.bobID}")`, app.context);
  const form = vm.runInContext("messageForm", app.context);
  const input = vm.runInContext("contentInput", app.context);
  for (let i = 0; i < 3; i++) {
    input.value = `sent ${i}`;
    await form.listeners.submit({ preventDefault() {} });
  }
  assert.equal(countThreadSummaries(app), 0);
  app.setPeerReadSeq(12);
  app.timers.get(app.state.threadSummaryTimer)();
  await settle();
  assert.equal(countThreadSummaries(app), 1);
  assert.equal(app.state.peerLastReadSeq, 12);
});
