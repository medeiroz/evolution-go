---
name: coolify-cli
description: Use the `coolify` CLI to manage Coolify deployments — apps, databases, services, servers, projects, deploys, env vars, backups, and logs — on cloud or self-hosted instances. Activate whenever the user mentions Coolify, deploying this app to Coolify, checking deployment/app logs on the server, syncing `.env` to the deployed app, restarting/starting/stopping a deployed app or database, managing backups, switching Coolify contexts, or the `medeiroz` context. Reach for this even when the user just says "deploy it", "check the logs on prod", "redeploy", or "push the env vars" in a context where this app ships via Coolify — don't hand-write API calls or SSH when the CLI covers it.
---

# Coolify CLI

Manage Coolify (cloud or self-hosted) from the terminal: servers, projects, applications, databases, services, deployments, env vars, backups, domains, private keys.

## Project context (this repo)

**This project's Coolify context is `medeiroz`** (already created and set as default — see `README.md`). So plain `coolify …` commands target it. Only pass `--context <name>` to hit a different instance. Confirm with `coolify context list` / `coolify context verify` if a command behaves unexpectedly.

## Operating rules

- **Prefer `--format json`** for anything you need to parse (piping into `jq`, extracting a UUID). `table` (default) is for humans; `pretty` is indented JSON for debugging.
- **Resources are addressed by UUID, not numeric ID.** Get UUIDs from the matching `list` command. **Exception: teams use numeric IDs.**
- **Auth comes from the saved context.** Use `--token` only to override for a one-off.
- **Sensitive fields (tokens, passwords, IPs, emails) are hidden by default.** Add `--show-sensitive` (`-s`) to reveal them — and don't paste revealed secrets into logs or commits.
- Destructive commands (`delete`, `remove`, `deploy cancel`) prompt for confirmation; `--force`/`-f` skips it. Don't add `--force` unless the user asked to skip the prompt.

## Global flags

`--context <name>` · `--token <token>` · `--format table|json|pretty` · `--show-sensitive` (`-s`) · `--debug`

## The everyday loop

```bash
# What exists?
coolify context list                 # instances (contexts)
coolify server list
coolify project list
coolify resource list                # everything, mixed
coolify app list                     # apps only (also: service list, database list)

# Inspect one thing (needs its UUID from the list above)
coolify app get <uuid>

# Deploy
coolify deploy name <resource-name>          # deploy by human name (no UUID lookup)
coolify deploy uuid <uuid>                    # deploy by UUID
coolify deploy batch api,worker,frontend --force
coolify deploy list                           # in-progress deployments
coolify deploy cancel <deployment-uuid>

# App lifecycle
coolify app start <uuid>             # aliases: app deploy; flags --force (rebuild), --instant-deploy
coolify app stop <uuid>
coolify app restart <uuid>

# Logs (what actually broke)
coolify app logs <uuid> --follow                     # runtime logs, default last 100 lines (-n N)
coolify app deployments list <app-uuid>              # deployment history
coolify app deployments logs <app-uuid> --follow     # build/deploy logs, default all lines
```

## Environment variables

```bash
coolify app env list <app-uuid>
coolify app env get <app-uuid> <env-uuid-or-key>
coolify app env create <app-uuid> --key API_KEY --value secret123
coolify app env update <app-uuid> <env-uuid-or-key> --value new-secret
coolify app env delete <app-uuid> <env-uuid>

# Bulk-load a dotenv file. Updates existing keys, creates missing ones,
# and does NOT delete keys that aren't in the file.
coolify app env sync <app-uuid> --file .env.production --build-time --preview
```

`database env …` and `service env …` mirror these subcommands for databases and services.

## Databases & services

```bash
coolify database list
coolify database get <uuid>
coolify database create postgresql --project-uuid <uuid> --server-uuid <uuid> --environment-name production
coolify database start|stop|restart <uuid>

# Backups
coolify database backup list <db-uuid>
coolify database backup create <db-uuid> --frequency "0 2 * * *" --enabled --save-s3
coolify database backup trigger <db-uuid> <backup-uuid>       # run now

coolify service list
coolify service create --list-types                          # discover one-click service types
coolify service create <type> --project-uuid <uuid> --server-uuid <uuid> --instant-deploy
coolify service start|stop|restart <uuid>
```

DB types for `database create`: `postgresql`, `mysql`, `mariadb`, `mongodb`, `redis`, `keydb`, `clickhouse`, `dragonfly`.

## Contexts (switching instances)

```bash
coolify context list
coolify context verify                        # is the current context reachable + authed?
coolify context add <name> <url> <token>      # -d sets it as default
coolify context use <name>                    # switch default
coolify context set-token <name> <token>
```

## When you need the exact flags

The commands above are the common paths. For the **full catalog** — every subcommand,
every flag, defaults, and which are required (app creation from git/dockerfile/image,
storage mounts, GitHub App integration, backup retention, firewall/mesh alpha commands,
teams) — read **`references/command-catalog.md`** and grep for the command. Don't guess a
flag name; the catalog is the source of truth and covers cases this file deliberately omits.

## Install / update (already installed on this machine)

```bash
curl -fsSL https://raw.githubusercontent.com/coollabsio/coolify-cli/main/scripts/install.sh | bash
coolify update        # update the CLI itself
coolify version
```
