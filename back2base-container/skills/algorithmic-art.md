---
name: algorithmic-art
model: opusplan
context: fork
agent: general-purpose
description: "Generate original computational art using p5.js with seeded randomness, particle systems, and interactive controls. Use when the user wants generative art, creative coding, or algorithmic visual pieces."
---

# Algorithmic Art Creation

## Philosophy

Beauty lives in the process, not the final frame. Prioritize emergent behavior from algorithms over static imagery.

## Two-Phase Process

### Phase 1: Algorithmic Philosophy (4-6 paragraphs)
- Name the aesthetic movement
- Describe computational processes, noise patterns, particle dynamics
- Explain temporal evolution and interactivity
- Define a conceptual seed (subtle reference woven into parameters)

### Phase 2: p5.js Implementation

Create a self-contained HTML file with:
- Seeded randomness for reproducibility
- Interactive parameter controls in a sidebar
- p5.js loaded from CDN

### Template Structure

```html
<!DOCTYPE html>
<html>
<head>
  <script src="https://cdnjs.cloudflare.com/ajax/libs/p5.js/1.9.0/p5.min.js"></script>
  <style>
    /* Sidebar with controls, canvas area */
  </style>
</head>
<body>
  <div id="sidebar">
    <!-- Seed navigation, parameter sliders -->
  </div>
  <div id="canvas-container"></div>
  <script>
    // Seeded random, setup(), draw(), parameter controls
  </script>
</body>
</html>
```

## Key Principles

- Parameters should emerge from what's tunable, not preset menus
- Algorithm flows from the philosophy
- Express controlled chaos, mathematical beauty, or emergent systems
- Use seeded randomness so results are reproducible
- Include interactive controls for exploration

## Technical Elements

- **Noise fields**: Perlin noise for organic flow
- **Particle systems**: For movement and accumulation
- **Color palettes**: Derive from the conceptual seed
- **Temporal evolution**: Animations that develop over time
- **Interactivity**: Mouse influence, parameter adjustment
