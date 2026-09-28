const assert = require("node:assert/strict");
const test = require("node:test");
const { merge, fetchThroughBoundary } = require("./realtime-core.js");

test("WebSocket duplicate and REST copy merge by external message ID, ordered by seq", () => {
  const bySeq = new Map();
  const byID = new Map();
  assert.equal(merge(bySeq, byID, [
    { message_id: "m2", seq: 2, content: "Bob" },
    { message_id: "m2", seq: 2, content: "Bob" },
  ]), true);
  assert.equal(merge(bySeq, byID, [
    { id: "m1", seq: 1, content: "Alice" },
    { id: "m2", seq: 2, content: "Bob" },
  ]), true);
  assert.deepEqual([...bySeq.values()].sort((a, b) => a.seq - b.seq).map((m) => m.id), ["m1", "m2"]);
  assert.equal(byID.size, 2);
  assert.equal(merge(bySeq, byID, [{ message_id: "m2", seq: 2 }]), false);
});

test("offline reconnect fetches every missed page through known seq", async () => {
  const pages = [
    { messages: Array.from({ length: 30 }, (_, i) => ({ id: `m${80-i}`, seq: 80-i })), next_cursor: 51 },
    { messages: Array.from({ length: 30 }, (_, i) => ({ id: `m${50-i}`, seq: 50-i })), next_cursor: 21 },
    { messages: Array.from({ length: 20 }, (_, i) => ({ id: `m${20-i}`, seq: 20-i })), next_cursor: null },
  ];
  const seen = [];
  const cursors = [];
  const maxSeq = await fetchThroughBoundary(async (cursor) => {
    cursors.push(cursor);
    return pages[cursors.length - 1];
  }, 10, (messages) => seen.push(...messages));
  assert.equal(maxSeq, 80);
  assert.deepEqual(cursors, [null, 51, 21]);
  assert.equal(seen.length, 80);
  assert.equal(seen.some((message) => message.seq === 11), true);
});

test("new thread with no baseline walks to end, not only first page", async () => {
  const pages = [
    { messages: [{ id: "m4", seq: 4 }, { id: "m3", seq: 3 }], next_cursor: 3 },
    { messages: [{ id: "m2", seq: 2 }, { id: "m1", seq: 1 }], next_cursor: null },
  ];
  let calls = 0;
  const seen = [];
  await fetchThroughBoundary(async () => pages[calls++], 0, (messages) => seen.push(...messages));
  assert.equal(calls, 2);
  assert.deepEqual(seen.map((message) => message.seq), [4, 3, 2, 1]);
});

test("bad cursor fails instead of silently omitting history", async () => {
  await assert.rejects(() => fetchThroughBoundary(
    async () => ({ messages: [{ id: "m5", seq: 5 }], next_cursor: 5 }),
    1,
    () => {},
  ), /Cursor/);
});
