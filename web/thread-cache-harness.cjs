const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const realtime = require("./realtime-core.js");

const ids = {
  bob: "00000000-0000-4000-8000-000000000002",
  alice: "00000000-0000-4000-8000-000000000001",
  carol: "00000000-0000-4000-8000-000000000004",
  a: "00000000-0000-4000-8000-000000000003",
  b: "00000000-0000-4000-8000-000000000005",
};

function element() {
  return {
    hidden: false, disabled: false, value: "", textContent: "", innerHTML: "",
    scrollHeight: 0, scrollTop: 0, clientHeight: 500, dataset: {}, elements: {}, children: [],
    listeners: {}, addEventListener(name, fn) { this.listeners[name] = fn; },
    replaceChildren() { this.children = []; }, append(...children) { this.children.push(...children); },
    focus() {}, reset() {}, setAttribute() {}, querySelectorAll() { return []; },
    getBoundingClientRect() { return { top: 0, bottom: 500 }; },
  };
}

function createApp(options = {}) {
  const source = options.source || fs.readFileSync(path.join(__dirname, "app.js"), "utf8");
  const nodes = new Map();
  const document = {
    visibilityState: "visible", listeners: {},
    querySelector(selector) {
      if (!nodes.has(selector)) nodes.set(selector, element());
      return nodes.get(selector);
    },
    createElement: element, addEventListener(name, fn) { this.listeners[name] = fn; },
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
    constructor(url) { this.url = url; this.readyState = 0; sockets.push(this); }
    open() { this.readyState = 1; this.onopen?.(); }
    message(event) { this.onmessage?.({ data: JSON.stringify(event) }); }
    close() { this.readyState = 3; this.onclose?.(); }
  }
  const timers = new Map();
  let nextTimer = 1;
  let ticketCount = 0;
  let uuidCount = 0;
  const histories = new Map([[ids.a, []], [ids.b, []]]);
  const requests = [];
  const readMarkers = [];
  const reads = new Map([[ids.a, 0], [ids.b, 0]]);
  const blockedHistory = new Map();
  const blockedDirect = new Map();
  const blockedSend = new Map();
  const knownThreads = new Set(options.knownThreads || [ids.a, ids.b]);
  const response = (body) => ({ ok: true, status: 200, json: async () => body });
  const peerFor = (threadID) => threadID === ids.a ? ids.alice : ids.carol;
  const summary = (threadID) => {
    const history = histories.get(threadID);
    const lastReadSeq = reads.get(threadID);
    return { id: threadID, peer: { id: peerFor(threadID), username: threadID === ids.a ? "alice" : "carol" },
      last_seq: history.at(-1)?.seq || 0, last_read_seq: lastReadSeq, peer_last_read_seq: 0,
      unread_count: history.filter((message) => message.seq > lastReadSeq && message.sender_id !== ids.bob).length };
  };
  const historyPage = (threadID, url) => {
    const params = new URL(url, "http://localhost").searchParams;
    const before = params.has("before_seq") ? Number(params.get("before_seq")) : Infinity;
    const all = histories.get(threadID).filter((message) => message.seq < before).reverse();
    const messages = all.slice(0, Number(params.get("limit")));
    return response({ messages, next_cursor: all.length > messages.length ? messages.at(-1).seq : null });
  };
  async function fetch(url, request = {}) {
    requests.push({ method: request.method || "GET", url });
    if (url === "/auth/ws-ticket") return response({ ticket: `ticket-${++ticketCount}`, ws_url: "ws://localhost:8081/ws" });
    if (url === "/threads") return response([...knownThreads].map(summary));
    if (url === "/threads/direct") {
      const peerID = JSON.parse(request.body).peer_id;
      const threadID = peerID === ids.alice ? ids.a : ids.b;
      knownThreads.add(threadID);
      if (blockedDirect.has(threadID)) {
        return new Promise((resolve) => blockedDirect.get(threadID).push(() => resolve(response(summary(threadID)))));
      }
      return response(summary(threadID));
    }
    for (const threadID of [ids.a, ids.b]) {
      if (url.startsWith(`/threads/${threadID}/messages?`)) {
        if (blockedHistory.has(threadID)) {
          return new Promise((resolve) => blockedHistory.get(threadID).push(() => resolve(historyPage(threadID, url))));
        }
        return historyPage(threadID, url);
      }
      if (url === `/threads/${threadID}/messages` && request.method === "POST") {
        const body = JSON.parse(request.body);
        const message = addMessage(threadID, ids.bob, body.message_id, body.content);
        if (blockedSend.has(threadID)) {
          return new Promise((resolve) => blockedSend.get(threadID).push(() => resolve(response(message))));
        }
        return response(message);
      }
      if (url === `/threads/${threadID}/read`) {
        const target = JSON.parse(request.body).last_read_seq;
        readMarkers.push({ threadID, target });
        reads.set(threadID, Math.max(reads.get(threadID), target));
        return response({ last_read_seq: reads.get(threadID) });
      }
    }
    throw new Error(`unexpected request: ${request.method || "GET"} ${url}`);
  }
  function addMessage(threadID, senderID = peerFor(threadID), messageID, content = "hello") {
    const history = histories.get(threadID);
    const message = { id: messageID || `message-${threadID}-${history.length + 1}`,
      thread_id: threadID, sender_id: senderID, seq: history.length + 1, kind: "text",
      content_format: "plaintext", content, created_at: "2026-09-28T00:00:00Z" };
    history.push(message);
    return message;
  }
  const sandbox = {
    document, window, sessionStorage, fetch, WebSocket: FakeWebSocket,
    MiniHermesRealtime: realtime, Headers, URL, URLSearchParams, AbortController, atob,
    crypto: { randomUUID: () => `sent-${++uuidCount}` },
    requestAnimationFrame: () => 1, cancelAnimationFrame() {},
    setTimeout(fn) { const id = nextTimer++; timers.set(id, fn); return id; },
    clearTimeout(id) { timers.delete(id); },
  };
  const context = vm.createContext(sandbox);
  vm.runInContext(source, context);
  const state = vm.runInContext("state", context);
  Object.assign(state, { sessionVersion: 1, token: "test-token", currentUserID: ids.bob,
    currentUsername: "bob", users: [{ id: ids.bob, username: "bob" },
      { id: ids.alice, username: "alice" }, { id: ids.carol, username: "carol" }] });
  nodes.get("#chat-view").hidden = false;
  const call = (name, ...args) => vm.runInContext(name, context)(...args);
  return {
    context, state, document, window, nodes, sockets, timers, histories, requests, readMarkers,
    addMessage, call,
    event(message) { call("handleSocketMessage", JSON.stringify({ ...message, type: "message.created",
      message_id: message.id, recipient_id: ids.bob })); },
    count(method, prefix) { return requests.filter((request) => request.method === method && request.url.startsWith(prefix)).length; },
    blockHistory(threadID) { blockedHistory.set(threadID, []); },
    releaseHistory(threadID) { for (const release of blockedHistory.get(threadID) || []) release(); blockedHistory.delete(threadID); },
    blockDirect(threadID) { blockedDirect.set(threadID, []); },
    releaseDirect(threadID) { for (const release of blockedDirect.get(threadID) || []) release(); blockedDirect.delete(threadID); },
    blockSend(threadID) { blockedSend.set(threadID, []); },
    releaseSend(threadID) { for (const release of blockedSend.get(threadID) || []) release(); blockedSend.delete(threadID); },
  };
}

module.exports = { createApp, ids };
