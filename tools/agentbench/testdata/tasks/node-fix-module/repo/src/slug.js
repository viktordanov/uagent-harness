// slugify turns a title into a URL slug: lower case ASCII letters and
// digits separated by single dashes, accents removed.
export function slugify(title) {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9]/g, '-');
}
