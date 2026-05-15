---
name: web-artifacts-builder
model: opusplan
context: fork
agent: general-purpose
description: "Build complex, self-contained HTML artifacts using React, Tailwind CSS, and shadcn/ui. Use when creating elaborate web components, interactive demos, or single-file web applications."
---

# Web Artifacts Builder

## Core Workflow

1. Initialize the frontend repository
2. Develop the artifact
3. Bundle code into a single HTML file
4. Display to the user
5. Optionally test

## Technology Stack

- React 18 + TypeScript
- Vite (dev server)
- Parcel (bundling to single HTML)
- Tailwind CSS 3.4.1
- shadcn/ui components
- Radix UI dependencies

## Initialization

```bash
# Set up a new artifact project
npx create-vite@latest <project-name> --template react-ts
cd <project-name>
npm install
npm install tailwindcss@3.4.1 postcss autoprefixer
npm install @radix-ui/react-slot class-variance-authority clsx tailwind-merge
```

## Design Guidelines

**Avoid AI slop**: No excessive centered layouts, purple gradients, uniform rounded corners, or Inter font everywhere.

Make bold, distinctive design decisions:
- Use asymmetric layouts
- Pick unexpected but harmonious color combinations
- Vary border radius intentionally
- Use real-feeling content, not lorem ipsum

## Bundling

Bundle the React application into a self-contained HTML file:
```bash
npx parcel build src/index.html --no-source-maps --no-scope-hoist
```

## Component Reference

shadcn/ui documentation: https://ui.shadcn.com/docs/components
