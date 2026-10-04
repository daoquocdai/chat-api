// Shared, side-effect-free history reconciliation used by the browser and Node tests.
const MiniHermesRealtime = (() => {
  function merge(bySeq, byID, messages) {
    let changed = false;
    const stagedSeq = new Map(), stagedID = new Map();
    for (const raw of messages) {
      const id = raw.id || raw.message_id;
      const seq = Number(raw.seq);
      if (typeof id !== "string" || !id || !Number.isSafeInteger(seq) || seq <= 0) {
        continue;
      }
      const existingSeq = stagedID.get(id) ?? byID.get(id);
      if (existingSeq !== undefined && existingSeq !== seq) {
        throw new Error("UUID message đã xuất hiện với seq khác.");
      }
      const existing = stagedSeq.get(seq) || bySeq.get(seq);
      if (existing && existing.id !== id) {
        throw new Error("Seq message đã xuất hiện với UUID khác.");
      }
      if (existing && ["thread_id", "sender_id", "kind", "content_format", "content"].some((field) => existing[field] !== raw[field])) {
        throw new Error("Context hoặc nội dung của UUID message đã thay đổi.");
      }
      const message = { ...raw, id, seq };
      delete message.message_id;
      stagedSeq.set(seq, message);
      stagedID.set(id, seq);
      changed ||= !existing;
    }
    // Validate the complete page before allowing any raw payload to overwrite a cache.
    for (const [seq, message] of stagedSeq) bySeq.set(seq, message);
    for (const [id, seq] of stagedID) byID.set(id, seq);
    return changed;
  }

  async function fetchThroughBoundary(loadPage, baseline, acceptPage) {
    let beforeSeq = null;
    let maxSeq = baseline;
    const visited = new Set();
    while (true) {
      const page = await loadPage(beforeSeq);
      const messages = Array.isArray(page.messages) ? page.messages : [];
      acceptPage(messages, page, beforeSeq);
      for (const message of messages) {
        maxSeq = Math.max(maxSeq, Number(message.seq) || 0);
      }
      if (baseline > 0 && messages.some((message) => Number(message.seq) <= baseline)) {
        return maxSeq;
      }
      const next = page.next_cursor;
      if (next === null || next === undefined) {
        return maxSeq;
      }
      const cursor = Number(next);
      if (!Number.isSafeInteger(cursor) || cursor <= 0 || visited.has(cursor) ||
          (beforeSeq !== null && cursor >= beforeSeq)) {
        throw new Error("Cursor lịch sử không hợp lệ.");
      }
      visited.add(cursor);
      beforeSeq = cursor;
    }
  }

  return { merge, fetchThroughBoundary };
})();

if (typeof module !== "undefined") {
  module.exports = MiniHermesRealtime;
}
