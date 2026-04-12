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
- environment sync
- rollback
- capture
- restore
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

## Example commands

```bash
dploy deploy staging
dploy deploy production

dploy env sync production local-jesse --resource database
dploy env sync staging-main local-jesse --resource files

dploy capture production --resource database
dploy restore snapshot-123 local-jesse --resource database

dploy rollback production
dploy status production
dploy logs production
dploy validate
```

---

## Why environment sync matters

This is one of the main reasons `dploy` exists.

Most deployment tools stop at code delivery.

`dploy` also treats environment sync as a first-class operation:

- production → staging
- production → local
- staging → local
- local → staging

With:

- explicit commands
- resource scope
- policy checks
- sanitization requirements
- logs and records

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
- env sync
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
