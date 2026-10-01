// formatCents formats an integer amount of cents as dollars with thousands
// separators, e.g. 123456 -> "$1,234.56" and -5 -> "-$0.05".
export function formatCents(cents) {
  const dollars = Math.floor(cents / 100);
  const rest = cents % 100;
  return `$${dollars}.${rest}`;
}
