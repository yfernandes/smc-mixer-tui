# Triage Labels

The skills speak in terms of five canonical triage roles. docket's built-in issue `status` field already uses these exact strings natively — no mapping needed.

| Role in skills   | docket `status` value | Meaning                                  |
| ----------------- | ---------------------- | ----------------------------------------- |
| `needs-triage`    | `needs-triage`          | Maintainer needs to evaluate this issue  |
| `needs-info`      | `needs-info`            | Waiting on reporter for more information |
| `ready-for-agent` | `ready-for-agent`       | Fully specified, AFK-ready               |
| `ready-for-human` | `ready-for-human`       | Requires human implementation            |
| `wontfix`         | `wontfix`               | Will not be actioned                     |

Set via `./task triage <task-id> <status>`.
