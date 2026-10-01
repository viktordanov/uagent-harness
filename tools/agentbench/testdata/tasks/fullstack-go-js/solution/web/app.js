// Pure helpers for the task list page; index.html wires them to the DOM.

export const API = '/api/tasks';

// STATUSES are the statuses a task moves through, as the API names them.
export const STATUSES = ['todo', 'doing', 'done'];

// tasksURL is the URL that lists tasks, filtered by status when given.
export function tasksURL(status) {
  if (!status) return API;
  return `${API}?status=${encodeURIComponent(status)}`;
}

// statusOptions are the filter dropdown's options: all, then each status.
export function statusOptions() {
  return [{ value: '', label: 'All' }, ...STATUSES.map((s) => ({ value: s, label: s[0].toUpperCase() + s.slice(1) }))];
}

// renderRow formats one task as a list item's text.
export function renderRow(task) {
  const mark = task.status === 'done' ? '✓' : '·';
  return `${mark} ${task.title}`;
}

// summarize counts tasks per status.
export function summarize(tasks) {
  const counts = { todo: 0, doing: 0, done: 0 };
  for (const t of tasks) {
    if (t.status in counts) counts[t.status]++;
  }
  return counts;
}
