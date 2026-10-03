---
id: local-sockets
description: Seatbelt counts local sockets as network
when: {sandbox: [read-only, workspace-write], os: [darwin], network: false}
---
Local sockets count as network: docker and other clients of a daemon's socket fail in the sandbox, so request escalation for them from the first try.
