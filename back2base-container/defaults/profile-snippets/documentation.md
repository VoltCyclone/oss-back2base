### Working on documentation (profile: `documentation`)

**Save-worthy signals in writing work:** voice/audience decisions (internal engineer ≠ external dev ≠ PM), terminology the org has standardized on (and the variants to avoid), structural decisions (reference vs. tutorial vs. how-to split, per Diátaxis), changelog/release-note conventions, accessibility constraints (headings, alt text, link text), and information-architecture choices for specific doc sets.

- **feedback example:** *"Lead every how-to with the outcome, not the setup. **Why:** readers scan the first sentence to decide if the page is for them; hiding the outcome behind 'First, install X…' costs them the decision. **How to apply:** any procedural doc over one paragraph."*
- **project example:** *"Our public docs are Diátaxis-structured: `/tutorials/`, `/how-to/`, `/reference/`, `/explanation/`. **Why:** reader intent splits cleanly along those four; mixing them produced bounce rates we tracked for a quarter. **How to apply:** a new page must declare its quadrant before drafting."*
- **reference example:** *"Company style guide: docs.internal/style. Vale config mirrors it — run `vale` before submitting."*

**Conventions this profile assumes:** active voice, sentence case in headings, no orphaned link text ("click here"), examples before prose where possible. Use `brave-search` for terminology; `fetch` for authoritative external sources.
