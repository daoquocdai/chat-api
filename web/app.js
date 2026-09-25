const sessionTokenKey = "mini-hermes.access-token";
const sessionUsernameKey = "mini-hermes.username";

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
const noticeElement = document.querySelector("#notice");
const errorElement = document.querySelector("#error");

const state = {
  token: "",
  currentUserID: "",
  currentUsername: "",
  users: [],
  peerID: "",
  threadID: "",
  conversationVersion: 0,
  loadingMessages: false,
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
  const headers = new Headers(options.headers || {});
  if (authenticated) {
    headers.set("Authorization", `Bearer ${state.token}`);
  }

  const response = await fetch(path, { ...options, headers });
  if (authenticated && response.status === 401) {
    clearSession();
    throw new Error("Phiên đăng nhập đã hết hạn. Vui lòng đăng nhập lại.");
  }
  return readResponse(response);
}

function resetConversation() {
  state.peerID = "";
  state.threadID = "";
  state.conversationVersion += 1;
  peerName.textContent = "Chọn một tài khoản";
  historyElement.innerHTML = '<p class="empty">Chọn một tài khoản để bắt đầu chat.</p>';
  contentInput.value = "";
  contentInput.disabled = true;
  sendButton.disabled = true;
  refreshButton.disabled = true;
}

function clearSession() {
  sessionStorage.removeItem(sessionTokenKey);
  sessionStorage.removeItem(sessionUsernameKey);
  state.token = "";
  state.currentUserID = "";
  state.currentUsername = "";
  state.users = [];
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
    const button = document.createElement("button");
    button.type = "button";
    button.className = `peer${user.id === state.peerID ? " active" : ""}`;
    button.textContent = user.username;
    button.addEventListener("click", () => openConversation(user.id));
    peerList.append(button);
  }
}

async function loadUsers() {
  const users = await apiRequest("/users");
  state.users = users;

  const me = users.find((user) => user.id === state.currentUserID);
  if (!me) {
    clearSession();
    throw new Error("Tài khoản của phiên đăng nhập không còn tồn tại.");
  }

  state.currentUsername = me.username;
  sessionStorage.setItem(sessionUsernameKey, me.username);
  currentUsername.textContent = me.username;

  if (state.peerID && !peerByID(state.peerID)) {
    resetConversation();
  }
  renderPeerList();
}

function displayName(userID) {
  if (userID === state.currentUserID) {
    return state.currentUsername;
  }
  return peerByID(userID)?.username || "unknown";
}

function renderMessages(messages) {
  historyElement.replaceChildren();
  if (messages.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Chưa có tin nhắn nào.";
    historyElement.append(empty);
    return;
  }

  for (const message of messages) {
    const item = document.createElement("article");
    item.className = `message ${message.sender_id === state.currentUserID ? "sent" : "received"}`;

    const content = document.createElement("p");
    content.textContent = message.content;

    const meta = document.createElement("small");
    const time = new Date(message.created_at).toLocaleString("vi-VN", {
      hour: "2-digit",
      minute: "2-digit",
      day: "2-digit",
      month: "2-digit",
    });
    meta.textContent = `${displayName(message.sender_id)} · ${time}`;
    item.append(content, meta);
    historyElement.append(item);
  }
  historyElement.scrollTop = historyElement.scrollHeight;
}

async function openConversation(peerID) {
  const version = ++state.conversationVersion;
  state.peerID = peerID;
  state.threadID = "";
  renderPeerList();
  const peer = peerByID(peerID);
  peerName.textContent = peer?.username || "Đang mở...";
  historyElement.innerHTML = '<p class="empty">Đang tải lịch sử...</p>';
  contentInput.disabled = true;
  sendButton.disabled = true;
  refreshButton.disabled = true;
  showError("");

  try {
    const thread = await apiRequest("/threads/direct", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ peer_id: peerID }),
    });
    if (version !== state.conversationVersion || peerID !== state.peerID) {
      return;
    }
    state.threadID = thread.id;
    contentInput.disabled = false;
    sendButton.disabled = false;
    refreshButton.disabled = false;
    await loadMessages();
    contentInput.focus();
  } catch (error) {
    if (version === state.conversationVersion) {
      showError(error.message);
    }
  }
}

async function loadMessages() {
  if (!state.threadID || state.loadingMessages) {
    return;
  }
  const threadID = state.threadID;
  const version = state.conversationVersion;
  state.loadingMessages = true;
  try {
    const messages = await apiRequest(`/threads/${threadID}/messages`);
    if (version === state.conversationVersion && threadID === state.threadID) {
      renderMessages(messages);
      showError("");
    }
  } catch (error) {
    if (version === state.conversationVersion) {
      showError(error.message);
    }
  } finally {
    state.loadingMessages = false;
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

    state.token = response.access_token;
    state.currentUserID = userID;
    state.currentUsername = username.trim().toLowerCase();
    sessionStorage.setItem(sessionTokenKey, state.token);
    sessionStorage.setItem(sessionUsernameKey, state.currentUsername);
    loginForm.reset();
    resetConversation();
    showChatView();
    await loadUsers();
  } catch (error) {
    showError(error.message);
  }
});

messageForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!state.threadID) {
    return;
  }
  sendButton.disabled = true;
  showError("");
  try {
    await apiRequest(`/threads/${state.threadID}/messages`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        client_msg_id: crypto.randomUUID(),
        content: contentInput.value,
      }),
    });
    contentInput.value = "";
    await loadMessages();
  } catch (error) {
    showError(error.message);
  } finally {
    sendButton.disabled = !state.threadID;
  }
});

logoutButton.addEventListener("click", () => {
  clearSession();
  showNotice("Đã đăng xuất.");
  showError("");
});

refreshButton.addEventListener("click", loadMessages);

window.addEventListener("focus", async () => {
  if (!state.token) {
    return;
  }
  try {
    await loadUsers();
    await loadMessages();
  } catch (error) {
    showError(error.message);
  }
});

setInterval(loadMessages, 1500);

state.token = sessionStorage.getItem(sessionTokenKey) || "";
state.currentUserID = decodeSubject(state.token);
state.currentUsername = sessionStorage.getItem(sessionUsernameKey) || "";
if (state.token && state.currentUserID) {
  showChatView();
  loadUsers().catch((error) => showError(error.message));
} else {
  clearSession();
}
