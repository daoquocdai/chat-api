const sessionTokenKey = "mini-hermes.access-token";
const sessionUsernameKey = "mini-hermes.username";
const messagePageLimit = 30;

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
const refreshButton = document.querySelector("#refresh-button");
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
  peerID: "",
  threadID: "",
  conversationVersion: 0,
  messages: new Map(),
  messageIDs: new Map(),
  messagesLoaded: false,
  syncedSeq: 0,
  nextCursor: null,
  lastReadSeq: 0,
  peerLastReadSeq: 0,
  seenReceivedSeqs: new Set(),
  initialHistoryRequest: null,
  syncAfterInitial: false,
  catchupRequest: null,
  catchupAgain: false,
  olderRequest: null,
  threadListRequest: null,
  threadListDirty: false,
  readRequest: null,
  pendingReadSeq: 0,
  socket: null,
  socketConnecting: false,
  socketGeneration: 0,
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
      if (attempt === 1 || !retryable || !currentConversationMatches(snapshot)) {
        throw error;
      }
    }
  }

  throw new Error("Không thể gửi tin nhắn.");
}

function resetConversation() {
  state.peerID = "";
  state.threadID = "";
  state.conversationVersion += 1;
  state.messages = new Map();
  state.messageIDs = new Map();
  state.messagesLoaded = false;
  state.syncedSeq = 0;
  state.nextCursor = null;
  state.lastReadSeq = 0;
  state.peerLastReadSeq = 0;
  state.seenReceivedSeqs = new Set();
  state.initialHistoryRequest = null;
  state.syncAfterInitial = false;
  state.catchupRequest = null;
  state.catchupAgain = false;
  state.olderRequest = null;
  state.readRequest = null;
  state.pendingReadSeq = 0;
  peerName.textContent = "Chọn một tài khoản";
  historyElement.innerHTML = '<p class="empty">Chọn một tài khoản để bắt đầu chat.</p>';
  contentInput.value = "";
  contentInput.disabled = true;
  sendButton.disabled = true;
  refreshButton.disabled = true;
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
  state.threadListRequest = null;
  state.threadListDirty = false;
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
  };
}

function currentConversationMatches(snapshot) {
  return (
    currentSessionMatches(snapshot.sessionVersion, snapshot.token) &&
    snapshot.version === state.conversationVersion &&
    snapshot.threadID === state.threadID &&
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
  if (!thread || thread.id !== state.threadID) {
    return;
  }

  const previousPeerMarker = state.peerLastReadSeq;
  state.lastReadSeq = Math.max(state.lastReadSeq, Number(thread.last_read_seq || 0));
  state.peerLastReadSeq = Math.max(state.peerLastReadSeq, Number(thread.peer_last_read_seq || 0));
  if (state.messagesLoaded && previousPeerMarker !== state.peerLastReadSeq) {
    renderMessages("preserve", false);
  }
}

async function loadThreads() {
  if (!state.token) {
    return;
  }
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  if (
    state.threadListRequest &&
    state.threadListRequest.sessionVersion === sessionVersion &&
    state.threadListRequest.token === token
  ) {
    state.threadListDirty = true;
    return;
  }

  const request = { sessionVersion, token };
  state.threadListRequest = request;
  try {
    const threads = await apiRequest("/threads");
    if (!currentSessionMatches(sessionVersion, token)) {
      return;
    }

    state.threadsByPeer = new Map(threads.map((thread) => [thread.peer.id, thread]));
    applyCurrentThreadSummary(threads.find((thread) => thread.id === state.threadID));
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
        void loadThreads();
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

function mergeMessages(messages) {
  return MiniHermesRealtime.merge(state.messages, state.messageIDs, messages);
}

function sortedMessages() {
  return [...state.messages.values()].sort((left, right) => left.seq - right.seq);
}

function highestMessageSeq() {
  let highest = 0;
  for (const seq of state.messages.keys()) {
    highest = Math.max(highest, seq);
  }
  return highest;
}

function renderMessages(scrollMode = "preserve", recordVisibility = true) {
  const previousHeight = historyElement.scrollHeight;
  const previousTop = historyElement.scrollTop;
  const wasNearBottom = previousHeight - previousTop - historyElement.clientHeight < 48;
  const messages = sortedMessages();

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
      const readStatus = sent && message.seq <= state.peerLastReadSeq ? " · Đã đọc" : "";
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

  loadOlderButton.hidden = state.nextCursor === null;
  loadOlderButton.disabled = state.nextCursor === null || Boolean(state.olderRequest);
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

async function loadInitialHistory() {
  if (!state.threadID || !state.token) {
    return;
  }
  const snapshot = conversationSnapshot();
  if (state.initialHistoryRequest) {
    state.syncAfterInitial = true;
    return;
  }
  const request = { ...snapshot };
  state.initialHistoryRequest = request;
  try {
    const page = await apiRequest(pageURL(snapshot.threadID));
    if (!currentConversationMatches(snapshot)) {
      return;
    }
    const messages = Array.isArray(page.messages) ? page.messages : [];
    mergeMessages(messages);
    state.nextCursor = page.next_cursor ?? null;
    state.syncedSeq = Math.max(0, ...messages.map((message) => Number(message.seq) || 0));
    state.messagesLoaded = true;
    renderMessages("initial");
    showError("");
    // An event could have arrived while the first REST page was in flight.
    if (state.syncAfterInitial || highestMessageSeq() > state.syncedSeq) {
      state.syncAfterInitial = false;
      await catchUpConversation(snapshot);
    }
  } catch (error) {
    if (currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (state.initialHistoryRequest === request) {
      state.initialHistoryRequest = null;
    }
  }
}

async function catchUpConversation(snapshot = conversationSnapshot()) {
  if (!currentConversationMatches(snapshot) || !state.messagesLoaded) {
    return;
  }
  if (state.catchupRequest) {
    state.catchupAgain = true;
    return;
  }
  const request = { ...snapshot };
  state.catchupRequest = request;
  const baseline = state.syncedSeq;
  const stale = new Error("conversation changed");
  try {
    const maxSeq = await MiniHermesRealtime.fetchThroughBoundary(
      async (beforeSeq) => {
        const page = await apiRequest(pageURL(snapshot.threadID, beforeSeq));
        if (!currentConversationMatches(snapshot)) {
          throw stale;
        }
        return page;
      },
      baseline,
      (messages) => mergeMessages(messages),
    );
    if (!currentConversationMatches(snapshot)) {
      return;
    }
    state.syncedSeq = Math.max(state.syncedSeq, maxSeq);
    renderMessages("new");
    showError("");
  } catch (error) {
    if (error !== stale && currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (state.catchupRequest === request) {
      state.catchupRequest = null;
      const again = state.catchupAgain;
      state.catchupAgain = false;
      if (again && currentConversationMatches(snapshot)) {
        void catchUpConversation(snapshot);
      }
    }
  }
}

async function syncCurrentConversation() {
  if (!state.threadID) {
    return;
  }
  if (!state.messagesLoaded) {
    await loadInitialHistory();
  } else {
    await catchUpConversation();
  }
}

function stopWebSocket() {
  state.socketGeneration += 1;
  if (state.reconnectTimer !== null) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  const socket = state.socket;
  state.socket = null;
  state.socketConnecting = false;
  state.reconnectDelay = 1000;
  if (socket) {
    socket.close(1000, "session ended");
  }
}

function scheduleReconnect(generation, sessionVersion, token) {
  if (generation !== state.socketGeneration || !currentSessionMatches(sessionVersion, token) ||
      state.reconnectTimer !== null) {
    return;
  }
  const delay = state.reconnectDelay;
  state.reconnectDelay = Math.min(delay * 2, 30000);
  state.reconnectTimer = setTimeout(() => {
    state.reconnectTimer = null;
    void connectWebSocket();
  }, delay);
}

function handleSocketMessage(data) {
  let event;
  try {
    event = JSON.parse(data);
  } catch {
    return;
  }
  const seq = Number(event.seq);
  if (event.type !== "message.created" || event.recipient_id !== state.currentUserID ||
      typeof event.message_id !== "string" || typeof event.thread_id !== "string" ||
      !Number.isSafeInteger(seq) || seq <= 0) {
    return;
  }
  if (event.thread_id === state.threadID) {
    const changed = mergeMessages([event]);
    if (state.messagesLoaded) {
      if (seq > state.syncedSeq + 1) {
        void catchUpConversation();
      } else {
        while (state.messages.has(state.syncedSeq + 1)) {
          state.syncedSeq += 1;
        }
      }
      if (changed) {
        renderMessages("new");
      }
    }
  }
  // Summary/unread count is updated by an event-triggered request, never a timer.
  void loadThreads();
}

async function connectWebSocket() {
  if (!state.token || state.socket || state.socketConnecting) {
    return;
  }
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  const generation = state.socketGeneration;
  state.socketConnecting = true;
  try {
    const response = await apiRequest("/auth/ws-ticket", { method: "POST" });
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
      state.reconnectDelay = 1000;
      showError("");
      void loadThreads();
      void syncCurrentConversation();
    };
    socket.onmessage = (message) => {
      if (generation === state.socketGeneration && state.socket === socket) {
        handleSocketMessage(message.data);
      }
    };
    socket.onerror = () => socket.close();
    socket.onclose = () => {
      if (generation !== state.socketGeneration || state.socket !== socket) {
        return;
      }
      state.socket = null;
      state.socketConnecting = false;
      scheduleReconnect(generation, sessionVersion, token);
    };
  } catch (error) {
    if (generation === state.socketGeneration && currentSessionMatches(sessionVersion, token)) {
      state.socketConnecting = false;
      showError(error.message);
      scheduleReconnect(generation, sessionVersion, token);
    }
  }
}

async function loadOlderMessages() {
  if (state.nextCursor === null || !state.threadID || state.olderRequest) {
    return;
  }
  const snapshot = conversationSnapshot();
  const request = { ...snapshot, beforeSeq: state.nextCursor };
  state.olderRequest = request;
  loadOlderButton.disabled = true;

  try {
    const page = await apiRequest(pageURL(snapshot.threadID, request.beforeSeq));
    if (!currentConversationMatches(snapshot)) {
      return;
    }
    mergeMessages(Array.isArray(page.messages) ? page.messages : []);
    state.nextCursor = page.next_cursor ?? null;
    renderMessages("older");
    showError("");
  } catch (error) {
    if (currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (state.olderRequest === request) {
      state.olderRequest = null;
      loadOlderButton.hidden = state.nextCursor === null;
      loadOlderButton.disabled = state.nextCursor === null;
    }
  }
}

function isElementVisibleInHistory(element) {
  const historyRect = historyElement.getBoundingClientRect();
  const messageRect = element.getBoundingClientRect();
  return messageRect.bottom > historyRect.top && messageRect.top < historyRect.bottom;
}

function recordVisibleMessages(snapshot = conversationSnapshot()) {
  if (
    !currentConversationMatches(snapshot) ||
    document.visibilityState !== "visible" ||
    chatView.hidden ||
    !state.messagesLoaded
  ) {
    return;
  }

  for (const element of historyElement.querySelectorAll(".message.received[data-seq]")) {
    if (element.dataset.kind === "system" || !isElementVisibleInHistory(element)) {
      continue;
    }
    const seq = Number(element.dataset.seq);
    if (Number.isSafeInteger(seq) && seq > state.lastReadSeq) {
      state.seenReceivedSeqs.add(seq);
    }
  }

  let candidate = state.lastReadSeq;
  let sawReceived = false;
  while (true) {
    const nextSeq = candidate + 1;
    const message = state.messages.get(nextSeq);
    if (!message) {
      break;
    }
    if (message.sender_id !== state.currentUserID && message.kind !== "system") {
      if (!state.seenReceivedSeqs.has(nextSeq)) {
        break;
      }
      sawReceived = true;
    }
    candidate = nextSeq;
  }

  if (sawReceived && candidate > state.lastReadSeq) {
    queueReadMarker(candidate, snapshot);
  }
}

function queueReadMarker(lastReadSeq, snapshot) {
  if (!currentConversationMatches(snapshot) || document.visibilityState !== "visible") {
    return;
  }
  state.pendingReadSeq = Math.max(state.pendingReadSeq, lastReadSeq);
  flushReadMarker(snapshot);
}

async function flushReadMarker(snapshot = conversationSnapshot()) {
  if (
    state.readRequest ||
    state.pendingReadSeq <= state.lastReadSeq ||
    !currentConversationMatches(snapshot) ||
    document.visibilityState !== "visible"
  ) {
    return;
  }

  const target = state.pendingReadSeq;
  state.pendingReadSeq = 0;
  const request = { ...snapshot, target };
  let completed = false;
  state.readRequest = request;
  try {
    const response = await apiRequest(`/threads/${snapshot.threadID}/read`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ last_read_seq: target }),
    });
    if (!currentConversationMatches(snapshot)) {
      return;
    }

    state.lastReadSeq = Math.max(state.lastReadSeq, Number(response.last_read_seq || 0));
    completed = true;
    for (const seq of state.seenReceivedSeqs) {
      if (seq <= state.lastReadSeq) {
        state.seenReceivedSeqs.delete(seq);
      }
    }
    await loadThreads();
  } catch (error) {
    if (currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (state.readRequest === request) {
      state.readRequest = null;
    }
    if (completed && currentConversationMatches(snapshot)) {
      recordVisibleMessages(snapshot);
    }
  }
}

async function openConversation(peerID) {
  const version = ++state.conversationVersion;
  const sessionVersion = state.sessionVersion;
  const token = state.token;
  state.peerID = peerID;
  state.threadID = "";
  state.messages = new Map();
  state.messageIDs = new Map();
  state.messagesLoaded = false;
  state.syncedSeq = 0;
  state.nextCursor = null;
  state.lastReadSeq = 0;
  state.peerLastReadSeq = 0;
  state.seenReceivedSeqs = new Set();
  state.initialHistoryRequest = null;
  state.syncAfterInitial = false;
  state.catchupRequest = null;
  state.catchupAgain = false;
  state.olderRequest = null;
  state.readRequest = null;
  state.pendingReadSeq = 0;
  renderPeerList();

  const peer = peerByID(peerID);
  peerName.textContent = peer?.username || "Đang mở...";
  historyElement.innerHTML = '<p class="empty">Đang tải lịch sử...</p>';
  contentInput.disabled = true;
  sendButton.disabled = true;
  refreshButton.disabled = true;
  loadOlderButton.hidden = true;
  showError("");

  try {
    const thread = await apiRequest("/threads/direct", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ peer_id: peerID }),
    });
    if (
      version !== state.conversationVersion ||
      peerID !== state.peerID ||
      !currentSessionMatches(sessionVersion, token)
    ) {
      return;
    }

    state.threadID = thread.id;
    state.lastReadSeq = Number(thread.last_read_seq || 0);
    state.peerLastReadSeq = Number(thread.peer_last_read_seq || 0);
    state.threadsByPeer.set(peerID, thread);
    renderPeerList();
    contentInput.disabled = false;
    sendButton.disabled = false;
    refreshButton.disabled = false;
    await loadInitialHistory();
    if (version === state.conversationVersion && thread.id === state.threadID) {
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
  const messageID = crypto.randomUUID();
  const content = contentInput.value;
  sendButton.disabled = true;
  showError("");
  try {
    const message = await sendMessageWithRetry(snapshot, messageID, content);
    if (!currentConversationMatches(snapshot)) {
      return;
    }
    contentInput.value = "";
    mergeMessages([message]);
    if (Number(message.seq) > state.syncedSeq + 1) {
      void catchUpConversation(snapshot);
    } else {
      while (state.messages.has(state.syncedSeq + 1)) {
        state.syncedSeq += 1;
      }
    }
    renderMessages("bottom", false);
    await loadThreads();
  } catch (error) {
    if (currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (currentConversationMatches(snapshot)) {
      sendButton.disabled = false;
    }
  }
});

logoutButton.addEventListener("click", () => {
  clearSession();
  showNotice("Đã đăng xuất.");
  showError("");
});

refreshButton.addEventListener("click", async () => {
  await Promise.all([loadThreads(), syncCurrentConversation()]);
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
  if (document.visibilityState !== "visible" || !state.token) {
    return;
  }
  const snapshot = conversationSnapshot();
  recordVisibleMessages(snapshot);
});

window.addEventListener("online", () => {
  if (!state.token || state.socket || state.socketConnecting) {
    return;
  }
  if (state.reconnectTimer !== null) {
    clearTimeout(state.reconnectTimer);
    state.reconnectTimer = null;
  }
  void connectWebSocket();
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
