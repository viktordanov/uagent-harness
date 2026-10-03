---
id: sh
description: Plain POSIX sh lacks bash features
when: {shell: [sh]}
---
This is plain POSIX sh: [[ ]], arrays, source, {a,b}, <(…) and echo -e may not work; use [ ], ., printf, or run bash -c '…'.
