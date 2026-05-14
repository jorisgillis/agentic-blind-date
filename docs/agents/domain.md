# Domain Docs

This repository uses a **single-context** layout for domain documentation.

## Layout

- **Primary context**: `CONTEXT.md` at repository root
- **Architecture decisions**: `docs/adr/` directory at repository root
- **Additional docs**: Any other markdown files in `docs/` or root

## Consumer Rules

Skills that read domain documentation follow these rules:

### For `CONTEXT.md`
- Location: `/CONTEXT.md` (repository root)
- Purpose: Project domain language, ubiquitous language, key concepts
- Read by: `improve-codebase-architecture`, `diagnose`, `tdd`

### For ADRs (Architecture Decision Records)
- Location: `/docs/adr/` (repository root)
- Format: Standard ADR format (e.g., `0001-record-architecture-decisions.md`)
- Read by: `improve-codebase-architecture`

### For Other Documentation
- Skills may also reference:
  - `README.md` at root for project overview
  - `AGENTS.md` or `CLAUDE.md` for agent instructions
  - Any files in `docs/` for additional context

## Creating Domain Docs

If you don't already have these files, create them with:

```bash
# Create CONTEXT.md
touch CONTEXT.md

# Create ADR directory and first decision record
mkdir -p docs/adr
touch docs/adr/0001-record-architecture-decisions.md
```

## Single-Context vs Multi-Context

This is configured as **single-context**, meaning:
- One unified domain language across the repository
- One `CONTEXT.md` file
- One `docs/adr/` directory

For monorepos with separate domains (frontend/backend), use `CONTEXT-MAP.md` instead to point to per-context files.
