---
id: bash
description: macOS /bin/bash 3.2 lacks bash 4 features
when: {shell: [bash], shell_path: [/bin/bash], os: [darwin]}
---
macOS /bin/bash is 3.2: no declare -A, mapfile/readarray, ${x,,} or |&.
