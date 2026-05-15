---
name: canvas-design
model: opusplan
context: fork
agent: general-purpose
description: "Design visual art in PNG and PDF formats. Use when the user wants visual designs, posters, infographics, or artistic compositions that are primarily visual (90% design, 10% text)."
---

# Canvas Design

## Core Approach

### Step 1: Design Philosophy (4-6 paragraphs)
Develop an aesthetic manifesto expressing a visual worldview through form, color, space, and composition rather than text.

### Step 2: Canvas Expression
Translate philosophy into a single-page PDF or PNG masterpiece.

## Key Principles

- Ideas communicate through space, form, color, composition - not paragraphs
- Text appears sparingly as visual accent, never as explanation blocks
- 90% visual design, 10% essential text
- Work must appear meticulously crafted with perfect spacing and flawless alignment

## Design Process

1. Establish a design philosophy (e.g., "Brutalist Joy", "Chromatic Silence")
2. Define a systematic visual language
3. Choose a subtle, niche reference as conceptual foundation
4. Execute with museum-ready quality

## Technical Requirements

- Typography must be design-forward
- All elements require breathing room - no overlapping
- Professional execution is non-negotiable
- Create original work only - no copying existing artists

## Implementation

Use Python with libraries like:
- **Pillow/PIL**: Raster graphics (PNG)
- **reportlab**: Vector graphics (PDF)
- **cairo/pycairo**: Advanced vector/raster
- **matplotlib**: Data-driven visual art

```python
from PIL import Image, ImageDraw, ImageFont

img = Image.new('RGB', (2400, 3200), color='#1a1a2e')
draw = ImageDraw.Draw(img)

# Your artistic composition here

img.save('artwork.png', quality=95)
```
