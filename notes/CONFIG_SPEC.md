# CONFIG_SPEC.md

## Purpose

Defines `deploy.yml` structure for `dploy`.

---

## Structure

app: string

environments:
  <name>:
    strategy: sequential | parallel | rolling
    targets:
      <target>:
        type: local | ssh
        host: string (ssh only)
        path: string
        roles: [string]

    secrets: (optional, see SECRETS.md)

    data: (optional, see ENV_SYNC.md)
      # workflow definitions only (no policy)

    deploy:
      - run: command
        on: [targets] (optional)
        on_role: role (optional)

    notes: (optional)
      - string  # printed after a successful `up`, never executed

    rollback: (optional)

---

## Policy Integration

`deploy.yml` may describe sync workflows, but **must not be the sole authority**
for high-risk operations.

dploy will also load a **trusted policy file** (e.g. `/etc/dploy/policy.yml`).

Merge rules:
- policy can restrict or require additional conditions
- project config cannot override policy restrictions

---

## Notes

- topology is derived from targets and roles
- data workflows are script-based
- secrets are referenced only
- policy is external by design
- `notes:` entries are displayed to the operator after a successful `up` and
  never executed — use them for follow-up actions a human needs to take
  (e.g. starting a dev server in a watchable terminal)

---

## Rules

- no DSL
- no logic engine
- no secret values
- no data storage logic
- no policy enforcement inside repo config
