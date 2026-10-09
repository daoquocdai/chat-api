(function (root) {
  "use strict";

  const methods = Object.freeze([
    "createAccount", "deriveCredentials", "openAccount", "createEpoch", "openEpoch",
    "encryptEpochBackup", "decryptEpochBackup", "sealMessage", "openMessage",
  ]);
  const errorMessages = Object.freeze({
    invalid_input: "Yêu cầu crypto không hợp lệ.",
    invalid_key: "Khóa crypto không hợp lệ hoặc bị thiếu.",
    invalid_bundle: "Bundle public không hợp lệ.",
    invalid_context: "Ngữ cảnh message không khớp.",
    decrypt_failed: "Không thể giải mã message.",
    crypto_failed: "Không thể xử lý crypto.",
    wasm_unavailable: "Không thể tải bộ mã hóa E2EE. Hãy tải lại trang.",
  });
  let status = "loading";
  let loading = null;
  let running = false;

  function failure(code) {
    const known = Object.prototype.hasOwnProperty.call(errorMessages, code);
    const error = new Error(errorMessages[known ? code : "crypto_failed"]);
    error.code = known ? code : "crypto_failed";
    return error;
  }

  function bridgeReady() {
    const bridge = root.MiniHermesE2EE;
    return bridge && methods.every((name) => typeof bridge[name] === "function");
  }

  function call(method, input) {
    if (status !== "ready" || !running || !bridgeReady()) {
      throw failure("wasm_unavailable");
    }
    if (!methods.includes(method) || !input || typeof input !== "object" || Array.isArray(input)) {
      throw failure("invalid_input");
    }
    let output;
    try {
      const raw = root.MiniHermesE2EE[method](JSON.stringify(input));
      if (typeof raw !== "string") throw failure("crypto_failed");
      output = JSON.parse(raw);
    } catch (_) {
      throw failure("crypto_failed");
    }
    if (!output || typeof output !== "object" || Array.isArray(output)
      || Object.keys(output).length !== 3 || typeof output.ok !== "boolean"
      || !Object.prototype.hasOwnProperty.call(output, "data")
      || !Object.prototype.hasOwnProperty.call(output, "error")) {
      throw failure("crypto_failed");
    }
    if (!output.ok) {
      if (output.data !== null || !output.error || typeof output.error.code !== "string"
        || typeof output.error.message !== "string") throw failure("crypto_failed");
      throw failure(output.error.code);
    }
    if (output.error !== null || !output.data || typeof output.data !== "object"
      || Array.isArray(output.data)) throw failure("crypto_failed");
    return output.data;
  }

  async function start() {
    status = "loading";
    if (!root.WebAssembly || typeof root.Go !== "function" || typeof root.fetch !== "function") {
      throw failure("wasm_unavailable");
    }
    const go = new root.Go();
    const response = await root.fetch("/e2ee.wasm", { cache: "no-store" });
    if (!response.ok) throw failure("wasm_unavailable");
    const artifact = await response.arrayBuffer();
    const result = await root.WebAssembly.instantiate(artifact, go.importObject);
    running = true;
    // Go main keeps its callbacks alive. Its completion is a runtime exit,
    // not the readiness signal; waiting for go.run would never resolve here.
    Promise.resolve(go.run(result.instance)).then(
      () => { running = false; status = "error"; },
      () => { running = false; status = "error"; },
    );
    const deadline = Date.now() + 10000;
    while (!bridgeReady()) {
      if (!running || Date.now() >= deadline) throw failure("wasm_unavailable");
      await new Promise((resolve) => root.setTimeout(resolve, 20));
    }
    // Observe an immediately completed/failed go.run before resolving ready.
    await Promise.resolve();
    if (!running) throw failure("wasm_unavailable");
    status = "ready";
    return Object.freeze({ call });
  }

  root.MiniHermesWASM = Object.freeze({
    get status() { return status; },
    load() {
      if (!loading) {
        loading = start().catch(() => {
          running = false;
          status = "error";
          throw failure("wasm_unavailable");
        });
      }
      return loading;
    },
  });
})(globalThis);
