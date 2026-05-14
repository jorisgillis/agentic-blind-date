# Issue Tracker: GitHub

This repository uses **GitHub Issues** as its issue tracker.

## Configuration

- **Repository**: `jorisgillis/agentic-blind-date`
- **Tool**: `gh` CLI (GitHub CLI)
- **Issue creation**: `gh issue create`
- **Issue listing**: `gh issue list`

## Usage by Skills

Skills that interact with the issue tracker:
- `to-issues` — Creates issues from plans/specs
- `triage` — Processes and labels incoming issues
- `to-prd` — Creates PRDs as issues
- `qa` — Manages quality assurance workflows

## Commands Reference

```bash
# Create a new issue
gh issue create --title "..." --body "..." --label "..."

# List open issues
gh issue list --state open

# View issue details
gh issue view <number>

# Add labels to an issue
gh issue edit <number> --add-label "..."

# Close an issue
gh issue close <number>
```
