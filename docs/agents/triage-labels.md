# Triage Labels

This repository uses the **default GitHub label vocabulary** for issue triage.

## Label Mapping

| Role | Label | Description |
|------|-------|-------------|
| Needs evaluation | `needs-triage` | Maintainer needs to evaluate the issue |
| Waiting on reporter | `needs-info` | Issue is blocked waiting for more information from the reporter |
| Ready for agent | `ready-for-agent` | Fully specified, an AFK agent can pick this up with no human context |
| Ready for human | `ready-for-human` | Needs human implementation |
| Won't fix | `wontfix` | Will not be actioned |

## Usage

The `triage` skill applies these labels automatically as issues move through the triage state machine:

1. New issues start with `needs-triage`
2. If more info is needed → `needs-info`
3. If fully specified for AFK work → `ready-for-agent`
4. If requires human judgment → `ready-for-human`
5. If declined → `wontfix`

## Creating Labels

If these labels don't exist in your repository yet, create them with:

```bash
# Create all triage labels
gh label create needs-triage --description "Maintainer needs to evaluate" --color "BFD4F2"
gh label create needs-info --description "Waiting on reporter for more information" --color "D4C5F9"
gh label create ready-for-agent --description "Fully specified, ready for AFK agent" --color "C2E0C6"
gh label create ready-for-human --description "Needs human implementation" --color "FBE983"
gh label create wontfix --description "Will not be actioned" --color "E06C75"
```

Color suggestions follow GitHub's default palette for consistency.
