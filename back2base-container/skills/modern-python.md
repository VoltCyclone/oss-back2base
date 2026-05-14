---
name: modern-python
model: sonnet
context: fork
agent: Plan
paths: "**/*.py,pyproject.toml,setup.cfg,setup.py,*.toml"
description: "Modern Python tooling and best practices with uv, ruff, and pytest. Use for: python project setup, uv, ruff, pytest, pyproject.toml, virtual environment, linting, formatting, type checking, python dependencies."
---

# Modern Python Tooling

## Core Tools

| Tool | Replaces | Purpose |
|------|----------|---------|
| **uv** | pip, virtualenv, pipx, poetry | Package management, env management |
| **ruff** | flake8, black, isort, pylint | Linting + formatting (10-100x faster) |
| **pytest** | unittest | Testing with fixtures and plugins |

## Project Setup

```bash
uv init myproject
cd myproject
uv add fastapi uvicorn          # Add dependencies
uv add --dev pytest ruff mypy   # Add dev dependencies
```

## pyproject.toml

```toml
[project]
name = "myproject"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
    "fastapi>=0.100",
    "uvicorn>=0.23",
]

[dependency-groups]
dev = ["pytest>=8.0", "ruff>=0.4", "mypy>=1.10"]

[tool.ruff]
target-version = "py311"
line-length = 88

[tool.ruff.lint]
select = ["E", "F", "I", "N", "UP", "B", "SIM", "RUF"]

[tool.pytest.ini_options]
testpaths = ["tests"]
pythonpath = ["src"]
```

## Key Commands

```bash
# Dependencies
uv add requests                  # Add dependency
uv remove requests               # Remove dependency
uv sync                          # Install all deps from lockfile
uv run python app.py             # Run with managed env

# Linting & Formatting
uv run ruff check .              # Lint
uv run ruff check --fix .        # Auto-fix
uv run ruff format .             # Format

# Testing
uv run pytest                    # Run tests
uv run pytest -x                 # Stop on first failure
uv run pytest --cov=src          # With coverage
uv run pytest -k "test_auth"     # Run matching tests

# Type Checking
uv run mypy src/                 # Type check
```

## Best Practices

- **Never manually activate venvs** - use `uv run` for all commands
- **Always use `uv add`/`uv remove`** - don't manually edit pyproject.toml deps
- **Use `src/` layout** - keeps imports clean and prevents accidental local imports
- **Pin Python version** - `requires-python = ">=3.11"` in pyproject.toml
- **Use dependency groups** - separate dev, test, and production dependencies

## Migration From Legacy

| From | To |
|------|----|
| `pip install -r requirements.txt` | `uv pip compile requirements.txt > requirements.lock && uv sync` |
| `poetry add pkg` | `uv add pkg` |
| `black . && isort .` | `ruff format . && ruff check --fix .` |
| `flake8 src/` | `ruff check src/` |
| `python -m venv .venv && source .venv/bin/activate` | `uv run <command>` (automatic) |
