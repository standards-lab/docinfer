# docinfer standards

The judgement calls the standards-reviewer applies to docinfer, beyond what `mise run check`
enforces: each one line, with a pointer to the architecture page that applies. The design task of
v1.ai.inference writes them; until then the full Go Elemental standard applies.

- `architecture/standards/go-elemental/principles/dependencies.md`: the base module requires
  docproc and go-ai only; nothing at or below go-ai's tier imports docinfer.
