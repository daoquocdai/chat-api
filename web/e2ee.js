/* Account vaults and immutable epoch backups are recovered from the API on every login. */
(() => {
  "use strict";

  const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  const uuid = (value) => typeof value === "string" && uuidPattern.test(value) &&
    value !== "00000000-0000-0000-0000-000000000000";
  const normalizeUsername = (value) => String(value).trim().toLowerCase();
  const validKey = (value) => {
    try { return typeof value === "string" && atob(value).length === 32 && btoa(atob(value)) === value; }
    catch (_) { return false; }
  };
  function assert(condition, message) {
    if (!condition) throw new Error(message);
  }
  async function runtime() {
    assert(globalThis.isSecureContext && globalThis.crypto?.randomUUID && globalThis.WebAssembly,
      "Chat mã hóa cần kết nối HTTPS hoặc localhost và trình duyệt hỗ trợ WebAssembly.");
    return globalThis.MiniHermesWASM.load();
  }
  async function createAccount(username, password) {
    username = normalizeUsername(username);
    const crypto = await runtime();
    const result = crypto.call("createAccount", { username, password });
    return { username, auth_credential: result.auth_credential, kdf: result.kdf,
      public_bundle: result.public_bundle, account_vault: result.encrypted_vault, vault_key: result.vault_key };
  }
  async function deriveCredentials(username, password, kdf) {
    const crypto = await runtime();
    return crypto.call("deriveCredentials", { username: normalizeUsername(username), password, kdf });
  }

  class Client {
    constructor({ apiURL, userID, username, vaultKey, request, onChange, current }) {
      assert(uuid(userID) && validKey(vaultKey), "Thông tin khôi phục tài khoản không hợp lệ.");
      this.userID = userID;
      this.username = normalizeUsername(username);
      this.vaultKey = vaultKey;
      this.request = request;
      this.onChange = onChange || (() => {});
      this.current = current || (() => true);
      this.status = "initializing";
      this.error = "";
      this.closed = false;
      this.account = null;
      this.fingerprint = "";
      this.crypto = null;
      this.outbox = globalThis.MiniHermesE2EEStorage.outbox(apiURL, userID);
      this.epochs = new Map();
      this.records = new Map();
      this.listRequests = new Map();
      this.epochRequests = new Map();
      this.currentRequests = new Map();
      this.posts = new Map();
      this.peerPins = new Map();
    }
    _live() { return !this.closed && this.current(); }
    _check() { assert(this._live(), "Phiên đăng nhập đã kết thúc."); }
    _notify() { if (this._live()) this.onChange(); }
    async _request(path, options) {
      this._check();
      const response = await this.request(path, options);
      this._check();
      return response;
    }
    _crypto(method, input) {
      this._check();
      assert(this.crypto, "Bộ mã hóa chưa sẵn sàng.");
      return this.crypto.call(method, input);
    }
    snapshot() {
      return { status: this.status, error: this.error, own_fingerprint: this.fingerprint,
        peer_pins: Object.fromEntries(this.peerPins),
        pending: this.outbox.values().map((item) => ({ message_id: item.message_id, thread_id: item.context?.thread_id })) };
    }
    canRead() { return this._live() && this.status === "ready" && Boolean(this.account); }
    _threadValid(thread) {
      return thread?.kind === "direct" && uuid(thread.id) && uuid(thread.peer?.id) && thread.peer.id !== this.userID;
    }
    canSend(thread) { return this.canRead() && this._threadValid(thread); }
    async start() {
      try {
        this.crypto = await runtime();
        this._check();
        const account = await this._request("/e2ee/account");
        assert(account.user_id === this.userID && normalizeUsername(account.username) === this.username,
          "Bản sao khóa không thuộc tài khoản đang đăng nhập.");
        const opened = this._crypto("openAccount", { username: account.username, kdf: account.kdf,
          vault_key: this.vaultKey, public_bundle: account.public_bundle, encrypted_vault: account.account_vault });
        this.account = opened.account;
        this.fingerprint = opened.fingerprint;
        this.status = "ready";
        this.error = "";
        this._notify();
      } catch (error) {
        if (this._live()) { this.status = "error"; this.error = error.message; this._notify(); }
        throw error;
      }
    }
    _pin(peerID, publicKey, fingerprint) {
      const previous = this.peerPins.get(peerID);
      assert(!previous || (previous.identity_public_key === publicKey && previous.fingerprint === fingerprint),
        "Khóa của người kia đã thay đổi. Hãy đối chiếu fingerprint trước khi tiếp tục.");
      this.peerPins.set(peerID, { identity_public_key: publicKey, fingerprint });
    }
    _header(thread, record) {
      assert(this._threadValid(thread) && record?.thread_id === thread.id && uuid(record.epoch_id) &&
        typeof record.bootstrap === "string", "Thông tin phiên khóa không hợp lệ.");
      const header = JSON.parse(record.bootstrap);
      assert(header.version === 2 && header.thread_id === thread.id && header.epoch_id === record.epoch_id &&
        header.sender_id === record.sender_id && header.recipient_id === record.recipient_id &&
        ((header.sender_id === this.userID && header.recipient_id === thread.peer.id) ||
         (header.recipient_id === this.userID && header.sender_id === thread.peer.id)),
      "Phiên khóa không khớp hai thành viên cuộc trò chuyện.");
      const ownKey = header.sender_id === this.userID ? header.sender_identity_key : header.recipient_identity_key;
      assert(ownKey === this.account.identity.public_key, "Identity của phiên khóa không khớp tài khoản đã khôi phục.");
      return header;
    }
    async _list(thread) {
      if (this.listRequests.has(thread.id)) return this.listRequests.get(thread.id);
      const request = this._request("/threads/" + thread.id + "/epochs").then((response) => {
        assert(Array.isArray(response.epochs) &&
          (response.current_epoch_id === null || uuid(response.current_epoch_id)), "Danh sách phiên khóa không hợp lệ.");
        for (const record of response.epochs) {
          this._header(thread, record);
          const previous = this.records.get(record.epoch_id);
          assert(!previous || previous.bootstrap === record.bootstrap, "Nội dung phiên khóa đã thay đổi.");
          this.records.set(record.epoch_id, record);
        }
        assert(response.current_epoch_id === null ||
          response.epochs.some((epoch) => epoch.epoch_id === response.current_epoch_id),
        "Không tìm thấy phiên khóa hiện tại.");
        return response;
      }).finally(() => this.listRequests.delete(thread.id));
      this.listRequests.set(thread.id, request);
      return request;
    }
    async _restore(thread, record, proposedKey) {
      const header = this._header(thread, record);
      const cached = this.epochs.get(record.epoch_id);
      if (cached) {
        assert(cached.bootstrap === record.bootstrap, "Phiên khóa đã thay đổi.");
        return cached;
      }
      if (this.epochRequests.has(record.epoch_id)) return this.epochRequests.get(record.epoch_id);
      const request = (async () => {
        let opened;
        if (record.key_backup) {
          opened = this._crypto("decryptEpochBackup", { vault_key: this.vaultKey, owner_id: this.userID,
            header: record.bootstrap, backup: record.key_backup });
        } else {
          assert(header.recipient_id === this.userID, "Phiên đã gửi chưa có bản sao khóa trên server.");
          assert(header.signed_prekey_id === this.account.signed_prekey.key_id, "Thiếu signed prekey cho phiên khóa.");
          const opk = header.one_time_prekey_id === null ? null :
            this.account.one_time_prekeys.find((item) => item.key_id === header.one_time_prekey_id)?.key_pair;
          assert(header.one_time_prekey_id === null || opk, "Thiếu private prekey để khôi phục phiên.");
          const context = { thread_id: thread.id, epoch_id: record.epoch_id,
            sender_id: header.sender_id, recipient_id: header.recipient_id };
          const derived = this._crypto("openEpoch", { context, identity: this.account.identity,
            signed_prekey: this.account.signed_prekey.key_pair, one_time_prekey: opk, header: record.bootstrap });
          const encrypted = this._crypto("encryptEpochBackup", { vault_key: this.vaultKey,
            owner_id: this.userID, header: record.bootstrap, session_key: derived.session_key });
          const saved = await this._request("/threads/" + thread.id + "/epochs/" + record.epoch_id + "/key", {
            method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ key_backup: encrypted.backup }) });
          assert(saved.key_backup, "Server chưa xác nhận lưu bản sao khóa phiên.");
          opened = this._crypto("decryptEpochBackup", { vault_key: this.vaultKey, owner_id: this.userID,
            header: record.bootstrap, backup: saved.key_backup });
          assert(opened.session_key === derived.session_key, "Bản sao khóa phiên không khớp khóa vừa xác minh.");
          record = { ...record, key_backup: saved.key_backup };
          this.records.set(record.epoch_id, record);
        }
        assert(validKey(opened.session_key) && (!proposedKey || opened.session_key === proposedKey),
          "Khóa phiên không khớp bản sao trên server.");
        const incoming = header.recipient_id === this.userID;
        this._pin(thread.peer.id, incoming ? header.sender_identity_key : header.recipient_identity_key,
          incoming ? opened.sender_fingerprint : opened.recipient_fingerprint);
        const epoch = { epoch_id: record.epoch_id, bootstrap: record.bootstrap, session_key: opened.session_key };
        this.epochs.set(record.epoch_id, epoch);
        this._notify();
        return epoch;
      })().finally(() => this.epochRequests.delete(record.epoch_id));
      this.epochRequests.set(record.epoch_id, request);
      return request;
    }
    async _epoch(thread, epochID) {
      if (!this.records.has(epochID)) await this._list(thread);
      const record = this.records.get(epochID);
      assert(record?.thread_id === thread.id, "Không tìm thấy phiên khóa của tin nhắn.");
      return this._restore(thread, record);
    }
    async _createEpoch(thread) {
      const epochID = crypto.randomUUID();
      const context = { thread_id: thread.id, epoch_id: epochID, sender_id: this.userID, recipient_id: thread.peer.id };
      const bundle = await this._request("/e2ee/bundles/" + thread.peer.id + "/claim", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ thread_id: thread.id }) });
      assert(bundle.user_id === thread.peer.id, "Public bundle không thuộc người nhận.");
      const created = this._crypto("createEpoch", { context, identity: this.account.identity, bundle });
      this._pin(thread.peer.id, bundle.identity_public_key, created.recipient_fingerprint);
      const backup = this._crypto("encryptEpochBackup", { vault_key: this.vaultKey, owner_id: this.userID,
        header: created.header, session_key: created.session_key });
      let record;
      try {
        record = await this._request("/threads/" + thread.id + "/epochs", {
          method: "POST", headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ epoch_id: epochID,
            bootstrap: created.header, key_backup: backup.backup }) });
        assert(record.epoch_id === epochID && record.bootstrap === created.header && record.key_backup,
          "Server chưa xác nhận phiên đề xuất và bản sao khóa.");
        this.records.set(record.epoch_id, record);
        return this._restore(thread, record, created.session_key);
      } catch (error) {
        if (error.status === 409 && error.details?.epoch) {
          // Never use the losing proposal's SK; restore the server's committed winner.
          record = error.details.epoch;
        } else if (error.status === undefined || error.status >= 500) {
          const current = await this._list(thread);
          record = current.epochs.find((epoch) => epoch.epoch_id === current.current_epoch_id);
          if (!record) throw error;
        } else throw error;
        this._header(thread, record);
        this.records.set(record.epoch_id, record);
        return this._restore(thread, record);
      }
    }
    async _current(thread) {
      if (this.currentRequests.has(thread.id)) return this.currentRequests.get(thread.id);
      const request = (async () => {
        const list = await this._list(thread);
        if (list.current_epoch_id === null) return this._createEpoch(thread);
        return this._restore(thread, this.records.get(list.current_epoch_id));
      })().finally(() => this.currentRequests.delete(thread.id));
      this.currentRequests.set(thread.id, request);
      return request;
    }
    _pending(item) {
      const context = item?.context;
      assert(uuid(item?.message_id) && item.message_id === context?.message_id &&
        uuid(context.thread_id) && uuid(context.epoch_id) && context.sender_id === this.userID &&
        uuid(context.recipient_id) && context.recipient_id !== this.userID &&
        typeof item.content === "string" && item.body === JSON.stringify({
          message_id: item.message_id, content_format: "e2ee_v2", content: item.content }),
      "Bản gửi lại trong bộ nhớ không hợp lệ.");
      return item;
    }
    async _post(item) {
      this._pending(item);
      if (this.posts.has(item.message_id)) return this.posts.get(item.message_id);
      const request = this._request("/threads/" + item.context.thread_id + "/messages", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: item.body }).then((message) => {
        assert((message.id || message.message_id) === item.message_id &&
          message.thread_id === item.context.thread_id && message.sender_id === this.userID &&
          message.kind === "text" && message.content_format === "e2ee_v2" && message.content === item.content &&
          Number.isSafeInteger(Number(message.seq)) && Number(message.seq) > 0,
        "Response không khớp tin đang chờ; giữ bản gửi lại.");
        this.outbox.remove(item.message_id);
        this._notify();
        return message;
      }).finally(() => this.posts.delete(item.message_id));
      this.posts.set(item.message_id, request);
      return request;
    }
    async send(thread, plaintext) {
      assert(this.canSend(thread), "Chat mã hóa chưa sẵn sàng.");
      const text = String(plaintext).trim();
      assert([...text].length > 0 && [...text].length <= 1000 && !text.includes("\0"),
        "Tin nhắn cần 1–1000 ký tự và không chứa NUL.");
      // Clicking Send again with an unconfirmed draft retries its exact UUID/envelope.
      for (const pending of this.outbox.values().filter((item) => item.context?.thread_id === thread.id)) {
        this._pending(pending);
        const epoch = await this._epoch(thread, pending.context.epoch_id);
        const opened = this._crypto("openMessage", { context: pending.context,
          session_key: epoch.session_key, content: pending.content });
        if (opened.plaintext === text) return this._post(pending);
      }
      const epoch = await this._current(thread);
      const context = { thread_id: thread.id, epoch_id: epoch.epoch_id, message_id: crypto.randomUUID(),
        sender_id: this.userID, recipient_id: thread.peer.id };
      const sealed = this._crypto("sealMessage", { context, session_key: epoch.session_key, plaintext: text });
      const item = { message_id: context.message_id, context, content: sealed.content,
        body: JSON.stringify({ message_id: context.message_id, content_format: "e2ee_v2", content: sealed.content }) };
      this.outbox.put(item);
      this._notify();
      return this._post(item);
    }
    async retry(messageID) {
      assert(this.canRead(), "Tài khoản chưa được mở khóa.");
      const item = this.outbox.get(messageID);
      assert(item, "Không có tin đang chờ gửi lại.");
      this._pending(item);
      const thread = { id: item.context.thread_id, kind: "direct", peer: { id: item.context.recipient_id } };
      const epoch = await this._epoch(thread, item.context.epoch_id);
      this._crypto("openMessage", { context: item.context, session_key: epoch.session_key, content: item.content });
      return this._post(item);
    }
    async receive(thread, message) {
      try {
        assert(this.canRead() && this._threadValid(thread), "Đang khôi phục khóa tài khoản.");
        const id = message.id || message.message_id;
        assert(uuid(id) && message.thread_id === thread.id && message.kind === "text" &&
          message.content_format === "e2ee_v2" && [this.userID, thread.peer.id].includes(message.sender_id),
        "Tin nhắn không thuộc cuộc trò chuyện mã hóa này.");
        const envelope = JSON.parse(message.content);
        assert(envelope.version === 2 && uuid(envelope.epoch_id), "Envelope tin nhắn không hợp lệ.");
        const context = { thread_id: thread.id, epoch_id: envelope.epoch_id, message_id: id,
          sender_id: message.sender_id, recipient_id: message.sender_id === this.userID ? thread.peer.id : this.userID };
        // message.recipient_id is the WS delivery target, not the cryptographic recipient.
        const epoch = await this._epoch(thread, envelope.epoch_id);
        const opened = this._crypto("openMessage", { context, session_key: epoch.session_key, content: message.content });
        const pending = this.outbox.get(id);
        if (pending) {
          assert(pending.content === message.content && pending.context.thread_id === thread.id &&
            message.sender_id === this.userID, "Lịch sử khác bản tin đang chờ; giữ bản gửi lại.");
          this.outbox.remove(id);
          this._notify();
        }
        return { status: "ready", plaintext: opened.plaintext };
      } catch (error) {
        return { status: "error", error: error.message };
      }
    }
    close() {
      this.closed = true;
      this.account = null;
      this.vaultKey = "";
      this.epochs.clear();
      this.records.clear();
      this.outbox.close();
      return Promise.resolve();
    }
  }

  globalThis.MiniHermesE2EEClient = Object.freeze({
    create: (options) => new Client(options), createAccount, deriveCredentials, normalizeUsername, validKey,
  });
})();
