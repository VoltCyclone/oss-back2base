---
name: mcp-builder
model: sonnet
context: fork
agent: Plan
description: Guide for creating high-quality MCP (Model Context Protocol) servers that enable LLMs to interact with external services through well-designed tools. Use when building MCP servers to integrate external APIs or services, whether in Python (FastMCP) or Node/TypeScript (MCP SDK).
---

# MCP Server Development Guide

## Overview

Create MCP servers that enable LLMs to interact with external services through well-designed tools.

## High-Level Workflow

### Phase 1: Deep Research and Planning

**API Coverage vs. Workflow Tools:**
Balance comprehensive API endpoint coverage with specialized workflow tools. When uncertain, prioritize comprehensive API coverage.

**Tool Naming:** Use consistent prefixes (e.g., `github_create_issue`, `github_list_repos`) and action-oriented naming.

**Study MCP Protocol Documentation:**
- Sitemap: `https://modelcontextprotocol.io/sitemap.xml`
- Fetch pages with `.md` suffix for markdown format

**Recommended stack:**
- **Language**: TypeScript (recommended) or Python
- **Transport**: Streamable HTTP for remote servers, stdio for local servers

### Phase 2: Implementation

**Project Structure:**
- TypeScript: See MCP TypeScript SDK README
- Python: See MCP Python SDK README

**Core Infrastructure:**
- API client with authentication
- Error handling helpers
- Response formatting (JSON/Markdown)
- Pagination support

**For each tool, define:**
- Input Schema (Zod for TypeScript, Pydantic for Python)
- Output Schema (define `outputSchema` where possible)
- Tool Description (concise summary)
- Annotations (`readOnlyHint`, `destructiveHint`, `idempotentHint`, `openWorldHint`)

### Phase 3: Review and Test

- No duplicated code (DRY)
- Consistent error handling
- Full type coverage
- Clear tool descriptions

**TypeScript:**
```bash
npm run build
npx @modelcontextprotocol/inspector
```

**Python:**
```bash
python -m py_compile your_server.py
```

### Phase 4: Create Evaluations

Create 10 evaluation questions that are:
- Independent, read-only, complex, realistic, verifiable, stable

Output format:
```xml
<evaluation>
  <qa_pair>
    <question>Your question here</question>
    <answer>Expected answer</answer>
  </qa_pair>
</evaluation>
```

## SDK Documentation

- **Python SDK**: `https://raw.githubusercontent.com/modelcontextprotocol/python-sdk/main/README.md`
- **TypeScript SDK**: `https://raw.githubusercontent.com/modelcontextprotocol/typescript-sdk/main/README.md`
