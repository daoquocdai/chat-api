const userForm = document.querySelector("#user-form");
const usernameInput = document.querySelector("#username");
const currentUserSelect = document.querySelector("#current-user");
const peerUserSelect = document.querySelector("#peer-user");
const historyElement = document.querySelector("#history");
const messageForm = document.querySelector("#message-form");
const contentInput = document.querySelector("#content");
const sendButton = document.querySelector("#send-button");
const errorElement = document.querySelector("#error");

const state = {
  users: [],
  currentUserID: "",
  peerUserID: "",
  loadingMessages: false,
  conversationVersion: 0,
};

function showError(message) {
  errorElement.textContent = message;
  errorElement.hidden = !message;
}

async function readResponse(response) {
  const body = await response.json().catch(() => ({}));
  if (!response.ok) {
    throw new Error(body.error || "Yêu cầu không thành công.");
  }
  return body;
}

function addOption(select, value, label) {
  const option = document.createElement("option");
  option.value = value;
  option.textContent = label;
  select.append(option);
}

function renderUserSelects() {
  const currentExists = state.users.some((user) => user.id === state.currentUserID);
  if (!currentExists) {
    state.currentUserID = "";
  }

  const peerExists = state.users.some(
    (user) => user.id === state.peerUserID && user.id !== state.currentUserID,
  );
  if (!peerExists) {
    state.peerUserID = "";
  }

  currentUserSelect.replaceChildren();
  addOption(currentUserSelect, "", "-- Chọn người dùng --");
  for (const user of state.users) {
    addOption(currentUserSelect, user.id, user.username);
  }
  currentUserSelect.value = state.currentUserID;

  peerUserSelect.replaceChildren();
  addOption(peerUserSelect, "", "-- Chọn người nhận --");
  for (const user of state.users) {
    if (user.id !== state.currentUserID) {
      addOption(peerUserSelect, user.id, user.username);
    }
  }
  peerUserSelect.value = state.peerUserID;
  peerUserSelect.disabled = !state.currentUserID;

  const ready = Boolean(state.currentUserID && state.peerUserID);
  contentInput.disabled = !ready;
  sendButton.disabled = !ready;
}

async function loadUsers() {
  try {
    const response = await fetch("/users");
    state.users = await readResponse(response);
    renderUserSelects();
  } catch (error) {
    showError(error.message);
  }
}

function userName(id) {
  return state.users.find((user) => user.id === id)?.username || "unknown";
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
    const isOwn = message.sender_id === state.currentUserID;
    item.className = `message ${isOwn ? "sent" : "received"}`;

    const content = document.createElement("p");
    content.textContent = message.content;

    const meta = document.createElement("small");
    const time = new Date(message.created_at).toLocaleTimeString("vi-VN", {
      hour: "2-digit",
      minute: "2-digit",
    });
    meta.textContent = `${userName(message.sender_id)} · ${time}`;

    item.append(content, meta);
    historyElement.append(item);
  }

  historyElement.scrollTop = historyElement.scrollHeight;
}

function showConversationPlaceholder() {
  historyElement.replaceChildren();
  const empty = document.createElement("p");
  empty.className = "empty";
  empty.textContent = state.currentUserID
    ? "Hãy chọn người nhận."
    : "Hãy chọn hai người dùng để bắt đầu.";
  historyElement.append(empty);
}

async function loadMessages() {
  if (!state.currentUserID || !state.peerUserID || state.loadingMessages) {
    return;
  }

  const currentUserID = state.currentUserID;
  const peerUserID = state.peerUserID;
  const version = state.conversationVersion;
  state.loadingMessages = true;

  try {
    const query = new URLSearchParams({
      user_id: currentUserID,
      peer_id: peerUserID,
    });
    const response = await fetch(`/messages?${query}`);
    const messages = await readResponse(response);

    if (
      version !== state.conversationVersion ||
      currentUserID !== state.currentUserID ||
      peerUserID !== state.peerUserID
    ) {
      return;
    }

    renderMessages(messages);
    showError("");
  } catch (error) {
    showError(error.message);
  } finally {
    state.loadingMessages = false;
  }
}

userForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  showError("");

  try {
    const response = await fetch("/users", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username: usernameInput.value }),
    });
    const user = await readResponse(response);
    usernameInput.value = "";
    state.currentUserID = user.id;
    state.peerUserID = "";
    state.conversationVersion += 1;
    await loadUsers();
    showConversationPlaceholder();
  } catch (error) {
    showError(error.message);
  }
});

currentUserSelect.addEventListener("change", () => {
  state.currentUserID = currentUserSelect.value;
  if (state.peerUserID === state.currentUserID) {
    state.peerUserID = "";
  }
  state.conversationVersion += 1;
  renderUserSelects();
  showConversationPlaceholder();
  loadMessages();
});

peerUserSelect.addEventListener("change", () => {
  state.peerUserID = peerUserSelect.value;
  state.conversationVersion += 1;
  renderUserSelects();
  showConversationPlaceholder();
  loadMessages();
});

messageForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!state.currentUserID || !state.peerUserID) {
    return;
  }

  sendButton.disabled = true;
  showError("");

  try {
    const response = await fetch("/messages", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        sender_id: state.currentUserID,
        receiver_id: state.peerUserID,
        content: contentInput.value,
      }),
    });
    await readResponse(response);
    contentInput.value = "";
    await loadMessages();
  } catch (error) {
    showError(error.message);
  } finally {
    sendButton.disabled = !(state.currentUserID && state.peerUserID);
  }
});

window.addEventListener("focus", loadUsers);
setInterval(loadMessages, 1000);
loadUsers();
