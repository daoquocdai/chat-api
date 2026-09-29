const assert = require("node:assert/strict");
const test = require("node:test");
const { createApp, ids } = require("./thread-cache-harness.cjs");

const settle = () => new Promise((resolve) => setTimeout(resolve, 25));

test("visibility and online do not bypass an offline reconnect timer", async () => {
  const app = createApp();
  await app.call("connectWebSocket");
  app.sockets[0].open();
  app.sockets[0].close();
  const timer = app.state.reconnectTimer;
  assert.notEqual(timer, null);
  for (let i = 0; i < 6; i += 1) {
    app.document.visibilityState = "hidden";
    app.document.listeners.visibilitychange();
    app.document.visibilityState = "visible";
    app.document.listeners.visibilitychange();
    app.window.listeners.online();
  }
  await settle();
  assert.equal(app.state.reconnectTimer, timer);
  assert.equal(app.count("POST", "/auth/ws-ticket"), 1);
  assert.equal(app.sockets.length, 1);
  app.timers.get(timer)();
  await settle();
  assert.equal(app.count("POST", "/auth/ws-ticket"), 2);
  assert.equal(app.sockets.length, 2);
});

test("A B A B uses loaded per-thread cache including an empty first page", async () => {
  const app = createApp();
  app.addMessage(ids.a);
  await app.call("loadThreads");
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  assert.equal(app.state.threadCaches.get(ids.b).messagesLoaded, true);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 0);
  const before = app.requests.length;
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  assert.equal(app.requests.length, before);
  assert.equal(app.count("POST", "/threads/direct"), 0);
  assert.equal(app.count("GET", `/threads/${ids.a}/messages?`), 1);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`), 1);
  assert.equal(app.requests.filter((request) => request.method === "GET" && request.url === "/threads").length, 1);
});

test("threads absent from the first list need direct POST only on their first open", async () => {
  const app = createApp({ knownThreads: [] });
  await app.call("loadThreads");
  assert.equal(app.state.threadsByPeer.size, 0);
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  assert.equal(app.count("POST", "/threads/direct"), 2);
  assert.equal(app.count("GET", `/threads/${ids.a}/messages?`), 1);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`), 1);
  const before = app.requests.length;
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  assert.equal(app.requests.length, before);
  assert.equal(app.state.threadsByPeer.get(ids.alice).id, ids.a);
  assert.equal(app.state.threadsByPeer.get(ids.carol).id, ids.b);
});

test("older-page cursor and loaded pages stay with their thread across switches", async () => {
  const app = createApp();
  for (let i = 0; i < 45; i += 1) app.addMessage(ids.a);
  await app.call("loadThreads");
  await app.call("openConversation", ids.alice);
  const cache = app.state.threadCaches.get(ids.a);
  assert.equal(cache.messages.size, 30);
  assert.equal(cache.nextCursor, 16);
  await app.call("openConversation", ids.carol);
  await app.call("openConversation", ids.alice);
  assert.equal(cache.nextCursor, 16);
  await app.call("loadOlderMessages");
  assert.equal(cache.messages.size, 45);
  assert.equal(cache.nextCursor, null);
  const before = app.requests.length;
  await app.call("openConversation", ids.carol);
  await app.call("openConversation", ids.alice);
  assert.equal(app.requests.length, before);
});

test("inactive thread receives events and its own gap catch-up without loading another thread", async () => {
  const app = createApp();
  app.addMessage(ids.a);
  app.addMessage(ids.b);
  await app.call("loadThreads");
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  await app.call("openConversation", ids.alice);
  const before = app.count("GET", `/threads/${ids.b}/messages?`);
  const second = app.addMessage(ids.b);
  app.event(second);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 2);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`), before);
  assert.equal(app.state.currentCache.threadID, ids.a);
  app.addMessage(ids.b); // seq 3 arrives in PostgreSQL, but its event is missed.
  const fourth = app.addMessage(ids.b);
  app.event(fourth);
  await settle();
  assert.equal(app.state.threadCaches.get(ids.b).syncedSeq, 4);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 4);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`), before + 1);
  assert.equal(app.count("GET", `/threads/${ids.a}/messages?`), 1);
  const beforeSwitch = app.requests.length;
  await app.call("openConversation", ids.carol);
  assert.equal(app.requests.length, beforeSwitch);
});

test("reconnect checks and backfills every loaded thread across multiple pages", async () => {
  const app = createApp();
  for (let i = 0; i < 10; i += 1) { app.addMessage(ids.a); app.addMessage(ids.b); }
  await app.call("loadThreads");
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  await app.call("connectWebSocket");
  app.sockets[0].open();
  await settle();
  app.sockets[0].close();
  for (let i = 0; i < 70; i += 1) { app.addMessage(ids.a); app.addMessage(ids.b); }
  const listCount = () => app.requests.filter((request) => request.method === "GET" && request.url === "/threads").length;
  const before = { list: listCount(), a: app.count("GET", `/threads/${ids.a}/messages?`),
    b: app.count("GET", `/threads/${ids.b}/messages?`) };
  app.timers.get(app.state.reconnectTimer)();
  await settle();
  app.sockets[1].open();
  await settle();
  assert.equal(app.state.threadCaches.get(ids.a).syncedSeq, 80);
  assert.equal(app.state.threadCaches.get(ids.b).syncedSeq, 80);
  assert.equal(app.state.threadCaches.get(ids.a).messages.size, 80);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 80);
  assert.equal(listCount() - before.list, 1);
  assert.equal(app.count("GET", `/threads/${ids.a}/messages?`) - before.a, 3);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`) - before.b, 3);
  const beforeSwitch = app.requests.length;
  await app.call("openConversation", ids.alice);
  await app.call("openConversation", ids.carol);
  assert.equal(app.requests.length, beforeSwitch);
});

test("late history and direct responses stay with their thread and old account is ignored", async () => {
  const app = createApp();
  app.addMessage(ids.a);
  app.addMessage(ids.b);
  await app.call("loadThreads");
  app.blockHistory(ids.a);
  const openingA = app.call("openConversation", ids.alice);
  await settle();
  await app.call("openConversation", ids.carol);
  app.releaseHistory(ids.a);
  await openingA;
  assert.equal(app.state.currentCache.threadID, ids.b);
  assert.equal(app.state.threadCaches.get(ids.a).messages.size, 1);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 1);
  const beforeSwitch = app.requests.length;
  await app.call("openConversation", ids.alice);
  assert.equal(app.requests.length, beforeSwitch);

  const second = createApp();
  second.blockDirect(ids.a);
  const slowDirect = second.call("openConversation", ids.alice);
  await settle();
  const sameDirect = second.call("openConversation", ids.alice);
  assert.equal(second.count("POST", "/threads/direct"), 1);
  await second.call("openConversation", ids.carol);
  second.releaseDirect(ids.a);
  await Promise.all([slowDirect, sameDirect]);
  assert.equal(second.state.threadID, ids.b);

  const third = createApp();
  third.blockHistory(ids.a);
  await third.call("loadThreads");
  const oldRequest = third.call("openConversation", ids.alice);
  await settle();
  third.call("clearSession");
  third.releaseHistory(ids.a);
  await oldRequest;
  assert.equal(third.state.threadCaches.size, 0);
});

test("an event for an unopened thread does not mark its history loaded", async () => {
  const app = createApp();
  await app.call("loadThreads");
  await app.call("openConversation", ids.alice);
  const message = app.addMessage(ids.b);
  app.event(message);
  assert.equal(app.state.threadCaches.get(ids.b).messagesLoaded, false);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`), 0);
  await app.call("openConversation", ids.carol);
  assert.equal(app.state.threadCaches.get(ids.b).messagesLoaded, true);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 1);
  assert.equal(app.count("GET", `/threads/${ids.b}/messages?`), 1);
});

test("late send response updates its originating cache without changing the new conversation", async () => {
  const app = createApp();
  await app.call("loadThreads");
  await app.call("openConversation", ids.alice);
  app.blockSend(ids.a);
  const input = app.nodes.get("#content");
  input.value = "message for Alice";
  const send = app.nodes.get("#message-form").listeners.submit({ preventDefault() {} });
  await settle();
  await app.call("openConversation", ids.carol);
  input.value = "draft for Carol";
  app.releaseSend(ids.a);
  await send;
  assert.equal(app.state.currentCache.threadID, ids.b);
  assert.equal(input.value, "draft for Carol");
  assert.equal(app.state.threadCaches.get(ids.a).messages.size, 1);
  assert.equal(app.state.threadCaches.get(ids.b).messages.size, 0);
  const before = app.requests.length;
  await app.call("openConversation", ids.alice);
  assert.equal(app.requests.length, before);
});

test("read markers remain per thread and do not repeat on a cache switch", async () => {
  const app = createApp();
  app.addMessage(ids.a);
  app.addMessage(ids.a);
  app.addMessage(ids.b);
  await app.call("loadThreads");
  const history = app.nodes.get("#history");
  history.querySelectorAll = () => [...app.state.currentCache.messages.values()].map((message) => ({
    dataset: { kind: message.kind, seq: String(message.seq) },
    getBoundingClientRect: () => ({ top: 0, bottom: 100 }),
  }));
  await app.call("openConversation", ids.alice);
  app.call("recordVisibleMessages");
  await settle();
  assert.equal(app.state.threadCaches.get(ids.a).lastReadSeq, 2);
  await app.call("openConversation", ids.carol);
  app.call("recordVisibleMessages");
  await settle();
  assert.equal(app.state.threadCaches.get(ids.b).lastReadSeq, 1);
  assert.deepEqual(app.readMarkers.map((item) => item.target), [2, 1]);
  await app.call("openConversation", ids.alice);
  app.call("recordVisibleMessages");
  await settle();
  assert.equal(app.readMarkers.length, 2);
});
