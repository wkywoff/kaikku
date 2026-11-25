export function shuffled(items, random = Math.random) {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1));
    [out[i], out[j]] = [out[j], out[i]];
  }
  return out;
}

export function followingIndex(s) {
  if (s.index + 1 < s.queue.length) return s.index + 1;
  if (s.repeat !== 'off' && s.queue.length > 0) return 0;
  return null;
}

export function startQueue(tracks, startIndex, shuffle) {
  if (!shuffle) return { queue: tracks, index: startIndex ?? 0, unshuffled: null };
  const queue = startIndex === null ? shuffled(tracks) : [tracks[startIndex], ...shuffled(tracks.filter((_, i) => i !== startIndex))];
  return { queue, index: 0, unshuffled: tracks };
}

export function shuffleRest({ queue, index }) {
  const current = queue[index];
  const rest = queue.filter((_, i) => i !== index);
  return { queue: current ? [current, ...shuffled(rest)] : shuffled(rest), index: current ? 0 : -1, unshuffled: queue };
}

export function restoreOrder({ queue, index, unshuffled }) {
  const current = queue[index];
  const order = unshuffled && unshuffled.length ? unshuffled : queue;
  return { queue: order, index: current ? order.indexOf(current) : -1, unshuffled: null };
}

export function beforeAutoplay(list, from) {
  const i = list.findIndex((e, j) => j > from && e.autoplay);
  return i < 0 ? list.length : i;
}

export function afterUpNext(list, from) {
  let i = from + 1;
  while (i < list.length && list[i].upNext) i++;
  return i;
}

export function insertEntries({ queue, index, unshuffled }, entries, place) {
  const at = place(queue, index);
  let order = unshuffled;
  if (order) {
    const u = place(order, order.indexOf(queue[index]));
    order = [...order.slice(0, u), ...entries, ...order.slice(u)];
  }
  return { queue: [...queue.slice(0, at), ...entries, ...queue.slice(at)], unshuffled: order, at };
}

export function removeEntry({ queue, index, unshuffled }, idx) {
  const removed = queue[idx];
  const rest = queue.filter((_, i) => i !== idx);
  let newIndex = idx < index ? index - 1 : index;
  if (idx === index) newIndex = Math.min(idx, rest.length - 1);
  return { queue: rest, index: newIndex, unshuffled: unshuffled ? unshuffled.filter((t) => t !== removed) : unshuffled };
}

export function swapEntries({ queue, index }, a, b) {
  const out = [...queue];
  [out[a], out[b]] = [out[b], out[a]];
  return { queue: out, index: index === a ? b : index === b ? a : index };
}

export function moveEntry({ queue, index }, from, to) {
  const out = [...queue];
  const [moved] = out.splice(from, 1);
  out.splice(to, 0, moved);
  return { queue: out, index: out.indexOf(queue[index]) };
}

export function withoutAutoplayed({ queue, index, unshuffled }) {
  const kept = queue.filter((e, i) => !e.autoplay || i <= index);
  return { queue: kept, unshuffled: unshuffled ? unshuffled.filter((e) => kept.includes(e)) : unshuffled };
}
