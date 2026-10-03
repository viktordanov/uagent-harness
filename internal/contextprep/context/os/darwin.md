---
id: darwin
description: macOS's BSD tools differ from GNU's
when: {os: [darwin]}
---
macOS has BSD tools, not GNU: sed -i '' (not sed -i), stat -f (not -c), date -v-1d (not -d), no grep -P (use -E or perl), no nproc (sysctl -n hw.ncpu).
