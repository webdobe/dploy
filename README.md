# dploy

A thin, open-source operations CLI for deploying code and moving environment data without locking you into a platform.

`dploy` connects your repo, servers, scripts, and existing tools into one clear workflow.

It does not try to replace them.

---

## Why dploy exists

Most teams already have the pieces:

- Git
- SSH
- Docker
- shell scripts
- CI
- backup scripts
- ad hoc deploy commands
- one-off “get prod DB locally” workflows

What they usually do **not** have is a clean, consistent interface for running those workflows across environments.

That is what `dploy` is for.

It gives you one model for:

- deploy
- capture
- restore
- rollback
- status
- logs

Without turning your stack into a platform.

---

## What makes it different

`dploy` is built around a few hard rules:

- explicit operations only
- one model for single-host and multi-host setups
- repo config defines requested behavior
- trusted policy defines allowed behavior
- no hidden behavior
- no secrets storage
- no data storage
- no workflow engine
- no lock-in

If a shell script already does the job well, `dploy` should orchestrate it instead of replacing it.

---

## The core idea

Everything in `dploy` follows the same basic shape:

environment → targets → steps

That same model works for:

- one local app
- one remote server
- multiple web and worker hosts
- production to local sync workflows
- rollback and restore workflows later

Single-host and multi-host do not need separate products or separate mental models.

---

## The real problem it solves

Deploying code is only part of the job.

Real teams also need to do things like:

- deploy staging
- deploy production
- pull a sanitized production database to local
- sync files from staging to local
- capture a backup before a risky deploy
- restore from a known snapshot
- see what last ran and whether it failed

Most of those workflows end up scattered across:

- CI YAML
- Bash history
- wiki docs
- local scripts
- tribal knowledge

`dploy` gives those workflows one consistent interface.

---

## What dploy is

`dploy` is:

- a CLI
- an orchestrator
- step-driven
- environment-aware
- policy-aware
- script-first
- portable across stacks

---

## What dploy is not

`dploy` is not:

- a PaaS
- a CI/CD platform
- a cloud abstraction layer
- a container orchestrator
- a secrets manager
- a backup storage system
- a database replication tool
- a control plane you are forced to adopt

It is glue.

Good glue, but still glue.

---

## Install

Requires Go 1.21 or newer.

```bash
go install github.com/webdobe/dploy/cmd/dploy@latest
```

The binary lands in `$(go env GOPATH)/bin`. If `dploy` isn't found after install, that directory isn't on your `PATH` — add it:

```bash
export PATH="$(go env GOPATH)/bin:$PATH"
```

(Add the same line to `~/.zshrc` or `~/.bashrc` to persist it.)

Prebuilt binaries and a Homebrew tap are planned once the CLI stabilizes.

---

## Golden path

A real walkthrough: a Drupal + Next.js project with a Google Cloud SQL
MySQL backend and a local DDEV setup. Save this as `dploy.yml` at the
project root:

```yaml
app: efe

environments:
  local:
    class: local
    targets:
      nextjs:
        type: local
        path: ./nextjs
        roles: [web]
      drupal:
        type: local
        path: ./drupal
        roles: [cms]
    deploy:
      - run: "npm install"
        on: [nextjs]
      - run: "ddev start"
        on: [drupal]
      - run: "ddev composer install"
        on: [drupal]
    notes:
      - "Next.js dev server: cd nextjs && npm run dev"
    capture:               # used as the safety snapshot before restore
      database:
        - "mkdir -p ./.dploy/artifacts"
        - "cd drupal && ddev export-db --file=../.dploy/artifacts/$DPLOY_SNAPSHOT_ID.sql.gz"
    restore:
      database:
        - "cd drupal && ddev import-db --file=../.dploy/artifacts/$DPLOY_SNAPSHOT_ID.sql.gz"

  production:
    class: production
    targets:
      web:
        type: local
        path: .
    capture:
      database:
        - "gcloud sql export sql my-instance gs://my-bucket/dploy/$DPLOY_SNAPSHOT_ID.sql.gz --project=my-project --database=my_db"
        - "gsutil cp gs://my-bucket/dploy/$DPLOY_SNAPSHOT_ID.sql.gz ./.dploy/artifacts/$DPLOY_SNAPSHOT_ID.sql.gz"
```

### Validate the config

```bash
dploy validate
```

### Bring up the local environment

```bash
dploy local
```

That's shorthand for `dploy up local` — installs Next.js deps, starts
DDEV, runs `composer install` inside the Drupal container, then prints
the notes block so you remember what still needs a watchable terminal.

### Capture the prod database

```bash
dploy capture production
```

`--resource` is auto-picked because `production.capture:` defines only
one. dploy prints a `To restore:` hint with the exact next command.

### Restore into local

```bash
dploy restore production-20260412-223303-31f222 local
```

Before running your restore workflow, dploy runs the `local.capture:`
workflow as a safety snapshot of your current local DB. If the main
restore fails for any reason, the safety snapshot's ID is printed so
you can revert with another `dploy restore`.

### List snapshots

```bash
dploy snapshots production
```

Shows snapshot IDs, status, creation time, and resources. Both captured
snapshots and safety snapshots appear here.

---

## Other commands

```bash
dploy rollback production    # runs user-defined rollback: steps
dploy status local           # last run for this env
dploy logs local             # step-by-step output of the last run
dploy version
```

---

## Why capture and restore matter

This is one of the main reasons `dploy` exists.

Most deployment tools stop at code delivery.

`dploy` also treats moving environment data as a first-class pair of operations: **capture** (take a snapshot of a resource in one environment) and **restore** (apply a captured snapshot into another).

Typical flows:

- capture production database → restore into local
- capture staging files → restore into local
- capture production before a risky deploy → restore if it goes wrong

With:

- explicit commands
- resource scope
- policy checks
- sanitization attestation
- logs and records
- a durable, re-runnable snapshot artifact

`dploy` does NOT become a database tool or storage system. It orchestrates approved workflows and lets policy decide what is allowed.

---

## Trusted policy

High-risk operations should not rely on repo config alone.

In `dploy`:

- project config describes what the project wants to do
- trusted policy describes what is allowed
- infrastructure still provides the final hard boundary

---

## Example config

```yaml
app: my-app

environments:
  production:
    class: production
    strategy: sequential

    targets:
      web-1:
        type: ssh
        host: web1.example.com
        path: /var/www/app
        roles: [web]

      worker-1:
        type: ssh
        host: worker1.example.com
        path: /var/www/app
        roles: [worker]

    deploy:
      - run: git pull
      - run: docker compose up -d
        on_role: web
      - run: php artisan queue:restart
        on_role: worker

    notes:
      - "tail logs: ssh web-1 'docker compose logs -f app'"

    data:
      sync_to_local:
        - ./scripts/get-sanitized-dump.sh
        - ./scripts/download-assets.sh
        - ./scripts/restore-local.sh
```

---

## Status

Early stage.

Focus is on:

- clean CLI
- strong config model
- trusted policy support
- deploy
- capture / restore
- logs
- status
- honest failure behavior

---

## Design philosophy

If `dploy` ever starts to feel like a platform instead of a tool, it is going in the wrong direction.

The job is simple:

- orchestrate operations
- stay explicit
- stay portable
- stay understandable
