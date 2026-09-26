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
  messagesLoaded: false,
  nextCursor: null,
  lastReadSeq: 0,
  peerLastReadSeq: 0,
  seenReceivedSeqs: new Set(),
  latestRequest: null,
  olderRequest: null,
  threadListRequest: null,
  readRequest: null,
  pendingReadSeq: 0,
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
    throw new Error(body.error || "Yêu cầu không thành công.");
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

function resetConversation() {
  state.peerID = "";
  state.threadID = "";
  state.conversationVersion += 1;
  state.messages = new Map();
  state.messagesLoaded = false;
  state.nextCursor = null;
  state.lastReadSeq = 0;
  state.peerLastReadSeq = 0;
  state.seenReceivedSeqs = new Set();
  state.latestRequest = null;
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
  sessionStorage.removeItem(sessionTokenKey);
  sessionStorage.removeItem(sessionUsernameKey);
  state.sessionVersion += 1;
  state.token = "";
  state.currentUserID = "";
  state.currentUsername = "";
  state.users = [];
  state.threadsByPeer = new Map();
  state.threadListRequest = null;
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
  for (const message of messages) {
    const seq = Number(message.seq);
    if (Number.isSafeInteger(seq) && seq > 0) {
      state.messages.set(seq, message);
    }
  }
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

async function pollLatestMessages(initial = false) {
  if (!state.threadID || !state.token) {
    return;
  }
  const snapshot = conversationSnapshot();
  if (
    state.latestRequest &&
    state.latestRequest.version === snapshot.version &&
    state.latestRequest.threadID === snapshot.threadID
  ) {
    return;
  }

  const request = { ...snapshot };
  state.latestRequest = request;
  const knownHighest = highestMessageSeq();
  const visitedCursors = new Set();
  let beforeSeq = null;
  let firstPage = true;

  try {
    while (true) {
      const page = await apiRequest(pageURL(snapshot.threadID, beforeSeq));
      if (!currentConversationMatches(snapshot)) {
        return;
      }

      const messages = Array.isArray(page.messages) ? page.messages : [];
      if (initial && firstPage) {
        state.nextCursor = page.next_cursor ?? null;
      }
      mergeMessages(messages);

      const reachedKnownMessage = knownHighest > 0 && messages.some((message) => message.seq <= knownHighest);
      const nextCursor = page.next_cursor ?? null;
      if (initial || nextCursor === null || reachedKnownMessage || visitedCursors.has(nextCursor)) {
        break;
      }
      visitedCursors.add(nextCursor);
      beforeSeq = nextCursor;
      firstPage = false;
    }

    state.messagesLoaded = true;
    renderMessages(initial ? "initial" : "new");
    showError("");
  } catch (error) {
    if (currentConversationMatches(snapshot)) {
      showError(error.message);
    }
  } finally {
    if (state.latestRequest === request) {
      state.latestRequest = null;
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
  state.messagesLoaded = false;
  state.nextCursor = null;
  state.lastReadSeq = 0;
  state.peerLastReadSeq = 0;
  state.seenReceivedSeqs = new Set();
  state.latestRequest = null;
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
    await pollLatestMessages(true);
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
  sendButton.disabled = true;
  showError("");
  try {
    const message = await apiRequest(`/threads/${snapshot.threadID}/messages`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        client_msg_id: crypto.randomUUID(),
        content: contentInput.value,
      }),
    });
    if (!currentConversationMatches(snapshot)) {
      return;
    }
    contentInput.value = "";
    mergeMessages([message]);
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
  await Promise.all([loadThreads(), pollLatestMessages()]);
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

window.addEventListener("focus", async () => {
  if (!state.token) {
    return;
  }
  try {
    await loadUsers();
    await Promise.all([loadThreads(), pollLatestMessages()]);
  } catch (error) {
    showError(error.message);
  }
});

document.addEventListener("visibilitychange", () => {
  if (document.visibilityState !== "visible" || !state.token) {
    return;
  }
  const snapshot = conversationSnapshot();
  recordVisibleMessages(snapshot);
  Promise.all([loadThreads(), pollLatestMessages()]).catch((error) => showError(error.message));
});

setInterval(() => {
  if (!state.token) {
    return;
  }
  Promise.all([loadThreads(), pollLatestMessages()]).catch((error) => showError(error.message));
}, 1500);

state.token = sessionStorage.getItem(sessionTokenKey) || "";
state.currentUserID = decodeSubject(state.token);
state.currentUsername = sessionStorage.getItem(sessionUsernameKey) || "";
if (state.token && state.currentUserID) {
  state.sessionVersion += 1;
  showChatView();
  Promise.all([loadUsers(), loadThreads()]).catch((error) => showError(error.message));
} else {
  clearSession();
}
