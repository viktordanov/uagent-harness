# ini

A small INI parser: `ini.Parse(r)` returns a `File`, a map from section name
to keys and values.

## Rules

1. Lines are split on `\n`; a trailing `\r` is removed. Leading and trailing
   white space on a line is ignored.
2. Blank lines are ignored. A line whose first non-space character is `#` or
   `;` is a comment. There are no inline comments: in `a = b # c` the value is
   `b # c`.
3. `[name]` starts a section. The name is trimmed and case-sensitive. A
   section header without its closing `]`, or with an empty name, is an
   error. Keys before the first header belong to the section `""`.
4. `key = value` sets a key. Key and value are trimmed. Keys are
   case-insensitive and stored lower-case. A line without `=`, or with an
   empty key, is an error.
5. A value that starts with `"` is quoted: it runs to the next unescaped `"`,
   which must end the line. Inside, `\"` is a quote and `\\` a backslash; any
   other backslash is kept as it is. Spaces inside the quotes are kept. A
   missing closing quote, or text after it, is an error.
6. An unquoted value ending in `\` continues on the next line: the `\` is
   removed and the next line, trimmed, is appended after a single space.
   Continuations may chain. Comment rules do not apply to continuation lines.
7. A key set twice in a section keeps the last value. A section that appears
   twice is merged.
8. Errors are `*ParseError` with the 1-based line number where the bad line
   (or the quoted value) starts.
