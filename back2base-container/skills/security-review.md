---
name: security-review
model: opus
context: fork
agent: Explore
allowed-tools: Read Grep Glob Bash
description: "Security-focused code review covering OWASP Top 10, secrets detection, auth patterns, and input validation. Use for: security audit, vulnerability scan, OWASP, injection, XSS, CSRF, authentication review, authorization review, secrets in code, dependency audit."
---

# Security Review

## OWASP Top 10 Checklist

### 1. Injection (SQL, NoSQL, Command, LDAP)
```
[ ] Parameterized queries for all DB operations
[ ] No string concatenation in queries
[ ] Input validated before use in commands
[ ] ORM used with parameterized methods
```

### 2. Broken Authentication
```
[ ] Passwords hashed with bcrypt/argon2 (not MD5/SHA)
[ ] Rate limiting on login endpoints
[ ] Session tokens are random, long, and expire
[ ] Multi-factor authentication for sensitive ops
```

### 3. Sensitive Data Exposure
```
[ ] No secrets in source code or logs
[ ] HTTPS everywhere, HSTS headers set
[ ] Sensitive data encrypted at rest
[ ] PII masked in logs
```

### 4. XML External Entities (XXE)
```
[ ] XML parsing disables external entities
[ ] DTD processing disabled
[ ] Use JSON instead of XML where possible
```

### 5. Broken Access Control
```
[ ] Authorization checked on every request
[ ] RBAC/ABAC enforced server-side
[ ] Direct object references validated
[ ] CORS configured restrictively
```

### 6. Security Misconfiguration
```
[ ] Default credentials changed
[ ] Error messages don't leak stack traces
[ ] Unnecessary features/ports disabled
[ ] Security headers set (CSP, X-Frame-Options, etc.)
```

### 7. Cross-Site Scripting (XSS)
```
[ ] Output encoded for context (HTML, JS, URL, CSS)
[ ] CSP header prevents inline scripts
[ ] User input never inserted into raw HTML
[ ] Framework auto-escaping enabled
```

### 8. Insecure Deserialization
```
[ ] No deserialization of untrusted data
[ ] JSON preferred over serialized objects
[ ] Integrity checks on serialized data
```

### 9. Known Vulnerabilities
```
[ ] Dependencies up to date
[ ] npm audit / pip-audit / govulncheck clean
[ ] No end-of-life frameworks or libraries
```

### 10. Insufficient Logging
```
[ ] Authentication events logged
[ ] Authorization failures logged
[ ] Input validation failures logged
[ ] Logs don't contain sensitive data
```

## Secrets Detection

```bash
# Check for common secret patterns
grep -rn 'password\s*=' --include='*.py' --include='*.js' --include='*.ts' .
grep -rn 'api[_-]key\s*=' --include='*.py' --include='*.js' --include='*.ts' .
grep -rn 'secret\s*=' --include='*.py' --include='*.js' --include='*.ts' .
grep -rn 'BEGIN.*PRIVATE KEY' .
grep -rn 'AKIA[0-9A-Z]{16}' .          # AWS access keys
```

Tools:
- `gitleaks detect` - scan git history for secrets
- `trufflehog filesystem .` - find secrets in files

## Dependency Audit

```bash
npm audit                          # Node.js
pip-audit                          # Python
govulncheck ./...                  # Go
cargo audit                        # Rust
```

## Security Headers

```
Content-Security-Policy: default-src 'self'; script-src 'self'
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Strict-Transport-Security: max-age=31536000; includeSubDomains
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: camera=(), microphone=(), geolocation=()
```

## Red Flags

- Disabled CSRF protection
- `eval()` or `exec()` with user input
- Hardcoded credentials or API keys
- `dangerouslySetInnerHTML` without sanitization
- `--no-verify` in git hooks
- Overly permissive CORS (`*`)
- JWT without expiration
- HTTP instead of HTTPS
