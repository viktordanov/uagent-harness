// chunk splits items into arrays of at most size items, in order.
export function chunk(items, size) {
  if (!(size > 0)) throw new RangeError('size must be positive');
  const out = [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
}
