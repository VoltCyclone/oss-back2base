---
name: webapp-testing
model: sonnet
context: fork
agent: general-purpose
paths: "**/*.spec.ts,**/*.spec.js,**/*.test.ts,**/*.test.js,playwright.config.*"
description: "Test local web applications using Playwright browser automation. Use when you need to interact with, test, or validate web application functionality through a real browser."
---

# Web Application Testing with Playwright

## Setup

```bash
pip install playwright
playwright install chromium
```

## Core Pattern

```python
from playwright.sync_api import sync_playwright

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()
    page.goto("http://localhost:3000")

    # CRITICAL: Wait for JS to execute
    page.wait_for_load_state('networkidle')

    # Interact with the page
    page.click("text=Submit")
    page.fill("input[name='email']", "test@example.com")

    # Assert
    assert page.is_visible("text=Success")

    # Screenshot for debugging
    page.screenshot(path="screenshot.png")

    browser.close()
```

## Decision Tree

1. **Static HTML?** -> Read files directly to find selectors
2. **Dynamic app, server running?** -> Write Playwright test directly
3. **Dynamic app, no server?** -> Start server first, then test

## Selector Strategies

Prefer in this order:
1. Text content: `page.click("text=Submit")`
2. Role: `page.get_by_role("button", name="Submit")`
3. Test ID: `page.get_by_test_id("submit-btn")`
4. CSS: `page.click(".submit-button")`
5. XPath: Last resort

## Common Operations

```python
# Fill form
page.fill("#username", "admin")
page.fill("#password", "secret")
page.click("button[type='submit']")

# Wait for navigation
page.wait_for_url("**/dashboard")

# Get text content
text = page.text_content(".result")

# Check element exists
assert page.locator(".success-message").count() > 0

# Console log capture
page.on("console", lambda msg: print(f"Console: {msg.text}"))

# Screenshot
page.screenshot(path="debug.png", full_page=True)
```

## Best Practices

- Always use `sync_playwright()` (not async)
- Always close the browser when done
- Wait for `networkidle` before inspecting dynamic content
- Use descriptive selectors (text, role) over fragile CSS paths
- Take screenshots for debugging failures
