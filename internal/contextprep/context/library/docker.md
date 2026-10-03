---
id: docker
description: Docker, the daemon's socket needs escalation
files: [Dockerfile, compose.yaml, compose.yml, docker-compose.yml, docker-compose.yaml]
check: [docker, --version]
enabled: false
---
Docker: the docker client talks to the daemon over a socket, which the sandbox treats as network: request escalation for docker commands from the first try. Use docker compose (v2), not docker-compose.
