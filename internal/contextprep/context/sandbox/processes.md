---
id: processes
description: Seatbelt blocks process listings
when: {sandbox: [read-only, workspace-write], os: [darwin]}
---
ps and pgrep fail in the sandbox (lsof works); request escalation to list processes.
