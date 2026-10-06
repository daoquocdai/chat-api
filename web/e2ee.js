/* Crypto stays in the Go/WASM bridge. This controller owns durable web state. */
(() => {
  "use strict";

  const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
  const hashPattern = /^[0-9a-f]{64}$/;
  const batchSize = 20;
  const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
  const clone = (value) => structuredClone(value);
  const object = (value) => value !== null && typeof value === "object" && !Array.isArray(value);
  const fields = (value, names) => object(value) && Object.keys(value).length === names.length && names.every((name) => own(value, name));
  const uuid = (value) => typeof value === "string" && uuidPattern.test(value) &&
    value !== "00000000-0000-0000-0000-000000000000";
  const positiveID = (value) => Number.isSafeInteger(value) && value > 0;
  const watermark = (value) => Number.isSafeInteger(value) && value >= 0;
  const hash = (value) => typeof value === "string" && hashPattern.test(value);

  function failure(code, message) {
    const error = new Error(message);
    error.code = code;
    return error;
  }
  function assert(condition, code = "missing_keys", message = "State E2EE bị thiếu hoặc không hợp lệ; không tự thay bộ khóa.") {
    if (!condition) throw failure(code, message);
  }
  function base64(value, length) {
    if (typeof value !== "string") return false;
    try {
      const bytes = atob(value);
      return bytes.length === length && btoa(bytes) === value;
    } catch (_) { return false; }
  }
  function sameContext(left, right) {
    return object(left) && object(right) &&
      ["thread_id", "message_id", "sender_id", "recipient_id"].every((name) => left[name] === right[name]);
  }
  function validContext(context) {
    return object(context) && Object.keys(context).length === 4 &&
      ["thread_id", "message_id", "sender_id", "recipient_id"].every((name) => uuid(context[name])) &&
      context.sender_id !== context.recipient_id;
  }
  function safeError(error) {
    if (error?.code === "stale_session") return "Phiên E2EE đã kết thúc.";
    if (error?.code && typeof error.message === "string") return error.message;
    if (error?.status === 409) return "API báo xung đột; giữ bộ khóa và payload đang chờ để đối chiếu.";
    if (error?.status === 403) return "Không có quyền thực hiện thao tác E2EE trong cuộc trò chuyện này.";
    if (error?.status === 404) return "Chưa tìm thấy thread hoặc public bundle của người nhận.";
    if (error?.status === 400 || error?.status === 413) return "API từ chối payload E2EE; giữ payload đang chờ.";
    return "Thao tác E2EE chưa được xác nhận. State đã lưu được giữ để thử lại.";
  }
  function validatedText(value) {
    assert(typeof value === "string", "invalid_input", "Nội dung tin nhắn không hợp lệ.");
    const text = value.trim();
    const runes = [...text];
    assert(runes.length > 0 && runes.length <= 1000 && !text.includes("\0") &&
      !runes.some((rune) => rune.length === 1 && rune.charCodeAt(0) >= 0xd800 && rune.charCodeAt(0) <= 0xdfff),
    "invalid_input", "Tin nhắn cần 1–1000 ký tự Unicode hợp lệ và không chứa NUL.");
    return text;
  }

  class Client {
    constructor({ apiURL, userID, request, onChange, current }) {
      this.apiURL = globalThis.MiniHermesE2EEStorage.normalizeAPIURL(apiURL);
      assert(uuid(userID), "invalid_context", "UUID tài khoản không hợp lệ.");
      this.userID = userID;
      this.request = request;
      this.onChange = onChange || (() => {});
      this.current = current || (() => true);
      this.status = "initializing";
      this.error = "";
      this.busy = false;
      this.owner = false;
      this.profile = null;
      this.runtime = null;
      this.store = null;
      this.queue = Promise.resolve();
      this.starting = null;
      this.lockRequest = null;
      this.releaseLock = null;
      this.closed = false;
      this.closing = false;
      this.loadFailed = false;
      this.profileLoaded = false;
    }

    snapshot() {
      const profile = this.profile;
      return {
        status: this.status, error: this.error, owner: this.owner, busy: this.busy,
        own_fingerprint: profile?.identity_fingerprint || "",
        registration: Boolean(profile?.registration?.confirmed),
        last_prekey_id: profile?.last_prekey_id || 0,
        opk_count: profile?.registration?.one_time_prekey_count ?? null,
        local_opk_count: profile ? Object.keys(profile.one_time_prekeys).length : 0,
        pending_upload: Boolean(profile?.pending_upload),
        pending_send: profile?.pending_send ? {
          message_id: profile.pending_send.message_id, thread_id: profile.pending_send.thread_id,
          in_flight: this.busy,
        } : null,
        peer_pins: clone(profile?.peer_pins || {}),
        can_initialize: this.owner && Boolean(this.runtime && this.store) && this.profileLoaded && !profile && !this.loadFailed,
      };
    }

    canRead() {
      return this._live() && this.owner && Boolean(this.runtime && this.store && this.profile?.registration?.confirmed) &&
        !this.loadFailed && globalThis.MiniHermesWASM.status === "ready";
    }
    canSend(thread) {
      return this.canRead() && this.status === "ready" && !this.busy && !this.profile.pending_send &&
        !this.profile.pending_upload && this._threadValid(thread);
    }
    _threadValid(thread) {
      return object(thread) && thread.kind === "direct" && thread.encryption_mode === "e2ee" &&
        uuid(thread.id) && uuid(thread.peer?.id) && thread.peer.id !== this.userID;
    }
    _live() { return !this.closing && !this.closed && this.current(); }
    _assertLive() {
      assert(this._live(), "stale_session", "Phiên E2EE đã kết thúc.");
    }
    _assertOwner() {
      this._assertLive();
      assert(this.owner && this.runtime && this.store, "not_ready", "Tab chưa sở hữu state E2EE hoặc WASM chưa sẵn sàng.");
    }
    _notify() { if (this._live()) this.onChange(); }
    _status() {
      if (this.loadFailed) this.status = "missing_keys";
      else if (this.owner && this.profile?.registration?.confirmed) this.status = "ready";
      else if (this.owner) this.status = "missing_keys";
    }
    async _crypto(method, input) {
      this._assertOwner();
      const result = await this.runtime.call(method, input);
      this._assertLive();
      return result;
    }
    _task(work, { message = false } = {}) {
      const result = this.queue.then(async () => {
        this._assertOwner();
        this.busy = true;
        if (!message) { this.error = ""; this._status(); }
        this._notify();
        try { return await work(); }
        catch (error) {
          if (!message && this._live()) {
            this.error = safeError(error);
            this.status = "error";
          }
          throw error;
        } finally {
          this.busy = false;
          this._notify();
        }
      });
      // Failure of one operation must not poison the serial state queue.
      this.queue = result.catch(() => {});
      return result;
    }
    async _save(next) {
      this._assertOwner();
      // All crypto/network work is complete before opening this transaction.
      await this.store.save(next);
      this._assertLive();
      // Update RAM only after transaction.oncomplete; abort keeps the old state.
      this.profile = next;
      this._status();
      this._notify();
    }

    start() {
      if (this.starting) return this.starting;
      this._assertLive();
      this.status = "initializing";
      this.error = "";
      this._notify();
      let settle;
      this.starting = new Promise((resolve) => { settle = resolve; });
      if (!globalThis.isSecureContext || !globalThis.navigator?.locks || !globalThis.indexedDB ||
          !globalThis.WebAssembly || !globalThis.crypto?.randomUUID) {
        this.status = "error";
        this.error = "E2EE cần secure context, Web Locks, IndexedDB, WebAssembly và UUID của browser; không có chế độ thay thế.";
        settle();
        this._notify();
        return this.starting;
      }
      const lockName = "mini-hermes:e2ee:" + JSON.stringify([this.apiURL, this.userID]);
      this.lockRequest = navigator.locks.request(lockName, { mode: "exclusive", ifAvailable: true }, async (lock) => {
        if (!lock) {
          this.status = "other_tab";
          this.error = "E2EE đang dùng ở tab khác. Đóng hoặc đăng xuất tab sở hữu rồi thử tiếp quản.";
          settle(); this._notify(); return;
        }
        this.owner = true;
        const held = new Promise((resolve) => { this.releaseLock = resolve; });
        try {
          this._assertLive();
          this.runtime = await globalThis.MiniHermesWASM.load();
          this._assertLive();
          this.store = await globalThis.MiniHermesE2EEStorage.open();
          this._assertLive();
          const profile = await this.store.load(this.apiURL, this.userID);
          this._assertLive();
          if (profile !== null) {
            try { await this._validateProfile(profile); }
            catch (error) { this.loadFailed = true; throw error; }
            this.profile = profile;
          }
          // Bootstrap must finish before initialize can observe an empty scope.
          // A loaded profile stays protected throughout async WASM validation.
          this.profileLoaded = true;
          this._status();
          if (!this.profile) this.error = "Không có khóa local. Chỉ khởi tạo nếu đây là tài khoản chưa đăng ký; mất IndexedDB không thể khôi phục khóa cũ.";
          else if (!this.profile.registration.confirmed) this.error = "Bộ khóa đã lưu; cần xác nhận đăng ký public bundle.";
        } catch (error) {
          if (this._live()) {
            this.status = this.loadFailed ? "missing_keys" : "error";
            this.error = safeError(error);
          }
        } finally { settle(); this._notify(); }
        if (this.closing || this.closed || !this.current()) this.releaseLock();
        await held;
        await this.queue;
        this.store?.close();
        this.store = null;
        this.profile = null;
        this.runtime = null;
        this.owner = false;
        this.releaseLock = null;
      }).catch((error) => {
        if (this._live()) { this.status = "error"; this.error = safeError(error); this._notify(); }
        settle();
      });
      return this.starting;
    }
    takeOver() {
      this._assertLive();
      assert(!this.owner && !this.busy, "not_ready", "Tab đã sở hữu E2EE hoặc đang xử lý state.");
      this.starting = null;
      return this.start();
    }
    async close() {
      if (this.closed) return;
      this.closing = true;
      // In-flight local transactions settle before the ownership callback exits.
      await this.starting;
      await this.queue;
      this.releaseLock?.();
      await this.lockRequest;
      this.profile = null;
      this.runtime = null;
      this.closed = true;
    }

    _publicBundle(profile) {
      return {
        user_id: this.userID, identity_public_key: profile.identity.public_key,
        signed_prekey: { key_id: profile.signed_prekey.key_id,
          public_key: profile.signed_prekey.key_pair.public_key, signature: profile.signed_prekey.signature },
        one_time_prekey: null,
      };
    }
    async _validatePair(pair) {
      assert(object(pair) && base64(pair.private_key, 32) && base64(pair.public_key, 32) && Object.keys(pair).length === 2);
      const restored = await this._crypto("generateKeyPair", { private_key: pair.private_key });
      assert(restored.key_pair.public_key === pair.public_key && restored.key_pair.private_key === pair.private_key);
      return restored.fingerprint;
    }
    async _validateProfile(profile) {
      assert(object(profile) && profile.version === 1 && profile.api_url === this.apiURL && profile.user_id === this.userID);
      assert(Object.keys(profile).every((key) => ["version", "api_url", "user_id", "identity", "identity_fingerprint",
        "signed_prekey", "one_time_prekeys", "next_prekey_id", "registration", "last_prekey_id", "pending_upload",
        "peer_pins", "message_keys", "pending_send"].includes(key)));
      const fingerprint = await this._validatePair(profile.identity);
      assert(hash(profile.identity_fingerprint) && fingerprint === profile.identity_fingerprint);
      assert(fields(profile.signed_prekey, ["key_id", "key_pair", "signature"]) && profile.signed_prekey.key_id === 1 && base64(profile.signed_prekey.signature, 64));
      await this._validatePair(profile.signed_prekey.key_pair);
      const verified = await this._crypto("verifyBundle", { bundle: this._publicBundle(profile) });
      assert(verified.valid === true && verified.fingerprint === fingerprint);
      assert(object(profile.one_time_prekeys) && positiveID(profile.next_prekey_id) && profile.next_prekey_id >= 2);
      for (const [id, pair] of Object.entries(profile.one_time_prekeys)) {
        assert(positiveID(Number(id)) && String(Number(id)) === id && Number(id) > 1 && Number(id) < profile.next_prekey_id);
        await this._validatePair(pair);
      }
      assert(fields(profile.registration, ["confirmed", "one_time_prekey_count"]) && typeof profile.registration.confirmed === "boolean" &&
        watermark(profile.registration.one_time_prekey_count) && watermark(profile.last_prekey_id) &&
        profile.last_prekey_id < profile.next_prekey_id && (!profile.registration.confirmed || profile.last_prekey_id >= 1));
      assert(profile.registration.one_time_prekey_count <= Math.max(0, profile.last_prekey_id - 1));
      if (!profile.registration.confirmed) {
        assert(profile.last_prekey_id === 0 && profile.registration.one_time_prekey_count === 0 &&
          profile.pending_upload !== null && profile.next_prekey_id === batchSize + 2 &&
          Object.keys(profile.one_time_prekeys).length === batchSize);
      }
      assert(object(profile.peer_pins) && object(profile.message_keys));
      for (const [id, pin] of Object.entries(profile.peer_pins)) {
        assert(uuid(id) && id !== this.userID && fields(pin, ["identity_public_key", "fingerprint"]) && base64(pin.identity_public_key, 32) && hash(pin.fingerprint));
      }
      for (const [id, cached] of Object.entries(profile.message_keys)) {
        assert(uuid(id) && fields(cached, ["message_key", "context", "content_sha256", "sender_identity_key", "recipient_identity_key"]) && validContext(cached.context) && cached.context.message_id === id &&
          [cached.context.sender_id, cached.context.recipient_id].includes(this.userID) && base64(cached.message_key, 32) &&
          base64(cached.sender_identity_key, 32) && base64(cached.recipient_identity_key, 32) && hash(cached.content_sha256));
        const incoming = cached.context.recipient_id === this.userID;
        const peerID = incoming ? cached.context.sender_id : cached.context.recipient_id;
        const peerKey = incoming ? cached.sender_identity_key : cached.recipient_identity_key;
        assert((incoming ? cached.recipient_identity_key : cached.sender_identity_key) === profile.identity.public_key &&
          profile.peer_pins[peerID]?.identity_public_key === peerKey);
      }
      assert(profile.pending_upload === null || object(profile.pending_upload));
      if (profile.pending_upload) this._validateUpload(profile);
      assert(profile.pending_send === null || object(profile.pending_send));
      if (profile.pending_send) {
        const pending = profile.pending_send;
        assert(fields(pending, ["thread_id", "message_id", "context", "content_format", "content", "body"]) &&
          validContext(pending.context) && pending.context.sender_id === this.userID &&
          pending.message_id === pending.context.message_id && pending.thread_id === pending.context.thread_id &&
          pending.content_format === "e2ee_v1" && typeof pending.content === "string" &&
          pending.body === JSON.stringify({ message_id: pending.message_id, content_format: "e2ee_v1", content: pending.content }));
        const cached = profile.message_keys[pending.message_id];
        assert(cached && sameContext(cached.context, pending.context));
        await this._crypto("decryptWithKey", { context: cached.context,
          sender_identity_key: cached.sender_identity_key, recipient_identity_key: cached.recipient_identity_key,
          message_key: cached.message_key, expected_content_sha256: cached.content_sha256, content: pending.content });
      }
    }
    _validateUpload(profile) {
      const pending = profile.pending_upload;
      const request = pending.request;
      assert(fields(pending, ["request", "body"]) && fields(request, ["identity_public_key", "signed_prekey", "one_time_prekeys"]) &&
        request.identity_public_key === profile.identity.public_key && fields(request.signed_prekey, ["key_id", "public_key", "signature"]) &&
        request.signed_prekey.key_id === profile.signed_prekey.key_id &&
        request.signed_prekey.public_key === profile.signed_prekey.key_pair.public_key &&
        request.signed_prekey.signature === profile.signed_prekey.signature && Array.isArray(request.one_time_prekeys) &&
        request.one_time_prekeys.length <= 100 && pending.body === JSON.stringify(request));
      const seen = new Set([1]);
      for (const prekey of request.one_time_prekeys) {
        assert(fields(prekey, ["key_id", "public_key"]) && positiveID(prekey.key_id) && !seen.has(prekey.key_id) &&
          prekey.key_id < profile.next_prekey_id && base64(prekey.public_key, 32));
        // An already uploaded refill OPK may have been consumed locally while its retry remains pending.
        if (profile.one_time_prekeys[prekey.key_id]) assert(profile.one_time_prekeys[prekey.key_id].public_key === prekey.public_key);
        else assert(profile.registration.confirmed);
        seen.add(prekey.key_id);
      }
    }
    _uploadPayload(profile, ids) {
      const bundle = this._publicBundle(profile);
      const request = { identity_public_key: bundle.identity_public_key, signed_prekey: bundle.signed_prekey,
        one_time_prekeys: ids.map((id) => ({ key_id: id, public_key: profile.one_time_prekeys[id].public_key })) };
      return { request, body: JSON.stringify(request) };
    }
    async _newBatch(next) {
      const ids = [];
      assert(positiveID(next.next_prekey_id) && next.next_prekey_id <= Number.MAX_SAFE_INTEGER - batchSize,
        "invalid_input", "Mốc prekey ID vượt miền số nguyên an toàn của browser.");
      for (let index = 0; index < batchSize; index += 1) {
        const id = next.next_prekey_id++;
        const generated = await this._crypto("generateKeyPair", {});
        next.one_time_prekeys[id] = generated.key_pair;
        ids.push(id);
      }
      next.pending_upload = this._uploadPayload(next, ids);
    }
    initialize() {
      return this._task(async () => {
        assert(this.profileLoaded && !this.profile && !this.loadFailed, "missing_keys", "Không được reset hoặc thay bộ khóa đã có, hoặc khởi tạo khi đọc state chưa thành công.");
        const identity = await this._crypto("generateKeyPair", {});
        const signed = await this._crypto("generateKeyPair", {});
        const signature = await this._crypto("signPrekey", {
          identity_private_key: identity.key_pair.private_key, signed_prekey_public_key: signed.key_pair.public_key,
        });
        const next = {
          version: 1, api_url: this.apiURL, user_id: this.userID,
          identity: identity.key_pair, identity_fingerprint: identity.fingerprint,
          signed_prekey: { key_id: 1, key_pair: signed.key_pair, signature: signature.signature },
          one_time_prekeys: {}, next_prekey_id: 2,
          registration: { confirmed: false, one_time_prekey_count: 0 }, last_prekey_id: 0,
          pending_upload: null, peer_pins: {}, message_keys: {}, pending_send: null,
        };
        await this._newBatch(next);
        await this._save(next);
      });
    }
    upload() { return this._task(() => this._upload()); }
    async _upload() {
      assert(this.profile?.pending_upload, "not_ready", "Không có public bundle đang chờ đăng ký.");
      const pending = this.profile.pending_upload;
      const response = await this.request("/e2ee/prekeys", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: pending.body,
      });
      this._assertLive();
      const highest = Math.max(1, ...pending.request.one_time_prekeys.map((key) => key.key_id));
      assert(object(response) && response.user_id === this.userID && response.identity_public_key === this.profile.identity.public_key &&
        response.signed_prekey_id === this.profile.signed_prekey.key_id && watermark(response.last_prekey_id) &&
        response.last_prekey_id >= Math.max(highest, this.profile.last_prekey_id) && watermark(response.one_time_prekey_count) &&
        response.one_time_prekey_count <= response.last_prekey_id - 1,
      "invalid_response", "Response đăng ký không khớp bundle đã lưu; giữ pending upload.");
      const next = clone(this.profile);
      next.last_prekey_id = response.last_prekey_id;
      // A higher server watermark suggests another client: never reuse its IDs.
      assert(response.last_prekey_id < Number.MAX_SAFE_INTEGER,
        "invalid_response", "Mốc prekey server vượt miền an toàn; giữ pending upload.");
      next.next_prekey_id = Math.max(next.next_prekey_id, response.last_prekey_id + 1);
      next.registration = { confirmed: true, one_time_prekey_count: response.one_time_prekey_count };
      next.pending_upload = null;
      await this._save(next);
      return response;
    }
    refill() {
      return this._task(async () => {
        assert(this.profile?.registration.confirmed && !this.profile.pending_upload,
          "not_ready", "Cần xác nhận đăng ký và giải quyết pending upload trước khi bổ sung OPK.");
        const next = clone(this.profile);
        await this._newBatch(next);
        await this._save(next);
        return this._upload();
      });
    }

    _pin(profile, peerID, identityKey, fingerprint) {
      const previous = profile.peer_pins[peerID];
      assert(!previous || previous.identity_public_key === identityKey,
        "identity_changed", "Identity của peer đã thay đổi. Dừng E2EE và đối chiếu fingerprint; không ghi đè pin cũ.");
      if (fingerprint) assert(hash(fingerprint) && (!previous || previous.fingerprint === fingerprint),
        "identity_changed", "Fingerprint của peer không khớp pin đã lưu.");
    }
    send(thread, plaintext) {
      return this._task(async () => {
        assert(this.canRead() && !this.profile.pending_send && !this.profile.pending_upload && this._threadValid(thread),
          "not_ready", "E2EE chưa sẵn sàng hoặc còn payload chờ xác nhận.");
        const text = validatedText(plaintext);
        const context = { thread_id: thread.id, message_id: crypto.randomUUID(), sender_id: this.userID, recipient_id: thread.peer.id };
        const bundle = await this.request(`/e2ee/bundles/${thread.peer.id}/claim`, {
          method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ thread_id: thread.id }),
        });
        this._assertLive();
        assert(object(bundle) && bundle.user_id === thread.peer.id && positiveID(bundle.signed_prekey?.key_id) &&
          (bundle.one_time_prekey === null || positiveID(bundle.one_time_prekey?.key_id)),
        "invalid_bundle", "Bundle không khớp recipient hoặc ID không biểu diễn chính xác.");
        const verified = await this._crypto("verifyBundle", { bundle });
        this._pin(this.profile, thread.peer.id, bundle.identity_public_key, verified.fingerprint);
        const sealed = await this._crypto("seal", { context, identity: this.profile.identity, bundle, plaintext: text });
        assert(typeof sealed.content === "string" && base64(sealed.message_key, 32) && hash(sealed.content_sha256),
          "crypto_failed", "Kết quả crypto không hợp lệ; chưa gửi message.");
        const next = clone(this.profile);
        next.peer_pins[thread.peer.id] = { identity_public_key: bundle.identity_public_key, fingerprint: verified.fingerprint };
        next.message_keys[context.message_id] = { message_key: sealed.message_key, context,
          content_sha256: sealed.content_sha256, sender_identity_key: next.identity.public_key,
          recipient_identity_key: bundle.identity_public_key };
        const body = JSON.stringify({ message_id: context.message_id, content_format: "e2ee_v1", content: sealed.content });
        next.pending_send = { thread_id: thread.id, message_id: context.message_id, context,
          content_format: "e2ee_v1", content: sealed.content, body };
        await this._save(next);
        return this._postPending();
      });
    }
    retry() {
      return this._task(async () => {
        assert(this.canRead() && this.profile.pending_send, "not_ready", "Không có message E2EE đang chờ gửi lại.");
        return this._postPending();
      });
    }
    _matchesPending(message, pending) {
      return object(message) && (message.id || message.message_id) === pending.message_id &&
        message.thread_id === pending.thread_id && message.sender_id === this.userID &&
        message.kind === "text" && message.content_format === pending.content_format && message.content === pending.content &&
        positiveID(message.seq);
    }
    async _postPending() {
      const pending = this.profile.pending_send;
      const response = await this.request(`/threads/${pending.thread_id}/messages`, {
        method: "POST", headers: { "Content-Type": "application/json" }, body: pending.body,
      });
      this._assertLive();
      assert(this._matchesPending(response, pending), "invalid_response", "Response message không khớp payload đã lưu; giữ pending để đối chiếu.");
      const next = clone(this.profile);
      next.pending_send = null;
      await this._save(next);
      return response;
    }
    receive(thread, message) {
      if (!this.canRead() || !this._threadValid(thread)) return Promise.resolve({ status: "pending",
        error: this.status === "other_tab" ? "E2EE đang dùng ở tab khác." : "Đang chờ bộ khóa, WASM hoặc thông tin thread E2EE." });
      return this._task(async () => {
        this._assertLive();
        assert(this.canRead() && this._threadValid(thread), "not_ready", "State E2EE chưa sẵn sàng.");
        const id = message.id || message.message_id;
        assert(uuid(id) && message.thread_id === thread.id && message.content_format === "e2ee_v1" &&
          message.kind === "text" && positiveID(message.seq) && typeof message.content === "string" &&
          [this.userID, thread.peer.id].includes(message.sender_id),
        "invalid_context", "Metadata message không khớp direct thread E2EE.");
        const context = { thread_id: thread.id, message_id: id, sender_id: message.sender_id,
          recipient_id: message.sender_id === this.userID ? thread.peer.id : this.userID };
        assert(message.recipient_id === undefined || message.recipient_id === context.recipient_id,
          "invalid_context", "Recipient của event không khớp context message.");
        let header;
        try { header = JSON.parse(message.content); }
        catch (_) { throw failure("invalid_input", "Envelope message không hợp lệ."); }
        assert(object(header) && header.recipient_id === context.recipient_id &&
          positiveID(header.signed_prekey_id) && (header.one_time_prekey_id === null || positiveID(header.one_time_prekey_id)),
        "invalid_context", "Header recipient/prekey không khớp context message.");
        const cached = this.profile.message_keys[id];
        if (cached) {
          assert(sameContext(cached.context, context) && cached.sender_identity_key === header.sender_identity_key,
            "invalid_context", "UUID/context/identity khác message đã cache; không ghi đè key.");
          const incoming = context.recipient_id === this.userID;
          assert((incoming ? cached.recipient_identity_key : cached.sender_identity_key) === this.profile.identity.public_key,
            "invalid_context", "Identity local không khớp cached message.");
          const peerKey = incoming ? cached.sender_identity_key : cached.recipient_identity_key;
          this._pin(this.profile, thread.peer.id, peerKey);
          const opened = await this._crypto("decryptWithKey", { context,
            sender_identity_key: cached.sender_identity_key, recipient_identity_key: cached.recipient_identity_key,
            message_key: cached.message_key, expected_content_sha256: cached.content_sha256, content: message.content });
          if (this.profile.pending_send?.message_id === id) {
            assert(this._matchesPending(message, this.profile.pending_send), "invalid_context", "Lịch sử khác payload pending; giữ pending.");
            const next = clone(this.profile);
            next.pending_send = null;
            await this._save(next);
          }
          return { status: "ready", plaintext: opened.plaintext };
        }
        assert(context.recipient_id === this.userID, "missing_keys", "Thiếu message key của tin đã gửi; không thể tái tạo hoặc claim lại bundle.");
        this._pin(this.profile, thread.peer.id, header.sender_identity_key);
        assert(header.signed_prekey_id === this.profile.signed_prekey.key_id,
          "missing_keys", "Không có signed prekey local đúng ID của message.");
        const opkID = header.one_time_prekey_id;
        const opk = opkID === null ? null : this.profile.one_time_prekeys[opkID];
        assert(opkID === null || opk, "missing_keys", "Thiếu private OPK và message key; không thử chuyển sang 3DH.");
        const opened = await this._crypto("open", { context, identity: this.profile.identity,
          signed_prekey: this.profile.signed_prekey.key_pair, one_time_prekey: opk, content: message.content });
        assert(base64(opened.message_key, 32) && hash(opened.content_sha256) && hash(opened.sender_fingerprint),
          "crypto_failed", "Kết quả giải mã không hợp lệ; giữ private OPK.");
        this._pin(this.profile, thread.peer.id, header.sender_identity_key, opened.sender_fingerprint);
        const next = clone(this.profile);
        next.message_keys[id] = { message_key: opened.message_key, context, content_sha256: opened.content_sha256,
          sender_identity_key: header.sender_identity_key, recipient_identity_key: next.identity.public_key };
        next.peer_pins[thread.peer.id] = { identity_public_key: header.sender_identity_key, fingerprint: opened.sender_fingerprint };
        if (opkID !== null) delete next.one_time_prekeys[opkID];
        // Cache/pin and private OPK removal are one put in one local transaction.
        await this._save(next);
        return { status: "ready", plaintext: opened.plaintext };
      }, { message: true }).catch((error) => {
        if (error?.code === "identity_changed" && this._live()) {
          this.error = safeError(error);
          this.status = "error";
          this._notify();
        }
        return { status: "error", error: safeError(error) };
      });
    }
  }

  globalThis.MiniHermesE2EEClient = Object.freeze({ create: (options) => new Client(options) });
})();
