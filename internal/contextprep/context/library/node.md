---
id: node
description: Node, the package manager the lockfile names, and caches
files: [package.json]
check: [node, --version]
enabled: false
---
Node: use the package manager the lockfile names (pnpm-lock.yaml: pnpm, yarn.lock: yarn, package-lock.json: npm). When a cache cannot be written in the sandbox, set npm_config_cache=$TMPDIR/npm. Run one test file instead of the whole suite.
