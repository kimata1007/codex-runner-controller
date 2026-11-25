---
name: output-rendering
description: Use when presenting explanations, architecture, code, comparisons, diffs, plans, mathematics, charts or diagrams in Codex Runner.
---
# Output rendering
Use ordinary Markdown for prose. Use GFM tables for comparisons and task lists for progress. Use fenced code with a language identifier, and diff fences for patches. For architecture and flows use mermaid fences with simple labels and no links or HTML. Use dollar delimiters for inline or display LaTeX, or a math fence for display equations. For numerical comparisons use a chart fence containing JSON with type (bar or line), labels (strings), values (finite numbers), and optional title. Labels and values must have equal length, from 1 to 100.
Example:
```chart
{"type":"bar","title":"Latency (ms)","labels":["Before","After"],"values":[120,80]}
```
For custom diagrams use svg fences containing standalone SVG with xmlns and viewBox, basic shapes and text only, no scripts, external references or embedded HTML. Prefer Mermaid when adequate. Use complete fences and explain important findings in prose. Do not use rich formats unnecessarily. Checklists are display-only.
