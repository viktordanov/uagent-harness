---
id: git-lfs
description: Git LFS, pointer files and fetching their content
files: [.gitattributes]
check: [git, lfs, version]
enabled: false
---
Git LFS: a file LFS tracks may be a small pointer file until its content is fetched. git lfs ls-files lists them; git lfs pull fetches them and needs the network, so request escalation for it.
