# Issue tracker: docket

Issues for this repo are tracked with [docket](https://github.com/yfernandes/docket), installed at `./tasks/` (orphan git worktree on a `tasks` branch), driven via the `./task` symlink at the repo root.

## Conventions

- Issues are markdown files under `tasks/issues/<scope>/<slug>.md`, with YAML frontmatter (`id`, `status`, `priority`, `owner`, `tags`, `created_at`).
- Built-in scopes: `backend`, `frontend`, `libs`, `cms`.
- `status` is the triage state — see `docs/agents/triage-labels.md` for the role strings (docket already uses the canonical names natively).
- `./task` is the **only** sanctioned way to mutate task state — never hand-edit `assignments.yaml`, `flow.md`'s Active/Agent Queue sections, or issue frontmatter directly.

## When a skill says "create an issue" / "publish to the issue tracker"

```bash
./task new <scope> "<title>"                    # default issue.md template
./task new product "<title>" --template prd      # PRD-shaped issue
```

## When a skill says "triage an issue"

```bash
./task triage <task-id> <status>
```

## When a skill says "claim" / "start work on" an issue

```bash
./task claim <task-id> --owner <name>
# agents must also pass: --agent <agent-id> --lease <minutes>
```

## When a skill says "close" / "finish" an issue

```bash
./task close <task-id>              # mark done, archive
./task close <task-id> --wontfix    # archive as wontfix
./task release <task-id>            # hand back without closing
```

## When a skill says "fetch the relevant ticket" / "list open issues"

```bash
./task list --status needs-triage --json
```

Or read the issue file directly at `tasks/issues/<scope>/<slug>.md` if the id/path is already known.
