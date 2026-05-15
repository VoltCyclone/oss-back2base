---
name: pptx
model: sonnet
context: fork
agent: general-purpose
paths: "**/*.pptx,**/*.ppt"
description: "Use this skill for any .pptx file involvement - creating, reading, editing, or modifying presentations, decks, and slide files."
---

# PowerPoint Presentation Guide

## Quick Reference

| Task | Guide |
|------|-------|
| Read/analyze content | `python -m markitdown presentation.pptx` |
| Create from scratch | Use pptxgenjs (`npm install -g pptxgenjs`) |

## Reading Content

```bash
python -m markitdown presentation.pptx
```

## Design Guidelines

### Before Starting

- **Pick a bold, content-informed color palette** specific to the topic
- **Dominance over equality**: One color dominates (60-70%), 1-2 supporting, one accent
- **Dark/light contrast**: Dark backgrounds for title + conclusion, light for content
- **Commit to a visual motif**: One distinctive element repeated throughout

### Color Palettes

| Theme | Primary | Secondary | Accent |
|-------|---------|-----------|--------|
| Midnight Executive | `1E2761` | `CADCFC` | `FFFFFF` |
| Forest & Moss | `2C5F2D` | `97BC62` | `F5F5F5` |
| Coral Energy | `F96167` | `F9E795` | `2F3C7E` |
| Warm Terracotta | `B85042` | `E7E8D1` | `A7BEAE` |
| Ocean Gradient | `065A82` | `1C7293` | `21295C` |
| Charcoal Minimal | `36454F` | `F2F2F2` | `212121` |

### Typography

| Element | Size |
|---------|------|
| Slide title | 36-44pt bold |
| Section header | 20-24pt bold |
| Body text | 14-16pt |
| Captions | 10-12pt muted |

### For Each Slide

**Every slide needs a visual element** - image, chart, icon, or shape. Text-only slides are forgettable.

Layout options:
- Two-column (text left, illustration right)
- Icon + text rows
- 2x2 or 2x3 grid
- Half-bleed image with content overlay

### Avoid

- Don't repeat the same layout
- Don't center body text - left-align
- Don't default to blue
- Don't create text-only slides
- NEVER use accent lines under titles (hallmark of AI-generated slides)

## QA (Required)

Convert to images for visual inspection:
```bash
python scripts/office/soffice.py --headless --convert-to pdf output.pptx
pdftoppm -jpeg -r 150 output.pdf slide
```

Check for: overlapping elements, text overflow, low contrast, uneven gaps.

## Dependencies

- `pip install "markitdown[pptx]"` - text extraction
- `npm install -g pptxgenjs` - creating from scratch
- LibreOffice - PDF conversion
- Poppler (`pdftoppm`) - PDF to images
