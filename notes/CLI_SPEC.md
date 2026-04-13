# CLI_SPEC.md

## Purpose

This document is the authoritative contract for the `dploy` command-line
interface. Anything the CLI does in v0.1 is documented here; anything not
documented here is not part of the contract.

The CLI is the primary product interface. It must remain:

- simple
- predictable
- explicit
- scriptable

---

## Design Principles

- One clear command for one clear action
- Human-readable output by default, machine-readable under `--json`
- Stable exit codes
- No hidden automation
- No interactive prompts
- Config declares intent; trusted policy declares what is allowed

---

## Command Structure

```bash
dploy [command] <positional args> [flags]
```

Commands use Cobra conventions. Global flags may appear before or after
positional args.

---

## Commands

### `dploy up <environment>`

Run the environment's `deploy:` steps.

**Args:**
- `<environment>` (required) — name from `environments:` in config

**Behavior:**
- Loads config, validates it, resolves the named environment
- Evaluates trusted policy
- Connects to each target (local or SSH)
- Runs each `deploy:` step in order against its scoped targets (honoring `on:` / `on_role:`)
- Streams output
- Stops on first step failure
- Records the run under `.dploy/state/`
- Prints environment `notes:` after success (never executes them)

**Exit codes:** `0` success, `5` step failure, `1` other.

---

### `dploy <environment>`

Shorthand for `dploy up <environment>`. Activates only when the positional
argument matches a configured environment. Anything else falls through as
an unknown command.

---

### `dploy capture <environment>`

Run the environment's `capture:` workflow(s) to produce a snapshot.

**Args:**
- `<environment>` (required)

**Flags:**
- `--resource <name[,name...]>` — resource(s) to capture. Auto-picks when
  exactly one is defined in the environment's `capture:` block. Required
  when multiple are defined. Errors with the list of available names.

**Behavior:**
- Loads config, resolves environment, evaluates policy
- Generates a snapshot ID (`<env>-<YYYYMMDD>-<HHMMSS>-<6 hex>`)
- Runs each resource's workflow in the order declared in config
- Scripts run locally on the invoking machine
- Records snapshot metadata under `.dploy/snapshots/<env>/<id>.json`
- After success, prints a hint with the exact `dploy restore` command

**Script environment variables:**
- `DPLOY_SOURCE` — environment name
- `DPLOY_SOURCE_CLASS` — environment class
- `DPLOY_RESOURCES` — comma-separated resource list
- `DPLOY_SNAPSHOT_ID` — the snapshot ID (scripts should tag their output with this)

Scripts are responsible for actually persisting captured data
(mysqldump, gcloud sql export, tar + scp, etc.). dploy only orchestrates
and records metadata.

---

### `dploy restore <snapshot-id> <environment>`

Apply a previously captured snapshot to the named environment.

**Args:**
- `<snapshot-id>` (required) — id produced by `dploy capture`
- `<environment>` (required) — target env to restore into

**Flags:**
- `--resource <name[,name...]>` — same auto-pick rules as `capture`

**Behavior:**
1. Loads config, resolves environment, evaluates policy
2. Locates the snapshot in `.dploy/snapshots/` (searches all envs so any ID works)
3. **Takes a safety snapshot** of the target's current state using the
   target env's `capture:` workflow for the same resources. Silently skipped
   if the target defines no matching capture workflow. Safety snapshots are
   recorded under `.dploy/snapshots/<env>/` with an id prefixed `safety-<env>-...`.
4. Runs the target env's `restore:` workflow(s)
5. On failure, prints the safety snapshot's restore command so the operator
   can revert manually

**Script environment variables:**
- `DPLOY_TARGET` — target environment name
- `DPLOY_TARGET_CLASS` — target environment class
- `DPLOY_RESOURCES` — comma-separated resource list
- `DPLOY_SNAPSHOT_ID` — the snapshot id being restored
- `DPLOY_SNAPSHOT_ENV` — the environment that snapshot was captured from

---

### `dploy rollback <environment>`

Run the environment's `rollback:` steps. Symmetrical to `up` — user
defines what rollback means for their app. No auto-generated inverse.

**Behavior:**
- Errors with exit code `6` if no `rollback:` block is defined
- Otherwise identical lifecycle to `up`

---

### `dploy snapshots <environment>`

List snapshots captured for the environment, newest first.

**Output columns:** `ID`, `STATUS`, `CREATED`, `RESOURCES`.

---

### `dploy status <environment>`

Show the last recorded operation for the environment.

---

### `dploy logs <environment>`

Show step-by-step output from the last recorded operation for the
environment.

---

### `dploy validate`

Parse and semantically check the config file. Does not connect to
hosts, evaluate policy, or execute anything.

**Exit codes:** `0` valid, `2` invalid.

---

### `dploy version`

Print the CLI version.

---

## Global Flags

| Flag | Default | Meaning |
|---|---|---|
| `--file` | `dploy.yml` | Path to the config file |
| `--policy` | `/etc/dploy/policy.yml` | Path to trusted policy file (missing default path is silently OK — empty policy = allow) |
| `--verbose` | false | Print commands, connection info, step-by-step execution |
| `--quiet` | false | Reduce output to essentials (errors still print) |
| `--json` | false | Machine-readable JSON for commands that support it (`status`, `snapshots`, `logs` initially) |
| `--confirm` | false | Acknowledge policy `confirm` requirements for this invocation |
| `--sanitized` | false | Assert data has been sanitized (satisfies policy `sanitization` requirements) |

---

## Exit Codes

| Code | Name | Meaning |
|---|---|---|
| 0 | success | Operation completed successfully |
| 1 | general failure | Uncategorized error |
| 2 | invalid config | Config file missing, unparseable, or semantically invalid |
| 3 | environment missing | Named environment not found in config |
| 4 | connection failure | Could not connect to a target (SSH, etc.) |
| 5 | step failure | A step exited non-zero |
| 6 | rollback unavailable | `rollback:` block not defined for the environment |
| 7 | policy denied | Trusted policy denied or has unmet requirements |

Exit codes are stable. Once a code is assigned a meaning, it does not change.

---

## State Storage

dploy persists per-project state under `.dploy/` in the current
working directory:

- `.dploy/state/<env>/<timestamp>.json` — one record per operation
  (up, capture, restore, rollback). The latest is used by `status` and `logs`.
- `.dploy/snapshots/<env>/<id>.json` — snapshot metadata (id, resources,
  status, sanitization flag, created/finished timestamps, policy source).
- `.dploy/artifacts/` — conventional location for snapshot payloads
  (scripts choose whether to put dumps/tarballs here).

All three directories are plain JSON on disk. No database, no daemon,
no remote storage required.

---

## Output Rules

Default (non-quiet, non-verbose) output is:

```
Running up for environment: local
Target: drupal (local)
Path: ./drupal
Target: nextjs (local)
Path: ./nextjs

[1/3] [drupal] ddev start
[2/3] [drupal] ddev composer install
[3/3] [nextjs] npm install

Deploy succeeded

Notes:
  - Next.js dev server: cd nextjs && npm run dev
```

Errors include the failing step and the underlying command's output.

---

## Command Behavior Rules

1. **No hidden automation.** A command only does what is documented here.
   `up` does not auto-rollback on failure. `capture` does not auto-restore.
   `restore` takes a safety snapshot because that is documented behavior,
   not because it's clever.
2. **No interactive prompts.** Scripts must be runnable in CI.
3. **Stop on first failure.** A step's non-zero exit halts the operation.
4. **Show the current step.** The user always knows which step is running.
5. **No environment guessing.** Commands that take an environment require it
   explicitly; `dploy` without args prints help.

---

## Non-Goals (v0.1)

- Team/user management
- Infrastructure provisioning
- Preview environment creation
- Secret storage
- Daemon / API server
- Auto-revert of failed restores (v0.1 prints the safety snapshot ID
  so operators can revert manually; auto-revert is future work)
- `dploy list`, `dploy doctor`, `dploy plan`, `dploy explain` (future)

---

## Philosophy Reminder

The CLI is a thin, explicit command surface for running workflows the
user already trusts. If a feature starts to feel like a platform, it
does not belong here.
