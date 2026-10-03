---
id: bash-heredoc
description: macOS /bin/bash's heredocs fail in the read-only sandbox
when: {sandbox: [read-only], os: [darwin], shell_path: [/bin/bash]}
---
macOS's /bin/bash ignores $TMPDIR for heredoc files, so heredocs fail here; write the text with printf to a file in $TMPDIR instead.
