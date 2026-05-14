---
name: testgen
model: sonnet
context: fork
agent: general-purpose
description: "Automated test generation with framework detection and language-specific patterns. Use for: generate tests, write tests, test coverage, unit test, integration test, e2e test, test suite, missing tests."
---

# Test Generation

## Workflow

1. **Analyze target** - file, function, or directory; check for existing tests
2. **Detect framework** - examine package.json, pyproject.toml, go.mod, Cargo.toml
3. **Load standards** - project conventions, existing test patterns, naming
4. **Generate tests** - happy path, edge cases, error scenarios
5. **Verify** - run generated tests, ensure they pass

## Framework Detection

| File | Framework | Test Runner |
|------|-----------|-------------|
| `package.json` with vitest | Vitest | `npx vitest run` |
| `package.json` with jest | Jest | `npx jest` |
| `pyproject.toml` with pytest | pytest | `pytest` |
| `go.mod` | Go testing | `go test ./...` |
| `Cargo.toml` | Rust test | `cargo test` |
| `playwright.config.*` | Playwright | `npx playwright test` |

## Test Structure Pattern

```
describe('Unit Under Test', () => {
  describe('methodName', () => {
    it('should handle the happy path', () => { ... })
    it('should handle edge case: empty input', () => { ... })
    it('should handle edge case: null/undefined', () => { ... })
    it('should throw on invalid input', () => { ... })
    it('should handle boundary values', () => { ... })
  })
})
```

## What to Test

### Always Test
- Happy path (normal expected input)
- Edge cases (empty, null, zero, max values, boundary)
- Error cases (invalid input, network failures, timeouts)
- State transitions (before/after side effects)

### Test Naming
- Describe WHAT the test verifies, not HOW
- Pattern: `should [expected behavior] when [condition]`

### Don't Test
- Implementation details (private methods)
- Framework code (React rendering, Express routing)
- Trivial getters/setters
- Third-party library internals

## Language-Specific Patterns

### TypeScript/JavaScript (Vitest/Jest)
```typescript
import { describe, it, expect, vi } from 'vitest'

describe('UserService', () => {
  it('should create user with valid data', async () => {
    const user = await service.create({ name: 'Test', email: 'test@test.com' })
    expect(user.id).toBeDefined()
    expect(user.name).toBe('Test')
  })

  it('should throw on duplicate email', async () => {
    await expect(service.create({ email: 'dup@test.com' }))
      .rejects.toThrow('Email already exists')
  })
})
```

### Python (pytest)
```python
import pytest

def test_create_user_with_valid_data(user_service):
    user = user_service.create(name="Test", email="test@test.com")
    assert user.id is not None
    assert user.name == "Test"

def test_create_user_duplicate_email_raises(user_service):
    with pytest.raises(ValueError, match="already exists"):
        user_service.create(email="dup@test.com")

@pytest.fixture
def user_service(db):
    return UserService(db)
```

### Go
```go
func TestCreateUser(t *testing.T) {
    t.Run("valid data", func(t *testing.T) {
        user, err := svc.Create(ctx, CreateUserInput{Name: "Test"})
        require.NoError(t, err)
        assert.Equal(t, "Test", user.Name)
    })

    t.Run("duplicate email", func(t *testing.T) {
        _, err := svc.Create(ctx, CreateUserInput{Email: "dup@test.com"})
        assert.ErrorContains(t, err, "already exists")
    })
}
```

## Coverage Targets

| Type | Target | Notes |
|------|--------|-------|
| Unit tests | 80%+ line coverage | Focus on business logic |
| Integration tests | Key paths covered | API endpoints, DB queries |
| E2E tests | Critical user flows | Login, checkout, core features |
