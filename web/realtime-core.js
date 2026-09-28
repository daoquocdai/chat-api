// Shared, side-effect-free history reconciliation used by the browser and Node tests.
const MiniHermesRealtime = (() => {
  function merge(bySeq, byID, messages) {
    let changed = false;
    for (const raw of messages) {
      const id = raw.id || raw.message_id;
      const seq = Number(raw.seq);
      if (typeof id !== "string" || !id || !Number.isSafeInteger(seq) || seq <= 0) {
        continue;
      }
      const existingSeq = byID.get(id);
      if (existingSeq !== undefined && existingSeq !== seq) {
        continue;
      }
      const existing = bySeq.get(seq);
      if (existing && existing.id !== id) {
        continue;
      }
      const message = { ...raw, id, seq };
      delete message.message_id;
      bySeq.set(seq, message);
      byID.set(id, seq);
      changed ||= !existing;
    }
    return changed;
  }

  async function fetchThroughBoundary(loadPage, baseline, acceptPage) {
    let beforeSeq = null;
    let maxSeq = baseline;
    const visited = new Set();
    while (true) {
      const page = await loadPage(beforeSeq);
      const messages = Array.isArray(page.messages) ? page.messages : [];
      acceptPage(messages);
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
