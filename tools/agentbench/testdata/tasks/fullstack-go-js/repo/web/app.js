// Pure helpers for the task list page; index.html wires them to the DOM.

export const API = '/api/tasks';

// tasksURL is the URL that lists tasks.
export function tasksURL() {
  return API;
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
