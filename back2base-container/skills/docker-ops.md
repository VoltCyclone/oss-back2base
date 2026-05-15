---
name: docker-ops
model: sonnet
context: fork
agent: Plan
paths: "Dockerfile*,docker-compose*.yml,docker-compose*.yaml,.dockerignore"
description: "Docker containerization patterns, Dockerfile best practices, multi-stage builds, and Docker Compose. Use for: docker, Dockerfile, docker-compose, container, image, multi-stage build, docker build, docker run, .dockerignore, health check, distroless, scratch image, BuildKit, layer caching, container security."
---

# Docker Operations

## Dockerfile Best Practices

| Practice | Do | Don't |
|----------|------|-------|
| Base image | `FROM node:20-slim` | `FROM node:latest` |
| Layer caching | Copy dependency files first, then source | `COPY . .` before install |
| Package install | `apt-get update && apt-get install -y ... && rm -rf /var/lib/apt/lists/*` | Separate RUN for update and install |
| User | `USER nonroot` | Run as root in production |
| Multi-stage | Separate build and runtime stages | Ship compiler toolchains |
| Secrets | `--mount=type=secret` (BuildKit) | `COPY .env .` or `ARG PASSWORD` |
| ENTRYPOINT vs CMD | ENTRYPOINT for fixed binary, CMD for defaults | Shell form for signal handling |

## Multi-Stage Build Decision Tree

```
Go ──── CGO disabled? ── Yes ──► scratch or distroless/static
                         No  ──► distroless/base or alpine

Rust ── Static musl? ─── Yes ──► scratch or distroless/static
                         No  ──► distroless/cc or debian-slim

Node.js ─ Need native? ─ Yes ──► node:20-slim
                         No  ──► node:20-alpine

Python ── Need C libs? ─ Yes ──► python:3.12-slim
                         No  ──► python:3.12-slim
```

## Optimal Layer Order

```dockerfile
FROM python:3.12-slim
# 1. System deps (rarely change)
RUN apt-get update && apt-get install -y --no-install-recommends libpq-dev && rm -rf /var/lib/apt/lists/*
# 2. Dependencies (occasionally change)
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
# 3. App code (frequently changes)
COPY src/ ./src/
CMD ["python", "-m", "app"]
```

## Docker Compose

```yaml
services:
  web:
    build:
      context: .
      target: production
    ports: ["8080:8000"]
    environment:
      DATABASE_URL: postgres://db:5432/app
    depends_on:
      db:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 30s
      timeout: 5s
      retries: 3
    restart: unless-stopped

  db:
    image: postgres:16-alpine
    volumes: [db-data:/var/lib/postgresql/data]
    environment:
      POSTGRES_DB: app
      POSTGRES_PASSWORD_FILE: /run/secrets/db_password
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]

volumes:
  db-data:
```

## Security

| Area | Recommendation |
|------|----------------|
| User | Run as non-root |
| Base image | Pin digest: `FROM python:3.12-slim@sha256:abc...` |
| Filesystem | Read-only root: `--read-only --tmpfs /tmp` |
| Capabilities | `--cap-drop=ALL --cap-add=NET_BIND_SERVICE` |
| Scanning | `trivy image myapp:latest` or `grype myapp:latest` |
| No latest | Always pin specific tags |

## Common Gotchas

| Gotcha | Fix |
|--------|-----|
| Large images | Multi-stage builds |
| Cache busting | Copy lockfile first, install, then copy source |
| Secrets in layers | `--mount=type=secret` or runtime env vars |
| PID 1 problem | Use `tini` as init or exec form CMD |
| DNS issues (Alpine) | `apk add --no-cache libc6-compat` |
| Missing signals | Exec form: `CMD ["node", "server.js"]` |
| Build context size | Add `.dockerignore` |

## Essential Commands

```bash
docker build -t myapp:1.0 .
docker build -t myapp:1.0 --target production .
docker run -d --name myapp -p 8080:8000 myapp:1.0
docker run --rm -it myapp:1.0 /bin/sh
docker exec -it myapp /bin/sh
docker logs -f myapp
docker system prune -a --volumes
```
