---
name: devcontainer-setup
model: sonnet
context: fork
agent: general-purpose
paths: ".devcontainer/**,devcontainer.json"
description: "Create pre-configured devcontainers with language-specific tooling for Python, Node, Rust, and Go projects. Use for: devcontainer, dev container, development container, containerized development, remote development, codespace."
---

# Devcontainer Setup

## When to Use
- Setting up isolated development environments
- Configuring Claude Code sandboxed workspaces
- Onboarding new developers with consistent environments

## Process

1. **Detect project** - infer from package.json, pyproject.toml, Cargo.toml, go.mod
2. **Detect languages** - identify tech stack from config files
3. **Generate config** - apply language-specific extensions and settings
4. **Write files** - output to `.devcontainer/` directory

## Base Template

```json
{
  "name": "project-name",
  "build": {
    "dockerfile": "Dockerfile"
  },
  "features": {
    "ghcr.io/devcontainers/features/common-utils:2": {},
    "ghcr.io/devcontainers/features/git:1": {}
  },
  "customizations": {
    "vscode": {
      "extensions": [],
      "settings": {}
    }
  },
  "postCreateCommand": "echo 'Setup complete'",
  "remoteUser": "vscode"
}
```

## Language Features

### Python
```json
{
  "features": {
    "ghcr.io/devcontainers/features/python:1": {
      "version": "3.12"
    }
  },
  "customizations": {
    "vscode": {
      "extensions": ["ms-python.python", "charliermarsh.ruff"],
      "settings": {
        "python.defaultInterpreterPath": "/usr/local/bin/python",
        "[python]": { "editor.defaultFormatter": "charliermarsh.ruff" }
      }
    }
  },
  "postCreateCommand": "pip install -e '.[dev]'"
}
```

### Node.js/TypeScript
```json
{
  "features": {
    "ghcr.io/devcontainers/features/node:1": {
      "version": "20"
    }
  },
  "customizations": {
    "vscode": {
      "extensions": ["dbaeumer.vscode-eslint", "esbenp.prettier-vscode"],
      "settings": {
        "editor.defaultFormatter": "esbenp.prettier-vscode",
        "editor.formatOnSave": true
      }
    }
  },
  "postCreateCommand": "npm ci"
}
```

### Go
```json
{
  "features": {
    "ghcr.io/devcontainers/features/go:1": {
      "version": "1.22"
    }
  },
  "customizations": {
    "vscode": {
      "extensions": ["golang.go"],
      "settings": {
        "go.toolsManagement.autoUpdate": true
      }
    }
  }
}
```

### Rust
```json
{
  "features": {
    "ghcr.io/devcontainers/features/rust:1": {
      "version": "latest"
    }
  },
  "customizations": {
    "vscode": {
      "extensions": ["rust-lang.rust-analyzer"],
      "settings": {
        "rust-analyzer.check.command": "clippy"
      }
    }
  }
}
```

## Multi-Language Projects
Configure all detected languages, chain setup commands:
```json
{
  "postCreateCommand": "pip install -e '.[dev]' && npm ci"
}
```
