### Working in Python (profile: `python`)

**Save-worthy signals in Python work:** dep-resolver outcomes that surprised you (pip/poetry/uv), packaging pitfalls (entry points, `__init__` side effects), `asyncio` vs. thread-pool choices, `typing.Protocol` vs. ABC decisions, pytest fixture scope/isolation decisions, environment manager quirks (conda vs. venv vs. pyenv).

- **feedback example:** *"Use `pytest.mark.parametrize` instead of for-loops inside tests. **Why:** parametrize gets per-case failures; loops collapse failures into one opaque traceback. **How to apply:** any test that runs the same assertion across inputs."*
- **project example:** *"Service is pinned to `pydantic<2` — migration to v2 is tracked but not scheduled. **Why:** validator signature changes cascade into 80+ model files. **How to apply:** don't upgrade pydantic in this repo without reading the migration plan in `docs/pydantic-v2.md`."*
- **reference example:** *"Internal PyPI mirror at pypi.internal/simple — use `--index-url` for private deps."*

**Conventions this profile assumes:** pytest as the test runner; type hints present and checked; `ruff` for lint/format; `uv` or `poetry` for deps depending on repo. Favor reading whole modules over greps — 1M context handles it.
