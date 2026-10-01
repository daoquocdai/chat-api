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

  // A REST page confirms both returned messages and gaps inside its numeric range.
  // A gap outside these ranges still needs fetching; an event alone confirms only its seq.
  function confirmRange(ranges, start, end) {
    if (end < start) return;
    const merged = [];
    for (const range of [...ranges, [start, end]].sort((a, b) => a[0] - b[0])) {
      const last = merged[merged.length - 1];
      if (last && range[0] <= last[1] + 1) last[1] = Math.max(last[1], range[1]);
      else merged.push([...range]);
    }
    ranges.splice(0, ranges.length, ...merged);
  }

  function covers(ranges, start, end) {
    return end < start || ranges.some(([low, high]) => low <= start && high >= end);
  }

  function confirmPage(cache, page, beforeSeq = null) {
    const messages = Array.isArray(page.messages) ? page.messages : [];
    const seqs = messages.map((message) => Number(message.seq));
    const end = beforeSeq === null ? Math.max(0, ...seqs) : beforeSeq - 1;
    const start = page.next_cursor == null ? 1 : Math.min(...seqs);
    confirmRange(cache.confirmedRanges, start, end);
    return end;
  }

  function visibleReadCandidate(cache, userID) {
    let candidate = cache.lastReadSeq;
    let target = candidate;
    for (const message of [...cache.messages.values()].sort((a, b) => a.seq - b.seq)) {
      if (message.seq <= candidate) continue;
      if (!covers(cache.confirmedRanges, candidate + 1, message.seq)) break;
      if (message.sender_id !== userID && message.kind !== "system") {
        if (!cache.seenReceivedSeqs.has(message.seq)) break;
        target = message.seq;
      }
      candidate = message.seq;
    }
    return target;
  }

  return { merge, fetchThroughBoundary, confirmRange, covers, confirmPage, visibleReadCandidate };
})();

if (typeof module !== "undefined") {
  module.exports = MiniHermesRealtime;
}
