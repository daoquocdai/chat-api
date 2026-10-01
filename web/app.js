const sessionTokenKey = "mini-hermes.access-token";
const sessionUsernameKey = "mini-hermes.username";
const messagePageLimit = 30;
const threadSummaryDebounceMs = 750;
const websocketConnectTimeoutMs = 10000;
const websocketHeartbeatTimeoutMs = 15000;
const websocketResumeGraceMs = 5000;
const pendingSendMismatchMessage = "Tin trước chưa được xác nhận. Hãy lưu nội dung mới ở nơi khác và khôi phục đúng nội dung tin cũ để bấm Gửi lại; chỉ gửi tin mới sau khi tin cũ thành công.";

const authView = document.querySelector("#auth-view");
const chatView = document.querySelector("#chat-view");
const registerForm = document.querySelector("#register-form");
const loginForm = document.querySelector("#login-form");
const logoutButton = document.querySelector("#logout-button");
const currentUsername = document.querySelector("#current-username");
const peerList = document.querySelector("#peer-list");
const peerName = document.querySelector("#peer-name");
const historyElement = document.querySelector("#history");
const messageForm = document.querySelector("#message-form");
const contentInput = document.querySelector("#content");
const sendButton = document.querySelector("#send-button");
const loadOlderButton = document.querySelector("#load-older-button");
const noticeElement = document.querySelector("#notice");
const errorElement = document.querySelector("#error");

const state = {
  token: "",
  sessionVersion: 0,
  currentUserID: "",
  currentUsername: "",
  users: [],
  threadsByPeer: new Map(),
  threadCaches: new Map(),
  directRequestsByPeer: new Map(),
  threadEventSeqs: new Map(),
  threadSummaryBaseSeqs: new Map(),
  peerID: "",
  threadID: "",
  currentCache: null,
  conversationVersion: 0,
  threadListRequest: null,
  threadListDirty: false,
  threadListCatchup: false,
  threadSummaryTimer: null,
  socket: null,
  socketConnecting: false,
  socketGeneration: 0,
  socketTicketAbort: null,
  socketHealthTimer: null,
  socketLastActivityAt: 0,
  socketSummaryStale: false,
  reconnectTimer: null,
  reconnectDelay: 1000,
};

function showNotice(message) {
  noticeElement.textContent = message;
  noticeElement.hidden = !message;
}

function showError(message) {
  errorElement.textContent = message;
  errorElement.hidden = !message;
}

function updateSendButton() {
  const pending = state.currentCache?.pendingSend;
  sendButton.textContent = pending ? "Gửi lại" : "Gửi";
  sendButton.disabled = !state.threadID || Boolean(pending?.inFlight);
}

function decodeSubject(token) {
  try {
    const payload = token.split(".")[1];
    const normalized = payload.replace(/-/g, "+").replace(/_/g, "/");
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "=");
    return JSON.parse(atob(padded)).sub || "";
  } catch {
    return "";
  }
}

async function readResponse(response) {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.error || "Yêu cầu không thành công.");
    error.status = response.status;
    throw error;
  }
  return body;
}

async function apiRequest(path, options = {}, authenticated = true) {
  const requestToken = state.token;
  const requestSessionVersion = state.sessionVersion;
  const headers = new Headers(options.headers || {});
  if (authenticated) {
    headers.set("Authorization", `Bearer ${requestToken}`);
  }

  const response = await fetch(path, { ...options, headers });
  if (
    authenticated &&
    response.status === 401 &&
    requestToken === state.token &&
    requestSessionVersion === state.sessionVersion
  ) {
    clearSession();
    throw new Error("Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.");
  }
  return readResponse(response);
}

async function sendMessageWithRetry(snapshot, messageID, content) {
  const request = {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message_id: messageID, content }),
  };

  for (let attempt = 0; attempt < 2; attempt += 1) {
    try {
      return await apiRequest(`/threads/${snapshot.threadID}/messages`, request);
    } catch (error) {
      const retryable = error.status === undefined || error.status >= 500;
      if (attempt === 1 || !retryable || !cacheSnapshotMatches(snapshot)) {
        throw error;
      }
    }
  }

  throw new Error("Không thể gửi tin nhắn.");
}

function resetConversation() {
  state.peerID = "";
  state.threadID = "";
  state.currentCache = null;
  state.conversationVersion += 1;
  peerName.textContent = "Chọn một tài khoản";
  historyElement.innerHTML = '<p class="empty">Chọn một tài khoản để bắt đầu chat.</p>';
  contentInput.value = "";
  contentInput.disabled = true;
  updateSendButton();
  loadOlderButton.hidden = true;
  loadOlderButton.disabled = true;
}

function clearSession() {
  stopWebSocket();
  sessionStorage.removeItem(sessionTokenKey);
  sessionStorage.removeItem(sessionUsernameKey);
  state.sessionVersion += 1;
  state.token = "";
  state.currentUserID = "";
  state.currentUsername = "";
  state.users = [];
  state.threadsByPeer = new Map();
  state.threadCaches = new Map();
  state.directRequestsByPeer = new Map();
  state.threadEventSeqs = new Map();
  state.threadSummaryBaseSeqs = new Map();
  state.threadListRequest = null;
  state.threadListDirty = false;
  state.threadListCatchup = false;
  if (state.threadSummaryTimer !== null) {
    clearTimeout(state.threadSummaryTimer);
    state.threadSummaryTimer = null;
  }
  state.socketSummaryStale = false;
  resetConversation();
  authView.hidden = false;
  chatView.hidden = true;
  logoutButton.hidden = true;
  currentUsername.textContent = "";
}

function showChatView() {
  authView.hidden = true;
  chatView.hidden = false;
  logoutButton.hidden = false;
  currentUsername.textContent = state.currentUsername;
}

function currentSessionMatches(sessionVersion, token) {
  return sessionVersion === state.sessionVersion && token === state.token && Boolean(token);
}

function conversationSnapshot() {
  return {
    sessionVersion: state.sessionVersion,
    token: state.token,
    version: state.conversationVersion,
    threadID: state.threadID,
    cache: state.currentCache,
  };
}

function newThreadCache(threadID) {
  return {
    threadID,
    messages: new Map(), messageIDs: new Map(), messagesLoaded: false,
    syncedSeq: 0, nextCursor: null, lastReadSeq: 0, peerLastReadSeq: 0,
    seenReceivedSeqs: new Set(), initialHistoryRequest: null,
    syncAfterInitial: false, catchupRequest: null, catchupTargetSeq: 0,
    olderRequest: null, readRequest: null, pendingReadSeq: 0,
    pendingSend: null, draftContent: "",
  };
}

function cacheForThread(threadID) {
  let cache = state.threadCaches.get(threadID);
  if (!cache) {
    cache = newThreadCache(threadID);
    state.threadCaches.set(threadID, cache);
  }
  return cache;
}

function cacheSnapshot(cache) {
  return { sessionVersion: state.sessionVersion, token: state.token,
    threadID: cache.threadID, cache };
}

function cacheSnapshotMatches(snapshot) {
  return currentSessionMatches(snapshot.sessionVersion, snapshot.token) &&
    state.threadCaches.get(snapshot.threadID) === snapshot.cache;
}

function currentConversationMatches(snapshot) {
  return (
    currentSessionMatches(snapshot.sessionVersion, snapshot.token) &&
    snapshot.version === state.conversationVersion &&
    snapshot.threadID === state.threadID &&
    snapshot.cache === state.currentCache &&
    Boolean(snapshot.threadID)
  );
}

function peerByID(id) {
  return state.users.find((user) => user.id === id);
}

function renderPeerList() {
  peerList.replaceChildren();
  const peers = state.users.filter((user) => user.id !== state.currentUserID);

  if (peers.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Chưa có tài khoản khác.";
    peerList.append(empty);
    return;
  }

  for (const user of peers) {
    const thread = state.threadsByPeer.get(user.id);
    const unreadCount = Number(thread?.unread_count || 0);
    const button = document.createElement("button");
    button.type = "button";
    button.className = `peer${user.id === state.peerID ? " active" : ""}`;

    const name = document.createElement("span");
    name.textContent = user.username;
    button.append(name);

    if (unreadCount > 0) {
      const badge = document.createElement("span");
      badge.className = "unread-badge";
      badge.textContent = unreadCount > 99 ? "99+" : String(unreadCount);
      badge.setAttribute("aria-label", `${unreadCount} tin chưa đọc`);
      button.append(badge);
    }

    button.addEventListener("click", () => openConversation(user.id));
    peerList.append(button);
  }
}

async function loadUsers() {
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  const users = await apiRequest("/users");
  if (!currentSessionMatches(sessionVersion, token)) {
    return;
  }

  const me = users.find((user) => user.id === state.currentUserID);
  if (!me) {
    clearSession();
    throw new Error("Tài khoản của phiên đăng nhập không còn tồn tại.");
  }

  state.users = users;
  state.currentUsername = me.username;
  sessionStorage.setItem(sessionUsernameKey, me.username);
  currentUsername.textContent = me.username;

  if (state.peerID && !peerByID(state.peerID)) {
    resetConversation();
  }
  renderPeerList();
}

function applyCurrentThreadSummary(thread) {
  if (!thread) {
    return;
  }
  const cache = state.threadCaches.get(thread.id);
  if (!cache) return;
  const previousPeerMarker = cache.peerLastReadSeq;
  cache.lastReadSeq = Math.max(cache.lastReadSeq, Number(thread.last_read_seq || 0));
  cache.peerLastReadSeq = Math.max(cache.peerLastReadSeq, Number(thread.peer_last_read_seq || 0));
  if (state.currentCache === cache && cache.messagesLoaded && previousPeerMarker !== cache.peerLastReadSeq) {
    renderMessages("preserve", false);
  }
}

function updateThreadFromMessage(message) {
  const seq = Number(message.seq);
  if (!Number.isSafeInteger(seq) || seq <= 0) {
    return;
  }
  const thread = [...state.threadsByPeer.values()].find((item) => item.id === message.thread_id);
  const baseline = state.threadSummaryBaseSeqs.get(message.thread_id) ?? Number(thread?.last_seq || 0);
  if (seq <= baseline) {
    return;
  }
  let events = state.threadEventSeqs.get(message.thread_id);
  if (!events) {
    events = new Map();
    state.threadEventSeqs.set(message.thread_id, events);
  }
  if (events.has(seq)) {
    return;
  }
  events.set(seq, message);
  if (!thread) {
    // A thread opened in another tab may not be in our initial list yet.
    if (state.threadListRequest) {
      state.threadListDirty = true;
    }
    void loadThreads();
    return;
  }
  thread.last_seq = Math.max(Number(thread.last_seq || 0), seq);
  if (message.sender_id !== state.currentUserID && message.kind !== "system" &&
      seq > Number(thread.last_read_seq || 0)) {
    thread.unread_count = Number(thread.unread_count || 0) + 1;
  }
  renderPeerList();
}

function acceptMessage(message) {
  const cache = cacheForThread(message.thread_id);
  const changed = MiniHermesRealtime.merge(cache.messages, cache.messageIDs, [message]);
  if (cache.messagesLoaded) {
    if (Number(message.seq) > cache.syncedSeq + 1) {
      void catchUpConversation(cache, Number(message.seq));
    } else {
      while (cache.messages.has(cache.syncedSeq + 1)) {
        cache.syncedSeq += 1;
      }
    }
  }
  return { cache, changed };
}

function reconcileThreadSummary(thread) {
  const old = state.threadsByPeer.get(thread.peer.id);
  const baseSeq = Number(thread.last_seq || 0);
  const events = state.threadEventSeqs.get(thread.id);
  const oldReadSeq = Number(old?.last_read_seq || 0);
  const freshReadSeq = Number(thread.last_read_seq || 0);
  const cache = state.threadCaches.get(thread.id);
  if (oldReadSeq > freshReadSeq && cache) {
    let newlyRead = 0;
    for (let seq = freshReadSeq + 1; seq <= oldReadSeq; seq += 1) {
      const message = cache.messages.get(seq);
      if (message && message.sender_id !== state.currentUserID && message.kind !== "system") {
        newlyRead += 1;
      }
    }
    thread.last_read_seq = oldReadSeq;
    thread.unread_count = Math.max(0, Number(thread.unread_count || 0) - newlyRead);
  }
  state.threadSummaryBaseSeqs.set(thread.id, baseSeq);
  if (events) {
    for (const [seq, message] of events) {
      if (seq <= baseSeq) {
        events.delete(seq);
      } else {
        thread.last_seq = Math.max(Number(thread.last_seq || 0), seq);
        if (message.sender_id !== state.currentUserID && message.kind !== "system" &&
            seq > Number(thread.last_read_seq || 0)) {
          thread.unread_count = Number(thread.unread_count || 0) + 1;
        }
      }
    }
  }
  return thread;
}

function applyLocalReadMarker(threadID, previous, current) {
  const thread = [...state.threadsByPeer.values()].find((item) => item.id === threadID);
  const cache = state.threadCaches.get(threadID);
  if (!thread || !cache || current <= previous) {
    return;
  }
  thread.last_read_seq = Math.max(Number(thread.last_read_seq || 0), current);
  let newlyRead = 0;
  for (let seq = previous + 1; seq <= current; seq += 1) {
    const message = cache.messages.get(seq);
    if (message && message.sender_id !== state.currentUserID && message.kind !== "system") {
      newlyRead += 1;
    }
  }
  thread.unread_count = Math.max(0, Number(thread.unread_count || 0) - newlyRead);
  renderPeerList();
}

function scheduleThreadSummaryRefresh() {
  if (state.threadSummaryTimer !== null) {
    clearTimeout(state.threadSummaryTimer);
  }
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  state.threadSummaryTimer = setTimeout(() => {
    state.threadSummaryTimer = null;
    if (!currentSessionMatches(sessionVersion, token)) {
      return;
    }
    if (state.threadListRequest) {
      state.threadListDirty = true;
    } else {
      void loadThreads();
    }
  }, threadSummaryDebounceMs);
}

async function loadThreads(catchUpCached = false) {
  if (!state.token) {
    return;
  }
  if (state.threadSummaryTimer !== null) {
    clearTimeout(state.threadSummaryTimer);
    state.threadSummaryTimer = null;
  }
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  if (
    state.threadListRequest &&
    state.threadListRequest.sessionVersion === sessionVersion &&
    state.threadListRequest.token === token
  ) {
    if (catchUpCached) {
      state.threadListDirty = true;
      state.threadListCatchup = true;
    }
    return;
  }

  const request = { sessionVersion, token };
  state.threadListRequest = request;
  try {
    const threads = await apiRequest("/threads");
    if (!currentSessionMatches(sessionVersion, token)) {
      return;
    }

    const refreshed = new Map();
    for (const thread of threads) {
      // This demo currently renders direct chats; group REST support is separate.
      if (thread.kind !== "direct" || !thread.peer) continue;
      const serverLastSeq = Number(thread.last_seq || 0);
      refreshed.set(thread.peer.id, reconcileThreadSummary(thread));
      const cache = state.threadCaches.get(thread.id);
      if (catchUpCached && cache?.messagesLoaded && serverLastSeq > cache.syncedSeq) {
        void catchUpConversation(cache, serverLastSeq);
      }
    }
    // A direct-thread POST may have completed after this list request began.
    for (const [peerID, thread] of state.threadsByPeer) {
      if (!refreshed.has(peerID)) {
        refreshed.set(peerID, thread);
        const cache = state.threadCaches.get(thread.id);
        if (catchUpCached && cache?.messagesLoaded) {
          void catchUpConversation(cache);
        }
      }
    }
    state.threadsByPeer = refreshed;
    for (const thread of refreshed.values()) applyCurrentThreadSummary(thread);
    renderPeerList();
  } catch (error) {
    if (currentSessionMatches(sessionVersion, token)) {
      showError(error.message);
    }
  } finally {
    if (state.threadListRequest === request) {
      state.threadListRequest = null;
      if (state.threadListDirty && currentSessionMatches(sessionVersion, token)) {
        state.threadListDirty = false;
        const retryCatchup = state.threadListCatchup;
        state.threadListCatchup = false;
        void loadThreads(retryCatchup);
      }
    }
  }
}

function displayName(userID) {
  if (userID === state.currentUserID) {
    return state.currentUsername;
  }
  return peerByID(userID)?.username || "unknown";
}

function mergeMessages(cache, messages) {
  return MiniHermesRealtime.merge(cache.messages, cache.messageIDs, messages);
}

function sortedMessages(cache) {
  return [...cache.messages.values()].sort((left, right) => left.seq - right.seq);
}

function highestMessageSeq(cache) {
  let highest = 0;
  for (const seq of cache.messages.keys()) {
    highest = Math.max(highest, seq);
  }
  return highest;
}

function renderMessages(scrollMode = "preserve", recordVisibility = true) {
  const cache = state.currentCache;
  if (!cache) return;
  const previousHeight = historyElement.scrollHeight;
  const previousTop = historyElement.scrollTop;
  const wasNearBottom = previousHeight - previousTop - historyElement.clientHeight < 48;
  const messages = sortedMessages(cache);

  historyElement.replaceChildren();
  if (messages.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Chưa có tin nhắn nào.";
    historyElement.append(empty);
  } else {
    for (const message of messages) {
      const sent = message.sender_id === state.currentUserID;
      const item = document.createElement("article");
      item.className = `message ${sent ? "sent" : "received"}`;
      item.dataset.seq = String(message.seq);
      item.dataset.kind = message.kind;

      const content = document.createElement("p");
      content.textContent = message.content;

      const meta = document.createElement("small");
      const time = new Date(message.created_at).toLocaleString("vi-VN", {
        hour: "2-digit",
        minute: "2-digit",
        day: "2-digit",
        month: "2-digit",
      });
      const readStatus = sent && message.seq <= cache.peerLastReadSeq ? " · Đã đọc" : "";
      meta.textContent = `${displayName(message.sender_id)} · ${time}${readStatus}`;
      item.append(content, meta);
      historyElement.append(item);
    }
  }

  if (scrollMode === "initial" || scrollMode === "bottom" || (scrollMode === "new" && wasNearBottom)) {
    historyElement.scrollTop = historyElement.scrollHeight;
  } else if (scrollMode === "older") {
    historyElement.scrollTop = previousTop + historyElement.scrollHeight - previousHeight;
  } else {
    historyElement.scrollTop = previousTop;
  }

  loadOlderButton.hidden = cache.nextCursor === null;
  loadOlderButton.disabled = cache.nextCursor === null || Boolean(cache.olderRequest);
  if (recordVisibility) {
    const snapshot = conversationSnapshot();
    requestAnimationFrame(() => recordVisibleMessages(snapshot));
  }
}

function pageURL(threadID, beforeSeq = null) {
  const params = new URLSearchParams({ limit: String(messagePageLimit) });
  if (beforeSeq !== null) {
    params.set("before_seq", String(beforeSeq));
  }
  return `/threads/${threadID}/messages?${params}`;
}

async function loadInitialHistory(cache = state.currentCache) {
  if (!cache || !state.token || cache.messagesLoaded) {
    return;
  }
  const snapshot = cacheSnapshot(cache);
  if (cache.initialHistoryRequest) {
    cache.syncAfterInitial = true;
    return;
  }
  const request = { ...snapshot };
  cache.initialHistoryRequest = request;
  try {
    const page = await apiRequest(pageURL(snapshot.threadID));
    if (!cacheSnapshotMatches(snapshot)) {
      return;
    }
    const messages = Array.isArray(page.messages) ? page.messages : [];
    mergeMessages(cache, messages);
    cache.nextCursor = page.next_cursor ?? null;
    cache.syncedSeq = Math.max(0, ...messages.map((message) => Number(message.seq) || 0));
    cache.messagesLoaded = true;
    if (state.currentCache === cache) {
      renderMessages("initial");
      showError("");
    }
    // An event could have arrived while the first REST page was in flight.
    if (cache.syncAfterInitial || highestMessageSeq(cache) > cache.syncedSeq) {
      cache.syncAfterInitial = false;
      await catchUpConversation(cache);
    }
  } catch (error) {
    if (cacheSnapshotMatches(snapshot) && state.currentCache === cache) {
      showError(error.message);
    }
  } finally {
    if (cache.initialHistoryRequest === request) {
      cache.initialHistoryRequest = null;
    }
  }
}

async function catchUpConversation(cache = state.currentCache, targetSeq = 0) {
  if (!cache || state.threadCaches.get(cache.threadID) !== cache || !cache.messagesLoaded || !state.token) {
    return;
  }
  if (cache.catchupRequest) {
    cache.catchupTargetSeq = Math.max(cache.catchupTargetSeq, targetSeq);
    return;
  }
  const snapshot = cacheSnapshot(cache);
  const request = { ...snapshot };
  cache.catchupRequest = request;
  const baseline = cache.syncedSeq;
  const stale = new Error("session changed");
  let completed = false;
  try {
    const maxSeq = await MiniHermesRealtime.fetchThroughBoundary(
      async (beforeSeq) => {
        const page = await apiRequest(pageURL(snapshot.threadID, beforeSeq));
        if (!cacheSnapshotMatches(snapshot)) {
          throw stale;
        }
        return page;
      },
      baseline,
      (messages) => mergeMessages(cache, messages),
    );
    if (!cacheSnapshotMatches(snapshot)) {
      return;
    }
    cache.syncedSeq = Math.max(cache.syncedSeq, maxSeq);
    completed = true;
    if (state.currentCache === cache) {
      renderMessages("new");
      showError("");
    }
  } catch (error) {
    if (error !== stale && cacheSnapshotMatches(snapshot) && state.currentCache === cache) {
      showError(error.message);
    }
  } finally {
    if (cache.catchupRequest === request) {
      cache.catchupRequest = null;
      const nextTarget = cache.catchupTargetSeq;
      cache.catchupTargetSeq = 0;
      if (completed && nextTarget > cache.syncedSeq && cacheSnapshotMatches(snapshot)) {
        void catchUpConversation(cache);
      }
    }
  }
}

async function syncCurrentConversation() {
  const cache = state.currentCache;
  if (!cache) {
    return;
  }
  if (!cache.messagesLoaded) {
    await loadInitialHistory(cache);
  } else {
    await catchUpConversation(cache);
  }
}

function stopWebSocket(resetBackoff = true) {
  state.socketGeneration += 1;
  state.socketTicketAbort?.abort();
  state.socketTicketAbort = null;
  clearSocketHealthTimer();
  if (state.reconnectTimer !== null) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  const socket = state.socket;
  state.socket = null;
  state.socketConnecting = false;
  state.socketLastActivityAt = 0;
  if (resetBackoff) {
    state.reconnectDelay = 1000;
    state.socketSummaryStale = false;
  }
  if (socket) {
    socket.close(1000, "session ended");
  }
}

function clearSocketHealthTimer() {
  if (state.socketHealthTimer !== null) {
    clearTimeout(state.socketHealthTimer);
    state.socketHealthTimer = null;
  }
}

function armSocketTimeout(generation, sessionVersion, token, delay) {
  clearSocketHealthTimer();
  const timer = setTimeout(() => {
    if (state.socketHealthTimer !== timer) {
      return;
    }
    state.socketHealthTimer = null;
    if (generation === state.socketGeneration && currentSessionMatches(sessionVersion, token) &&
        !(document.visibilityState === "hidden" && state.socket?.readyState === WebSocket.OPEN)) {
      restartWebSocket();
    }
  }, delay);
  state.socketHealthTimer = timer;
}

function restartWebSocket(immediate = false) {
  if (!state.token) {
    return;
  }
  stopWebSocket(false);
  state.socketSummaryStale = true;
  if (immediate) {
    void connectWebSocket();
  } else {
    scheduleReconnect(state.socketGeneration, state.sessionVersion, state.token);
  }
}

function ensureWebSocket() {
  if (!state.token || (state.socketConnecting && !state.socket)) {
    return;
  }
  if (!state.socket) {
    if (state.reconnectTimer !== null) {
      return;
    }
    void connectWebSocket();
    return;
  }
  if (state.socket.readyState === 0 && state.socketConnecting) {
    return;
  }
  if (state.socket.readyState === WebSocket.OPEN) {
    // Background tabs can delay both timers and onmessage callbacks. Give a queued
    // heartbeat a chance to arrive before declaring an apparently open socket dead.
    const stale = Date.now() - state.socketLastActivityAt >= websocketHeartbeatTimeoutMs;
    armSocketTimeout(state.socketGeneration, state.sessionVersion, state.token,
      stale ? websocketResumeGraceMs : websocketHeartbeatTimeoutMs);
  } else {
    restartWebSocket();
  }
}

function scheduleReconnect(generation, sessionVersion, token) {
  if (generation !== state.socketGeneration || !currentSessionMatches(sessionVersion, token) ||
      state.reconnectTimer !== null) {
    return;
  }
  const delay = state.reconnectDelay;
  state.reconnectDelay = Math.min(delay * 2, 30000);
  const timer = setTimeout(() => {
    if (state.reconnectTimer !== timer) {
      return;
    }
    state.reconnectTimer = null;
    if (generation === state.socketGeneration && currentSessionMatches(sessionVersion, token)) {
      void connectWebSocket();
    }
  }, delay);
  state.reconnectTimer = timer;
}

function handleSocketMessage(data) {
  let event;
  try {
    event = JSON.parse(data);
  } catch {
    return;
  }
  if (event.thread_kind === "group") return;
  const seq = Number(event.seq);
  if (event.type !== "message.created" || event.recipient_id !== state.currentUserID ||
      typeof event.message_id !== "string" || typeof event.thread_id !== "string" ||
      !Number.isSafeInteger(seq) || seq <= 0) {
    return;
  }
  updateThreadFromMessage(event);
  const { cache, changed } = acceptMessage(event);
  if (state.currentCache === cache && cache.messagesLoaded && changed) {
    renderMessages("new");
  }
}

async function connectWebSocket() {
  if (!state.token || state.socket || state.socketConnecting) {
    return;
  }
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  const generation = state.socketGeneration;
  const ticketAbort = new AbortController();
  state.socketTicketAbort = ticketAbort;
  state.socketConnecting = true;
  armSocketTimeout(generation, sessionVersion, token, websocketConnectTimeoutMs);
  try {
    const response = await apiRequest("/auth/ws-ticket", { method: "POST", signal: ticketAbort.signal });
    if (state.socketTicketAbort === ticketAbort) {
      state.socketTicketAbort = null;
    }
    if (generation !== state.socketGeneration || !currentSessionMatches(sessionVersion, token)) {
      return;
    }
    const url = new URL(response.ws_url);
    if (url.protocol !== "ws:" && url.protocol !== "wss:") {
      throw new Error("Địa chỉ WebSocket không hợp lệ.");
    }
    url.searchParams.set("ticket", response.ticket);
    const socket = new WebSocket(url.toString());
    state.socket = socket;
    socket.onopen = () => {
      if (generation !== state.socketGeneration || state.socket !== socket) {
        socket.close();
        return;
      }
      state.socketConnecting = false;
      state.socketLastActivityAt = Date.now();
      if (document.visibilityState === "visible") {
        armSocketTimeout(generation, sessionVersion, token, websocketHeartbeatTimeoutMs);
      } else {
        clearSocketHealthTimer();
      }
      showError("");
      if (state.socketSummaryStale) {
        state.socketSummaryStale = false;
        void loadThreads(true);
        if (state.currentCache && !state.currentCache.messagesLoaded) {
          void loadInitialHistory(state.currentCache);
        }
      } else {
        // The first socket may open after the initial REST page was fetched.
        void syncCurrentConversation();
      }
      void flushReadMarker();
    };
    socket.onmessage = (message) => {
      if (generation === state.socketGeneration && state.socket === socket) {
        state.socketLastActivityAt = Date.now();
        if (document.visibilityState === "visible") {
          armSocketTimeout(generation, sessionVersion, token, websocketHeartbeatTimeoutMs);
        } else {
          clearSocketHealthTimer();
        }
        state.reconnectDelay = 1000;
        handleSocketMessage(message.data);
      }
    };
    socket.onerror = () => socket.close();
    socket.onclose = () => {
      if (generation !== state.socketGeneration || state.socket !== socket) {
        return;
      }
      clearSocketHealthTimer();
      state.socket = null;
      state.socketConnecting = false;
      state.socketSummaryStale = true;
      scheduleReconnect(generation, sessionVersion, token);
    };
  } catch (error) {
    if (state.socketTicketAbort === ticketAbort) {
      state.socketTicketAbort = null;
    }
    if (generation === state.socketGeneration && currentSessionMatches(sessionVersion, token)) {
      clearSocketHealthTimer();
      state.socketConnecting = false;
      state.socketSummaryStale = true;
      showError(error.message);
      scheduleReconnect(generation, sessionVersion, token);
    }
  }
}

async function loadOlderMessages() {
  const cache = state.currentCache;
  if (!cache || cache.nextCursor === null || cache.olderRequest) {
    return;
  }
  const snapshot = cacheSnapshot(cache);
  const request = { ...snapshot, beforeSeq: cache.nextCursor };
  cache.olderRequest = request;
  loadOlderButton.disabled = true;

  try {
    const page = await apiRequest(pageURL(snapshot.threadID, request.beforeSeq));
    if (!cacheSnapshotMatches(snapshot)) {
      return;
    }
    mergeMessages(cache, Array.isArray(page.messages) ? page.messages : []);
    cache.nextCursor = page.next_cursor ?? null;
    if (state.currentCache === cache) {
      renderMessages("older");
      showError("");
    }
  } catch (error) {
    if (cacheSnapshotMatches(snapshot) && state.currentCache === cache) {
      showError(error.message);
    }
  } finally {
    if (cache.olderRequest === request) {
      cache.olderRequest = null;
      if (state.currentCache === cache) {
        loadOlderButton.hidden = cache.nextCursor === null;
        loadOlderButton.disabled = cache.nextCursor === null;
      }
    }
  }
}

function isElementVisibleInHistory(element) {
  const historyRect = historyElement.getBoundingClientRect();
  const messageRect = element.getBoundingClientRect();
  return messageRect.bottom > historyRect.top && messageRect.top < historyRect.bottom;
}

function recordVisibleMessages(snapshot = conversationSnapshot()) {
  const cache = snapshot.cache;
  if (
    !currentConversationMatches(snapshot) ||
    document.visibilityState !== "visible" ||
    chatView.hidden ||
    !cache.messagesLoaded
  ) {
    return;
  }

  for (const element of historyElement.querySelectorAll(".message.received[data-seq]")) {
    if (element.dataset.kind === "system" || !isElementVisibleInHistory(element)) {
      continue;
    }
    const seq = Number(element.dataset.seq);
    if (Number.isSafeInteger(seq) && seq > cache.lastReadSeq) {
      cache.seenReceivedSeqs.add(seq);
    }
  }

  let candidate = cache.lastReadSeq;
  let sawReceived = false;
  while (true) {
    const nextSeq = candidate + 1;
    const message = cache.messages.get(nextSeq);
    if (!message) {
      break;
    }
    if (message.sender_id !== state.currentUserID && message.kind !== "system") {
      if (!cache.seenReceivedSeqs.has(nextSeq)) {
        break;
      }
      sawReceived = true;
    }
    candidate = nextSeq;
  }

  if (sawReceived && candidate > cache.lastReadSeq) {
    queueReadMarker(candidate, snapshot);
  }
}

function queueReadMarker(lastReadSeq, snapshot) {
  if (!currentConversationMatches(snapshot) || document.visibilityState !== "visible") {
    return;
  }
  snapshot.cache.pendingReadSeq = Math.max(snapshot.cache.pendingReadSeq, lastReadSeq);
  flushReadMarker(snapshot);
}

async function flushReadMarker(snapshot = conversationSnapshot()) {
  const cache = snapshot.cache;
  if (
    !cache || cache.readRequest ||
    cache.pendingReadSeq <= cache.lastReadSeq ||
    !currentConversationMatches(snapshot) ||
    document.visibilityState !== "visible"
  ) {
    return;
  }

  const target = cache.pendingReadSeq;
  cache.pendingReadSeq = 0;
  const request = { ...snapshot, target };
  let completed = false;
  cache.readRequest = request;
  try {
    const response = await apiRequest(`/threads/${snapshot.threadID}/read`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ last_read_seq: target }),
    });
    if (!cacheSnapshotMatches(snapshot)) {
      return;
    }

    const previousReadSeq = cache.lastReadSeq;
    cache.lastReadSeq = Math.max(cache.lastReadSeq, Number(response.last_read_seq || 0));
    applyLocalReadMarker(snapshot.threadID, previousReadSeq, cache.lastReadSeq);
    if (cache.lastReadSeq > target) {
      // Another tab advanced the marker; local messages may not cover that range.
      void loadThreads();
    }
    completed = true;
    for (const seq of cache.seenReceivedSeqs) {
      if (seq <= cache.lastReadSeq) {
        cache.seenReceivedSeqs.delete(seq);
      }
    }
  } catch (error) {
    if (cacheSnapshotMatches(snapshot)) {
      // PUT may have committed despite a lost response; retrying a monotonic marker is safe.
      cache.pendingReadSeq = Math.max(cache.pendingReadSeq, target);
    }
    if (currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (cache.readRequest === request) {
      cache.readRequest = null;
    }
    if (completed && currentConversationMatches(snapshot)) {
      recordVisibleMessages(snapshot);
    } else if (completed && state.currentCache === cache) {
      recordVisibleMessages();
    }
  }
}

async function openConversation(peerID) {
  if (state.currentCache) {
    state.currentCache.draftContent = contentInput.value;
  }
  const version = ++state.conversationVersion;
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  state.peerID = peerID;
  state.threadID = "";
  state.currentCache = null;
  renderPeerList();

  const peer = peerByID(peerID);
  peerName.textContent = peer?.username || "Đang mở...";
  historyElement.innerHTML = '<p class="empty">Đang tải lịch sử...</p>';
  contentInput.disabled = true;
  sendButton.disabled = true;
  loadOlderButton.hidden = true;
  showError("");

  try {
    let thread = state.threadsByPeer.get(peerID);
    if (!thread) {
      let request = state.directRequestsByPeer.get(peerID);
      if (!request) {
        request = apiRequest("/threads/direct", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ peer_id: peerID }),
        });
        state.directRequestsByPeer.set(peerID, request);
        void request.finally(() => {
          if (state.directRequestsByPeer.get(peerID) === request) {
            state.directRequestsByPeer.delete(peerID);
          }
        }).catch(() => {});
      }
      thread = await request;
      if (!currentSessionMatches(sessionVersion, token)) return;
      thread = reconcileThreadSummary(thread);
      state.threadsByPeer.set(peerID, thread);
      renderPeerList();
    }
    if (
      version !== state.conversationVersion ||
      peerID !== state.peerID ||
      !currentSessionMatches(sessionVersion, token)
    ) {
      return;
    }

    state.threadID = thread.id;
    const cache = cacheForThread(thread.id);
    state.currentCache = cache;
    contentInput.value = cache.draftContent;
    applyCurrentThreadSummary(thread);
    renderPeerList();
    contentInput.disabled = false;
    updateSendButton();
    if (cache.messagesLoaded) {
      renderMessages("initial");
    } else {
      await loadInitialHistory(cache);
    }
    if (state.currentCache === cache) {
      void flushReadMarker();
    }
    if (version === state.conversationVersion && thread.id === state.threadID &&
        currentSessionMatches(sessionVersion, token)) {
      contentInput.focus();
    }
  } catch (error) {
    if (version === state.conversationVersion && currentSessionMatches(sessionVersion, token)) {
      showError(error.message);
    }
  }
}

registerForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  showError("");
  showNotice("");
  const username = registerForm.elements.username.value;
  const password = registerForm.elements.password.value;

  try {
    await apiRequest("/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    }, false);
    loginForm.elements.username.value = username;
    loginForm.elements.password.focus();
    registerForm.reset();
    showNotice("Đăng ký thành công. Hãy đăng nhập bằng tài khoản vừa tạo.");
  } catch (error) {
    showError(error.message);
  }
});

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  showError("");
  showNotice("");
  const username = loginForm.elements.username.value;
  const password = loginForm.elements.password.value;

  try {
    const response = await apiRequest("/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    }, false);
    const userID = decodeSubject(response.access_token);
    if (!userID) {
      throw new Error("Token đăng nhập không hợp lệ.");
    }

    stopWebSocket();
    state.sessionVersion += 1;
    state.token = response.access_token;
    state.currentUserID = userID;
    state.currentUsername = username.trim().toLowerCase();
    state.users = [];
    state.threadsByPeer = new Map();
    state.threadCaches = new Map();
    state.directRequestsByPeer = new Map();
    state.threadEventSeqs = new Map();
    state.threadSummaryBaseSeqs = new Map();
    state.threadListRequest = null;
    state.threadListDirty = false;
    state.threadListCatchup = false;
    sessionStorage.setItem(sessionTokenKey, state.token);
    sessionStorage.setItem(sessionUsernameKey, state.currentUsername);
    loginForm.reset();
    resetConversation();
    showChatView();
    await loadUsers();
    await loadThreads();
    void connectWebSocket();
  } catch (error) {
    showError(error.message);
  }
});

messageForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!state.threadID) {
    return;
  }
  const snapshot = conversationSnapshot();
  const cache = snapshot.cache;
  if (cache.pendingSend?.inFlight) {
    return;
  }
  const content = contentInput.value;
  let pending = cache.pendingSend;
  if (pending && content !== pending.content) {
    showError(pendingSendMismatchMessage);
    return;
  }
  if (!pending) {
    pending = { messageID: crypto.randomUUID(), content, inFlight: false };
    cache.pendingSend = pending;
  }
  cache.draftContent = content;
  pending.inFlight = true;
  updateSendButton();
  showError("");
  try {
    const message = await sendMessageWithRetry(snapshot, pending.messageID, pending.content);
    if (!cacheSnapshotMatches(snapshot)) {
      return;
    }
    if (cache.pendingSend === pending) {
      cache.pendingSend = null;
    }
    if (cache.draftContent === pending.content) {
      cache.draftContent = "";
    }
    updateThreadFromMessage(message);
    acceptMessage(message);
    if (state.currentCache === cache) {
      if (contentInput.value === pending.content) {
        contentInput.value = "";
      } else {
        cache.draftContent = contentInput.value;
        showNotice("Tin trước đã gửi; nội dung đã sửa vẫn ở ô nhập. Bấm Gửi để gửi thành tin mới.");
      }
      renderMessages(currentConversationMatches(snapshot) ? "bottom" : "new", false);
    }
    scheduleThreadSummaryRefresh();
  } catch (error) {
    if (cacheSnapshotMatches(snapshot) && state.currentCache === cache) {
      showError(contentInput.value === pending.content
        ? `${error.message} Tin chưa được xác nhận; bấm Gửi lại với nội dung cũ.`
        : pendingSendMismatchMessage);
    }
  } finally {
    pending.inFlight = false;
    if (state.currentCache === cache) {
      updateSendButton();
    }
  }
});

contentInput.addEventListener("input", () => {
  const cache = state.currentCache;
  if (!cache) return;
  cache.draftContent = contentInput.value;
  if (cache.pendingSend && contentInput.value !== cache.pendingSend.content) {
    showError(pendingSendMismatchMessage);
  } else if (errorElement.textContent === pendingSendMismatchMessage) {
    showError("");
  }
});

logoutButton.addEventListener("click", () => {
  clearSession();
  showNotice("Đã đăng xuất.");
  showError("");
});

loadOlderButton.addEventListener("click", loadOlderMessages);

let visibilityFrame = null;
historyElement.addEventListener("scroll", () => {
  if (visibilityFrame !== null) {
    cancelAnimationFrame(visibilityFrame);
  }
  const snapshot = conversationSnapshot();
  visibilityFrame = requestAnimationFrame(() => {
    visibilityFrame = null;
    recordVisibleMessages(snapshot);
  });
});

document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible") {
    if (state.socket?.readyState === WebSocket.OPEN) {
      clearSocketHealthTimer();
    }
    return;
  }
  if (!state.token) {
    return;
  }
  const snapshot = conversationSnapshot();
  recordVisibleMessages(snapshot);
  void flushReadMarker(snapshot);
  ensureWebSocket();
});

window.addEventListener("online", () => {
  if (!state.token) {
    return;
  }
  ensureWebSocket();
});

state.token = sessionStorage.getItem(sessionTokenKey) || "";
state.currentUserID = decodeSubject(state.token);
state.currentUsername = sessionStorage.getItem(sessionUsernameKey) || "";
if (state.token && state.currentUserID) {
  state.sessionVersion += 1;
  showChatView();
  Promise.all([loadUsers(), loadThreads()])
    .then(() => connectWebSocket())
    .catch((error) => showError(error.message));
} else {
  clearSession();
}
