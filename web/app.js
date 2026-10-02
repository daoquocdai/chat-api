const sessionTokenKey = "mini-hermes.access-token";
const sessionUsernameKey = "mini-hermes.username";
const messagePageLimit = 30;
const threadSummaryDebounceMs = 750;
const websocketConnectTimeoutMs = 10000;
const websocketHeartbeatTimeoutMs = 15000;
const websocketResumeGraceMs = 5000;
const historyBottomTolerance = 4;
const pendingSendMismatchMessage = "Tin trước chưa được xác nhận. Khôi phục đúng nội dung cũ để bấm Gửi lại; chỉ gửi tin mới sau khi tin cũ thành công.";

const authView = document.querySelector("#auth-view");
const chatView = document.querySelector("#chat-view");
const registerForm = document.querySelector("#register-form");
const loginForm = document.querySelector("#login-form");
const logoutButton = document.querySelector("#logout-button");
const currentUsername = document.querySelector("#current-username");
const peerList = document.querySelector("#peer-list");
const directThreadList = document.querySelector("#direct-thread-list");
const groupThreadList = document.querySelector("#group-thread-list");
const newDirectSection = document.querySelector("#new-direct-section");
const peerName = document.querySelector("#peer-name");
const threadStatus = document.querySelector("#thread-status");
const historyElement = document.querySelector("#history");
const messageForm = document.querySelector("#message-form");
const contentInput = document.querySelector("#content");
const sendButton = document.querySelector("#send-button");
const loadOlderButton = document.querySelector("#load-older-button");
const noticeElement = document.querySelector("#notice");
const errorElement = document.querySelector("#error");
const groupForm = document.querySelector("#group-form");
const groupUsers = document.querySelector("#group-users");
const createGroupButton = document.querySelector("#create-group-button");
const recoverGroupButton = document.querySelector("#recover-group-button");
const membersButton = document.querySelector("#members-button");
const membersPanel = document.querySelector("#members-panel");
const memberList = document.querySelector("#member-list");
const addMemberForm = document.querySelector("#add-member-form");
const addMemberUser = document.querySelector("#add-member-user");
const addMemberButton = document.querySelector("#add-member-button");
const leaveButton = document.querySelector("#leave-button");

const state = {
  token: "", sessionVersion: 0, sessionAbort: new AbortController(),
  currentUserID: "", currentUsername: "", users: [],
  // One entry per thread: summary, permissions, messages, drafts and requests.
  threads: new Map(), directRequestsByPeer: new Map(),
  threadID: "", currentCache: null, conversationVersion: 0,
  readFrame: null, readSerial: 0,
  membershipSerial: 0, threadListRequest: null, threadListDirty: false,
  threadListCatchup: false, threadSummaryTimer: null, groupCreation: null,
  socket: null, socketConnecting: false, socketGeneration: 0,
  socketTicketAbort: null, socketHealthTimer: null, socketLastActivityAt: 0,
  socketSummaryStale: false, reconnectTimer: null, reconnectDelay: 1000,
};

function showNotice(message) {
  noticeElement.textContent = message;
  noticeElement.hidden = !message;
}
function showError(message) {
  errorElement.textContent = message;
  errorElement.hidden = !message;
}
function canUseThread(cache) {
  return Boolean(cache?.summary && cache.active && !cache.permissionsPending);
}
function updateSendButton() {
  const cache = state.currentCache;
  const pending = cache?.pendingSend;
  sendButton.textContent = pending ? "Gửi lại" : "Gửi";
  sendButton.disabled = !canUseThread(cache) || Boolean(pending?.inFlight);
  contentInput.disabled = !canUseThread(cache);
}
function renderThreadHeading() {
  const cache = state.currentCache;
  const thread = cache?.summary;
  peerName.textContent = thread ? threadTitle(thread) : "Chọn cuộc trò chuyện";
  const group = thread?.kind === "group";
  threadStatus.textContent = group
    ? !cache.active ? "Bạn đã rời hoặc bị xóa khỏi nhóm; chỉ xem lịch sử đã được cấp quyền."
      : cache.permissionsPending ? "Đang đối chiếu thành viên và quyền..."
      : `${thread.member_count} thành viên · ${thread.role === "admin" ? "Quản trị viên" : "Thành viên"}`
    : "";
  membersButton.hidden = !group || !cache.active;
  if (!group || !cache.active) membersPanel.hidden = true;
  updateSendButton();
  if (group && !membersPanel.hidden) renderMembers(cache);
}
function decodeSubject(token) {
  try {
    const normalized = token.split(".")[1].replace(/-/g, "+").replace(/_/g, "/");
    return JSON.parse(atob(normalized.padEnd(Math.ceil(normalized.length / 4) * 4, "="))).sub || "";
  } catch { return ""; }
}
async function readResponse(response) {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(body.error || "Yêu cầu không thành công.");
    error.status = response.status;
    error.details = body; // Preserve committed thread/message IDs and seq on 503.
    throw error;
  }
  return body;
}
async function apiRequest(path, options = {}, authenticated = true) {
  const requestToken = state.token;
  const requestSessionVersion = state.sessionVersion;
  const headers = new Headers(options.headers || {});
  if (authenticated) headers.set("Authorization", `Bearer ${requestToken}`);
  const signal = options.signal
    ? AbortSignal.any([state.sessionAbort.signal, options.signal]) : state.sessionAbort.signal;
  const response = await fetch(path, { ...options, headers, signal });
  if (authenticated && response.status === 401 &&
      currentSessionMatches(requestSessionVersion, requestToken)) {
    clearSession();
    showError("Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.");
    throw new Error("Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.");
  }
  return readResponse(response);
}
async function sendMessageWithRetry(snapshot, messageID, content) {
  const request = { method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ message_id: messageID, content }) };
  for (let attempt = 0; attempt < 2; attempt += 1) {
    try { return await apiRequest(`/threads/${snapshot.threadID}/messages`, request); }
    catch (error) {
      if (attempt === 1 || (error.status !== undefined && error.status < 500) ||
          !cacheSnapshotMatches(snapshot) || !canUseThread(snapshot.cache)) throw error;
    }
  }
}
function resetConversation() {
  state.threadID = "";
  state.currentCache = null;
  state.conversationVersion += 1;
  peerName.textContent = "Chọn cuộc trò chuyện";
  threadStatus.textContent = "";
  historyElement.replaceChildren();
  const empty = document.createElement("p");
  empty.className = "empty";
  empty.textContent = "Chọn tài khoản hoặc nhóm để bắt đầu chat.";
  historyElement.append(empty);
  contentInput.value = "";
  membersPanel.hidden = true;
  membersButton.hidden = true;
  updateSendButton();
  loadOlderButton.hidden = true;
}
function clearSession() {
  stopWebSocket();
  state.sessionAbort.abort();
  state.sessionAbort = new AbortController();
  sessionStorage.removeItem(sessionTokenKey);
  sessionStorage.removeItem(sessionUsernameKey);
  state.sessionVersion += 1;
  state.token = "";
  state.currentUserID = "";
  state.currentUsername = "";
  state.users = [];
  state.threads = new Map();
  state.directRequestsByPeer = new Map();
  state.threadListRequest = null;
  state.threadListDirty = false;
  state.threadListCatchup = false;
  state.membershipSerial = 0;
  state.groupCreation = null;
  if (state.readFrame !== null) cancelAnimationFrame(state.readFrame);
  state.readFrame = null;
  state.readSerial = 0;
  createGroupButton.disabled = false;
  recoverGroupButton.hidden = true;
  groupForm.reset();
  if (state.threadSummaryTimer !== null) clearTimeout(state.threadSummaryTimer);
  state.threadSummaryTimer = null;
  resetConversation();
  authView.hidden = false;
  chatView.hidden = true;
  logoutButton.hidden = true;
  currentUsername.textContent = "";
  peerList.replaceChildren();
  directThreadList.replaceChildren();
  groupThreadList.replaceChildren();
  newDirectSection.hidden = true;
  groupUsers.replaceChildren();
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
  return { ...cacheSnapshot(state.currentCache), version: state.conversationVersion };
}
function newThreadCache(threadID) {
  return { threadID, summary: null, active: false, permissionsPending: false,
    membershipEpoch: 0, members: null, membersDirty: true, membersRequest: null,
    memberMutation: null, eventMessages: new Map(), summaryBaseSeq: 0,
    messages: new Map(), messageIDs: new Map(), messagesLoaded: false,
    syncedSeq: 0, nextCursor: null, lastReadSeq: 0, peerLastReadSeq: 0,
    initialHistoryRequest: null, catchupRequest: null, catchupTargetSeq: 0,
    olderRequest: null, readRequest: null, pendingReadSeq: 0,
    pendingSend: null, draftContent: "" };
}
function cacheForThread(threadID) {
  if (!state.threads.has(threadID)) state.threads.set(threadID, newThreadCache(threadID));
  return state.threads.get(threadID);
}
function cacheSnapshot(cache) {
  return { sessionVersion: state.sessionVersion, token: state.token,
    threadID: cache?.threadID || "", cache, membershipEpoch: cache?.membershipEpoch };
}
function cacheSnapshotMatches(snapshot) {
  return currentSessionMatches(snapshot.sessionVersion, snapshot.token) &&
    state.threads.get(snapshot.threadID) === snapshot.cache;
}
function currentConversationMatches(snapshot) {
  return cacheSnapshotMatches(snapshot) && snapshot.version === state.conversationVersion &&
    snapshot.threadID === state.threadID && snapshot.cache === state.currentCache;
}
function membershipSnapshotMatches(snapshot) {
  return cacheSnapshotMatches(snapshot) && snapshot.membershipEpoch === snapshot.cache.membershipEpoch;
}
function peerByID(id) { return state.users.find((user) => user.id === id); }
function threadTitle(thread) { return thread.kind === "group" ? thread.name : thread.peer?.username || "Chat 1-1"; }
function directThreadForPeer(peerID) {
  return [...state.threads.values()].find((cache) => cache.summary?.kind === "direct" &&
    cache.summary.peer?.id === peerID);
}
function renderPeerList() {
  directThreadList.replaceChildren();
  groupThreadList.replaceChildren();
  for (const cache of state.threads.values()) {
    const thread = cache.summary;
    if (!thread) continue;
    const button = document.createElement("button");
    button.type = "button";
    button.className = `peer${thread.id === state.threadID ? " active" : ""}`;
    const name = document.createElement("span");
    name.textContent = `${threadTitle(thread)}${thread.kind === "group"
      ? cache.active ? ` · ${thread.member_count} người` : " · Đã rời (lịch sử)" : ""}`;
    button.append(name);
    const unread = cache.active ? Number(thread.unread_count || 0) : 0;
    if (unread > 0) {
      const badge = document.createElement("span");
      badge.className = "unread-badge";
      badge.textContent = unread > 99 ? "99+" : String(unread);
      badge.setAttribute("aria-label", `${unread} tin chưa đọc`);
      button.append(badge);
    }
    button.addEventListener("click", () => openThread(thread.id));
    (thread.kind === "group" ? groupThreadList : directThreadList).append(button);
  }
  for (const [list, text] of [[directThreadList, "Chưa có cuộc trò chuyện 1-1."],
    [groupThreadList, "Chưa tham gia nhóm nào."]]) {
    if (list.children.length) continue;
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = text;
    list.append(empty);
  }
  peerList.replaceChildren();
  const newPeers = state.users.filter((user) => user.id !== state.currentUserID && !directThreadForPeer(user.id));
  newDirectSection.hidden = newPeers.length === 0;
  for (const user of newPeers) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "peer";
    button.textContent = user.username;
    button.addEventListener("click", () => openDirect(user.id));
    peerList.append(button);
  }
}
async function loadUsers() {
  const sessionVersion = state.sessionVersion, token = state.token;
  const users = await apiRequest("/users");
  if (!currentSessionMatches(sessionVersion, token)) return;
  const me = users.find((user) => user.id === state.currentUserID);
  if (!me) { clearSession(); throw new Error("Tài khoản không còn tồn tại."); }
  state.users = users;
  state.currentUsername = me.username;
  sessionStorage.setItem(sessionUsernameKey, me.username);
  currentUsername.textContent = me.username;
  groupUsers.replaceChildren();
  for (const user of users.filter((user) => user.id !== state.currentUserID)) {
    const label = document.createElement("label");
    const checkbox = document.createElement("input");
    checkbox.type = "checkbox"; checkbox.value = user.id;
    const name = document.createElement("span"); name.textContent = user.username;
    label.append(checkbox, name); groupUsers.append(label);
  }
  renderPeerList();
}
function receivedMessage(message) {
  return message.sender_id !== state.currentUserID;
}
function reconcileThreadSummary(thread) {
  const cache = cacheForThread(thread.id);
  const old = cache.summary;
  const rejoined = old && thread.kind === "group" && Number(old.joined_seq) !== Number(thread.joined_seq);
  if (rejoined) {
    cache.membershipEpoch += 1;
    cache.lastReadSeq = Number(thread.last_read_seq || 0);
    cache.pendingReadSeq = 0;
    cache.readRequest = null;
    cache.members = null;
    cache.membersDirty = true;
  } else if (cache.lastReadSeq > Number(thread.last_read_seq || 0)) {
    // An older summary cannot undo a PUT that already completed.
    if (cache.lastReadSeq < Number(thread.last_seq || 0)) {
      scheduleThreadSummaryRefresh();
      return cache;
    }
    thread.unread_count = 0;
  }
  cache.lastReadSeq = Math.max(cache.lastReadSeq, Number(thread.last_read_seq || 0));
  thread.last_read_seq = cache.lastReadSeq;
  cache.peerLastReadSeq = thread.kind === "direct"
    ? Math.max(cache.peerLastReadSeq, Number(thread.peer_last_read_seq || 0)) : 0;
  cache.summaryBaseSeq = Number(thread.last_seq || 0);
  for (const [seq, message] of cache.eventMessages) {
    if (seq <= cache.summaryBaseSeq) cache.eventMessages.delete(seq);
    else {
      thread.last_seq = Math.max(Number(thread.last_seq || 0), seq);
      if (receivedMessage(message) && seq > cache.lastReadSeq) thread.unread_count += 1;
    }
  }
  cache.summary = thread;
  cache.active = true;
  cache.permissionsPending = false;
  return cache;
}
function deactivateGroup(cache) {
  if (cache.active) cache.membershipEpoch += 1;
  cache.active = false;
  cache.permissionsPending = false;
  cache.pendingReadSeq = 0;
  cache.members = null;
  if (cache.summary) cache.summary.unread_count = 0;
}
function updateThreadFromMessage(message) {
  const cache = cacheForThread(message.thread_id);
  const seq = Number(message.seq);
  if (!Number.isSafeInteger(seq) || seq <= 0 || seq <= cache.summaryBaseSeq || cache.eventMessages.has(seq)) return;
  cache.eventMessages.set(seq, message);
  const thread = cache.summary;
  if (!thread) scheduleThreadSummaryRefresh();
  else {
    thread.last_seq = Math.max(Number(thread.last_seq || 0), seq);
    if (cache.active && receivedMessage(message) && seq > cache.lastReadSeq) thread.unread_count += 1;
  }
  if (message.kind === "system") {
    state.membershipSerial += 1;
    cache.permissionsPending = true;
    cache.membersDirty = true;
    if (state.threadListRequest) state.threadListDirty = true;
    scheduleThreadSummaryRefresh();
    if (state.currentCache === cache) renderThreadHeading();
  }
  renderPeerList();
}
function acceptMessage(message) {
  const cache = cacheForThread(message.thread_id);
  const changed = mergeMessages(cache, [message]);
  if (cache.messagesLoaded && changed) {
    const seq = Number(message.seq);
    if (seq > cache.syncedSeq + 1) void catchUpConversation(cache, seq);
    else while (cache.messages.has(cache.syncedSeq + 1)) cache.syncedSeq += 1;
  }
  return { cache, changed };
}
function scheduleThreadSummaryRefresh() {
  if (state.threadSummaryTimer !== null) return;
  const sessionVersion = state.sessionVersion, token = state.token;
  state.threadSummaryTimer = setTimeout(() => {
    state.threadSummaryTimer = null;
    if (currentSessionMatches(sessionVersion, token)) void loadThreads();
  }, threadSummaryDebounceMs);
}
async function loadThreads(catchUpCached = false) {
  if (!state.token) return;
  state.threadListCatchup ||= catchUpCached;
  if (state.threadSummaryTimer !== null) clearTimeout(state.threadSummaryTimer);
  state.threadSummaryTimer = null;
  if (state.threadListRequest) return state.threadListRequest.promise;
  const sessionVersion = state.sessionVersion, token = state.token;
  const request = {};
  state.threadListRequest = request;
  request.promise = (async () => {
    try {
      do {
        state.threadListDirty = false;
        const serial = state.membershipSerial;
        const readSerial = state.readSerial;
        const threads = await apiRequest("/threads");
        if (!currentSessionMatches(sessionVersion, token)) return;
        // Membership/read changes after GET began invalidate its permissions/counts.
        if (serial !== state.membershipSerial || readSerial !== state.readSerial) {
          state.threadListDirty = true;
          continue;
        }
        const activeIDs = new Set(threads.map((thread) => thread.id));
        for (const thread of threads) {
          const cache = reconcileThreadSummary(thread);
          if (cache.messagesLoaded && (state.threadListCatchup || cache.syncedSeq < Number(thread.last_seq))) {
            void catchUpConversation(cache, Number(thread.last_seq));
          }
        }
        for (const cache of state.threads.values()) {
          if (cache.summary?.kind === "group" && !activeIDs.has(cache.threadID)) {
            const wasActive = cache.active;
            deactivateGroup(cache);
            // Also recover the removal/leave notice if it happened while offline.
            if (wasActive && cache.messagesLoaded) void catchUpConversation(cache);
          }
        }
        renderPeerList();
        renderThreadHeading();
        const cache = state.currentCache;
        if (cache?.messagesLoaded) renderMessages("preserve");
        if (cache?.summary?.kind === "group" && cache.active && !membersPanel.hidden && cache.membersDirty) {
          void loadMembers(cache);
        }
        state.threadListCatchup = false;
      } while (state.threadListDirty && currentSessionMatches(sessionVersion, token));
    } catch (error) {
      if (currentSessionMatches(sessionVersion, token)) showError(error.message);
    } finally {
      if (state.threadListRequest === request) state.threadListRequest = null;
    }
  })();
  return request.promise;
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

function atConversationBottom() {
  return historyElement.scrollHeight - historyElement.scrollTop - historyElement.clientHeight <= historyBottomTolerance;
}

function renderMessages(scrollMode = "preserve") {
  const cache = state.currentCache;
  if (!cache) return;
  const previousHeight = historyElement.scrollHeight;
  const previousTop = historyElement.scrollTop;
  const wasAtBottom = atConversationBottom();
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
      item.className = `message ${message.kind === "system" ? "system" : sent ? "sent" : "received"}`;
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
      const readStatus = cache.summary?.kind === "direct" && sent && message.seq <= cache.peerLastReadSeq ? " · Đã đọc" : "";
      meta.textContent = `${displayName(message.sender_id)} · ${time}${readStatus}`;
      item.append(content, meta);
      historyElement.append(item);
    }
  }

  if (scrollMode === "initial" || scrollMode === "bottom" || (scrollMode === "new" && wasAtBottom)) {
    historyElement.scrollTop = historyElement.scrollHeight;
  } else if (scrollMode === "older") {
    historyElement.scrollTop = previousTop + historyElement.scrollHeight - previousHeight;
  } else {
    historyElement.scrollTop = previousTop;
  }
  loadOlderButton.hidden = cache.nextCursor === null;
  loadOlderButton.disabled = cache.nextCursor === null || Boolean(cache.olderRequest);
  scheduleReadMarker();
}

function pageURL(threadID, beforeSeq = null) {
  const params = new URLSearchParams({ limit: String(messagePageLimit) });
  if (beforeSeq !== null) {
    params.set("before_seq", String(beforeSeq));
  }
  return `/threads/${threadID}/messages?${params}`;
}

async function loadInitialHistory(cache = state.currentCache) {
  if (!cache || !state.token || cache.messagesLoaded) return;
  if (cache.initialHistoryRequest) return cache.initialHistoryRequest.promise;
  const snapshot = cacheSnapshot(cache);
  const request = {};
  cache.initialHistoryRequest = request;
  request.promise = (async () => {
    try {
      const page = await apiRequest(pageURL(snapshot.threadID));
      if (!cacheSnapshotMatches(snapshot)) return;
      mergeMessages(cache, Array.isArray(page.messages) ? page.messages : []);
      cache.nextCursor = page.next_cursor ?? null;
      cache.syncedSeq = Math.max(0, ...(page.messages || []).map((message) => Number(message.seq) || 0));
      cache.messagesLoaded = true;
      if (state.currentCache === cache) renderMessages("initial");
      const target = Math.max(highestMessageSeq(cache), Number(cache.summary?.last_seq || 0));
      if (target > cache.syncedSeq) await catchUpConversation(cache, target);
    } catch (error) {
      if (cacheSnapshotMatches(snapshot) && state.currentCache === cache) showError(error.message);
    } finally {
      if (cache.initialHistoryRequest === request) cache.initialHistoryRequest = null;
    }
  })();
  return request.promise;
}

async function catchUpConversation(cache = state.currentCache, targetSeq = 0) {
  if (!cache || state.threads.get(cache.threadID) !== cache || !cache.messagesLoaded || !state.token) return;
  if (cache.catchupRequest) {
    cache.catchupTargetSeq = Math.max(cache.catchupTargetSeq, targetSeq);
    return cache.catchupRequest.promise;
  }
  const snapshot = cacheSnapshot(cache);
  const request = {};
  cache.catchupRequest = request;
  request.promise = (async () => {
    const baseline = cache.syncedSeq;
    let completed = false;
    try {
      const maxSeq = await MiniHermesRealtime.fetchThroughBoundary(
        async (beforeSeq) => {
          const page = await apiRequest(pageURL(snapshot.threadID, beforeSeq));
          if (!cacheSnapshotMatches(snapshot)) throw new Error("session changed");
          return page;
        }, baseline,
        (messages) => mergeMessages(cache, messages),
      );
      if (!cacheSnapshotMatches(snapshot)) return;
      cache.syncedSeq = Math.max(cache.syncedSeq, maxSeq);
      completed = true;
      if (state.currentCache === cache) renderMessages("new");
    } catch (error) {
      if (cacheSnapshotMatches(snapshot) && state.currentCache === cache) showError(error.message);
    } finally {
      if (cache.catchupRequest !== request) return;
      cache.catchupRequest = null;
      const nextTarget = cache.catchupTargetSeq;
      cache.catchupTargetSeq = 0;
      // Only a newer event received during this fetch can cause one more pass.
      // The same inaccessible target must never keep a group in a sync loop.
      if (completed && nextTarget > cache.syncedSeq && nextTarget > targetSeq && cacheSnapshotMatches(snapshot)) {
        void catchUpConversation(cache, nextTarget);
      }
    }
  })();
  return request.promise;
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

function renderedReadSequence(snapshot) {
  const cache = snapshot.cache;
  if (
    !currentConversationMatches(snapshot) ||
    !membershipSnapshotMatches(snapshot) ||
    document.visibilityState !== "visible" ||
    chatView.hidden ||
    !cache.messagesLoaded || !canUseThread(cache) || !atConversationBottom()
  ) {
    return 0;
  }
  const latest = historyElement.querySelector(".message:last-child");
  if (!latest) return 0;
  const historyRect = historyElement.getBoundingClientRect();
  const messageRect = latest.getBoundingClientRect();
  const visibleTop = Math.max(0, historyRect.top);
  const visibleBottom = Math.min(window.innerHeight, historyRect.bottom);
  if (messageRect.bottom <= visibleTop || messageRect.top >= visibleBottom ||
      messageRect.bottom > visibleBottom + historyBottomTolerance ||
      messageRect.right <= 0 || messageRect.left >= window.innerWidth) return 0;
  const seq = Number(latest.dataset.seq);
  return Number.isSafeInteger(seq) && seq >= Number(cache.summary.joined_seq || 1) ? seq : 0;
}

function markConversationRead(snapshot = conversationSnapshot()) {
  const seq = renderedReadSequence(snapshot);
  if (seq <= (snapshot.cache?.lastReadSeq || 0)) return;
  snapshot.cache.pendingReadSeq = Math.max(snapshot.cache.pendingReadSeq, seq);
  void flushReadMarker(snapshot);
}

function scheduleReadMarker() {
  if (state.readFrame !== null) cancelAnimationFrame(state.readFrame);
  const snapshot = conversationSnapshot();
  state.readFrame = requestAnimationFrame(() => {
    state.readFrame = null;
    markConversationRead(snapshot);
  });
}

async function flushReadMarker(snapshot = conversationSnapshot()) {
  const cache = snapshot.cache;
  const renderedSeq = renderedReadSequence(snapshot);
  if (!renderedSeq || cache.readRequest || cache.pendingReadSeq <= cache.lastReadSeq) {
    return;
  }

  const target = Math.min(cache.pendingReadSeq, renderedSeq);
  if (target <= cache.lastReadSeq) return;
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
    if (!membershipSnapshotMatches(snapshot) || !canUseThread(cache)) return;

    const previousReadSeq = cache.lastReadSeq;
    const hadUnread = Number(cache.summary.unread_count || 0) > 0;
    cache.lastReadSeq = Math.max(cache.lastReadSeq, Number(response.last_read_seq || 0));
    if (cache.lastReadSeq > previousReadSeq) {
      state.readSerial += 1;
      cache.summary.last_read_seq = cache.lastReadSeq;
      if (cache.lastReadSeq >= Number(cache.summary.last_seq || 0)) cache.summary.unread_count = 0;
      renderPeerList();
      // Existing GETs must not restore a marker from before this PUT.
      if (state.threadListRequest) state.threadListDirty = true;
      // Zero unread stays zero when only our own messages advance the marker.
      // Otherwise recount pages not loaded here, including another tab's read.
      if (hadUnread || cache.lastReadSeq > target) void loadThreads();
    }
    completed = true;
  } catch (error) {
    if (membershipSnapshotMatches(snapshot) && canUseThread(cache)) {
      // PUT may have committed despite a lost response; retrying a monotonic marker is safe.
      cache.pendingReadSeq = Math.max(cache.pendingReadSeq, target);
    }
    if (cacheSnapshotMatches(snapshot) && error.status === 403) void loadThreads();
    if (currentConversationMatches(snapshot)) showError(error.message);
  } finally {
    if (cache.readRequest === request) {
      cache.readRequest = null;
    }
    if (completed && membershipSnapshotMatches(snapshot) && currentConversationMatches(snapshot)) {
      markConversationRead(snapshot);
    } else if (completed && membershipSnapshotMatches(snapshot) && state.currentCache === cache) {
      markConversationRead();
    }
  }
}

async function openThread(threadID) {
  const cache = state.threads.get(threadID);
  if (!cache?.summary) return;
  if (state.currentCache) state.currentCache.draftContent = contentInput.value;
  state.conversationVersion += 1;
  state.threadID = threadID;
  state.currentCache = cache;
  contentInput.value = cache.draftContent;
  membersPanel.hidden = true;
  renderPeerList();
  renderThreadHeading();
  showError("");
  if (cache.messagesLoaded) renderMessages("initial");
  else {
    historyElement.replaceChildren();
    loadOlderButton.hidden = true;
    await loadInitialHistory(cache);
  }
  if (state.currentCache === cache) {
    void flushReadMarker();
    if (canUseThread(cache)) contentInput.focus();
  }
}

async function openDirect(peerID) {
  const cached = directThreadForPeer(peerID);
  if (cached) return openThread(cached.threadID);
  if (state.currentCache) state.currentCache.draftContent = contentInput.value;
  const version = ++state.conversationVersion;
  const sessionVersion = state.sessionVersion, token = state.token;
  state.currentCache = null;
  state.threadID = "";
  renderThreadHeading();
  try {
    let request = state.directRequestsByPeer.get(peerID);
    if (!request) {
      request = apiRequest("/threads/direct", { method: "POST",
        headers: { "Content-Type": "application/json" }, body: JSON.stringify({ peer_id: peerID }) });
      state.directRequestsByPeer.set(peerID, request);
      void request.finally(() => {
        if (state.directRequestsByPeer.get(peerID) === request) state.directRequestsByPeer.delete(peerID);
      }).catch(() => {});
    }
    const thread = await request;
    if (!currentSessionMatches(sessionVersion, token)) return;
    reconcileThreadSummary(thread);
    renderPeerList();
    if (version === state.conversationVersion) await openThread(thread.id);
  } catch (error) {
    if (version === state.conversationVersion && currentSessionMatches(sessionVersion, token)) showError(error.message);
  }
}

function renderMembers(cache) {
  if (state.currentCache !== cache || membersPanel.hidden) return;
  memberList.replaceChildren();
  const admin = canUseThread(cache) && cache.summary.role === "admin";
  for (const member of cache.members || []) {
    const item = document.createElement("li");
    const label = document.createElement("span");
    label.textContent = `${member.username} · ${member.role === "admin" ? "Quản trị viên" : "Thành viên"}`;
    item.append(label);
    if (admin && member.id !== state.currentUserID) {
      const remove = document.createElement("button");
      remove.type = "button";
      remove.textContent = "Xóa";
      remove.disabled = Boolean(cache.memberMutation);
      remove.addEventListener("click", () => changeMembership(cache, "remove", member.id));
      item.append(remove);
    }
    memberList.append(item);
  }
  addMemberForm.hidden = !admin;
  const selected = addMemberUser.value;
  addMemberUser.replaceChildren();
  const memberIDs = new Set((cache.members || []).map((member) => member.id));
  for (const user of state.users.filter((user) => !memberIDs.has(user.id))) {
    const option = document.createElement("option");
    option.value = user.id; option.textContent = user.username;
    addMemberUser.append(option);
  }
  if ([...addMemberUser.options].some((option) => option.value === selected)) addMemberUser.value = selected;
  addMemberButton.disabled = !admin || !cache.members || !addMemberUser.options.length || Boolean(cache.memberMutation);
  leaveButton.disabled = !canUseThread(cache) || Boolean(cache.memberMutation);
}
async function loadMembers(cache = state.currentCache) {
  if (!cache?.active || cache.summary?.kind !== "group") return;
  if (cache.membersRequest) return cache.membersRequest;
  const snapshot = cacheSnapshot(cache), serial = state.membershipSerial;
  const request = (async () => {
    try {
      const members = await apiRequest(`/threads/${cache.threadID}/members`);
      if (!membershipSnapshotMatches(snapshot) || !cache.active || serial !== state.membershipSerial) return;
      cache.members = members;
      cache.membersDirty = false;
      renderMembers(cache);
    } catch (error) {
      if (!cacheSnapshotMatches(snapshot)) return;
      if (error.status === 403) void loadThreads();
      if (state.currentCache === cache) showError(error.message);
    } finally {
      if (cache.membersRequest === request) cache.membersRequest = null;
      if (cacheSnapshotMatches(snapshot) && cache.active && cache.membersDirty &&
          (serial !== state.membershipSerial || !membershipSnapshotMatches(snapshot)) &&
          state.currentCache === cache && !membersPanel.hidden) {
        void loadMembers(cache);
      }
    }
  })();
  cache.membersRequest = request;
  return request;
}
async function changeMembership(cache, action, userID = "") {
  if (!canUseThread(cache) || cache.memberMutation || (action !== "leave" && cache.summary.role !== "admin")) return;
  const snapshot = cacheSnapshot(cache);
  const request = {};
  cache.memberMutation = request;
  renderMembers(cache);
  const path = action === "leave" ? `/threads/${cache.threadID}/leave`
    : `/threads/${cache.threadID}/members${action === "remove" ? `/${userID}` : ""}`;
  try {
    const message = await apiRequest(path, { method: action === "remove" ? "DELETE" : "POST",
      headers: { "Content-Type": "application/json" },
      ...(action === "add" ? { body: JSON.stringify({ user_id: userID }) } : {}) });
    if (!cacheSnapshotMatches(snapshot)) return;
    updateThreadFromMessage(message);
    acceptMessage(message);
    if (state.currentCache === cache) renderMessages("new");
  } catch (error) {
    if (!cacheSnapshotMatches(snapshot)) return;
    if (error.status === 503 && error.details?.thread_id) {
      showNotice("Thay đổi thành viên đã lưu; realtime chưa được xác nhận. Đang đối chiếu trạng thái.");
      void catchUpConversation(cache, Number(error.details.seq || 0));
    } else if (state.currentCache === cache) showError(error.message);
  } finally {
    if (cache.memberMutation === request) cache.memberMutation = null;
    if (cacheSnapshotMatches(snapshot)) {
      state.membershipSerial += 1;
      cache.permissionsPending = true;
      cache.membersDirty = true;
      state.threadListDirty = true;
      if (state.currentCache === cache) { renderThreadHeading(); renderMembers(cache); }
      await loadThreads();
      if (cacheSnapshotMatches(snapshot) && cache.active && state.currentCache === cache && !membersPanel.hidden) await loadMembers(cache);
    }
  }
}
async function recoverCreatedGroup(creation) {
  const sessionVersion = state.sessionVersion, token = state.token;
  await loadThreads();
  if (!currentSessionMatches(sessionVersion, token) || state.groupCreation !== creation) return;
  if (creation.threadID && state.threads.get(creation.threadID)?.active) {
    state.groupCreation = null;
    createGroupButton.disabled = false;
    recoverGroupButton.hidden = true;
    groupForm.reset();
    if (creation.conversationVersion === state.conversationVersion) await openThread(creation.threadID);
    if (currentSessionMatches(sessionVersion, token)) showNotice("Nhóm đã được lưu. Đã đối chiếu và mở nhóm; không tạo lại nhóm.");
  } else {
    showNotice(creation.threadID
      ? `Nhóm đã lưu với ID ${creation.threadID}. Chưa đối chiếu được; bấm Đối chiếu nhóm đã tạo khi API hoạt động lại.`
      : "Chưa xác nhận kết quả tạo nhóm. Hãy kiểm tra danh sách nhóm trước khi tải lại trang và tạo nhóm khác.");
    recoverGroupButton.hidden = false;
  }
}

groupForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (state.groupCreation || !state.token) return;
  const sessionVersion = state.sessionVersion, token = state.token;
  const creation = { threadID: "", conversationVersion: state.conversationVersion };
  state.groupCreation = creation;
  createGroupButton.disabled = true;
  showError(""); showNotice("");
  const memberIDs = [...groupUsers.querySelectorAll("input:checked")].map((input) => input.value);
  try {
    const thread = await apiRequest("/threads/group", { method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: groupForm.elements.name.value, member_ids: memberIDs }) });
    if (!currentSessionMatches(sessionVersion, token)) return;
    state.membershipSerial += 1;
    reconcileThreadSummary(thread);
    state.groupCreation = null;
    createGroupButton.disabled = false;
    groupForm.reset();
    renderPeerList();
    if (creation.conversationVersion === state.conversationVersion) await openThread(thread.id);
  } catch (error) {
    if (!currentSessionMatches(sessionVersion, token)) return;
    if (error.status === undefined || error.status >= 500) {
      creation.threadID = error.details?.thread_id || "";
      state.membershipSerial += 1;
      state.threadListDirty = true;
      await recoverCreatedGroup(creation);
    } else {
      state.groupCreation = null;
      createGroupButton.disabled = false;
      showError(error.message);
    }
  }
});
recoverGroupButton.addEventListener("click", () => {
  if (state.groupCreation) {
    state.groupCreation.conversationVersion = state.conversationVersion;
    void recoverCreatedGroup(state.groupCreation);
  }
});
membersButton.addEventListener("click", () => {
  const cache = state.currentCache;
  if (!cache?.active || cache.summary?.kind !== "group") return;
  membersPanel.hidden = !membersPanel.hidden;
  if (!membersPanel.hidden) {
    renderMembers(cache);
    if (!cache.members || cache.membersDirty) void loadMembers(cache);
  }
});
addMemberForm.addEventListener("submit", (event) => {
  event.preventDefault();
  if (state.currentCache && addMemberUser.value) void changeMembership(state.currentCache, "add", addMemberUser.value);
});
leaveButton.addEventListener("click", () => {
  if (state.currentCache) void changeMembership(state.currentCache, "leave");
});

registerForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  showError("");
  showNotice("");
  const username = registerForm.elements.username.value;
  const password = registerForm.elements.password.value;
  const sessionVersion = state.sessionVersion;

  try {
    await apiRequest("/auth/register", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    }, false);
    if (sessionVersion !== state.sessionVersion) return;
    loginForm.elements.username.value = username;
    loginForm.elements.password.focus();
    registerForm.reset();
    showNotice("Đăng ký thành công. Hãy đăng nhập bằng tài khoản vừa tạo.");
  } catch (error) {
    if (sessionVersion === state.sessionVersion) showError(error.message);
  }
});

loginForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  showError("");
  showNotice("");
  const loginVersion = state.sessionVersion;
  let activeLoginVersion = loginVersion;
  const username = loginForm.elements.username.value;
  const password = loginForm.elements.password.value;

  try {
    const response = await apiRequest("/auth/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    }, false);
    if (loginVersion !== state.sessionVersion) return;
    const userID = decodeSubject(response.access_token);
    if (!userID) {
      throw new Error("Token đăng nhập không hợp lệ.");
    }

    clearSession();
    activeLoginVersion = state.sessionVersion;
    state.token = response.access_token;
    state.currentUserID = userID;
    state.currentUsername = username.trim().toLowerCase();
    sessionStorage.setItem(sessionTokenKey, state.token);
    sessionStorage.setItem(sessionUsernameKey, state.currentUsername);
    loginForm.reset();
    resetConversation();
    showChatView();
    await loadUsers();
    if (state.token !== response.access_token) return;
    await loadThreads();
    if (state.token !== response.access_token) return;
    void connectWebSocket();
  } catch (error) {
    if (activeLoginVersion === state.sessionVersion) showError(error.message);
  }
});

messageForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!canUseThread(state.currentCache)) return;
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
      renderMessages(currentConversationMatches(snapshot) ? "bottom" : "new");
    }
  } catch (error) {
    if (cacheSnapshotMatches(snapshot) && error.status === 403) void loadThreads();
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

historyElement.addEventListener("scroll", scheduleReadMarker);
window.addEventListener("scroll", scheduleReadMarker);
window.addEventListener("resize", scheduleReadMarker);

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
  markConversationRead(snapshot);
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
  const sessionVersion = state.sessionVersion, token = state.token;
  showChatView();
  Promise.all([loadUsers(), loadThreads()])
    .then(() => { if (currentSessionMatches(sessionVersion, token)) return connectWebSocket(); })
    .catch((error) => { if (currentSessionMatches(sessionVersion, token)) showError(error.message); });
} else {
  clearSession();
}
