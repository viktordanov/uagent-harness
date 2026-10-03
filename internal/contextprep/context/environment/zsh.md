---
id: zsh
description: zsh's glob and word-splitting gotchas
when: {shell: [zsh]}
---
zsh differs from bash:
- An unmatched glob is an error ("no matches found") and the command does not run: quote patterns and URLs with *, ?, [ (find -name '*.go', curl 'https://x?a=1').
- Unquoted $var is not word-split: use ${=var} or an array.
- Arrays index from 1; read -a is read -A.
