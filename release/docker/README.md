# CLIProxyAPI Docker Release Bundle

This bundle is intended for direct deployment from the GitHub Release page.

## Files

- `.env.example`: image tag and runtime environment defaults
- `docker-compose.yml`: Docker deployment template
- `config/config.yaml`: starter runtime config
- `data/`: persistent mount points

## Quick Start

```bash
cp .env.example .env
docker compose up -d
```

Open the management page after startup:

- `http://127.0.0.1:8317/management.html`

## Required Changes Before First Use

- Change `remote-management.secret-key` in `config/config.yaml`
- Change `api-keys` in `config/config.yaml`

## Optional Usage Persistence

Enable persistence by setting:

```yaml
usage-persistence-file: /workspace/usage-backups/usage-statistics.json
```

The file will be stored on the host at:

- `./data/usage-backups/usage-statistics.json`

## Remote Access

To allow remote access to the management UI:

1. Change `remote-management.allow-remote` to `true`
2. Change the port mapping in `docker-compose.yml` from `127.0.0.1:${CLI_PROXY_PUBLIC_PORT:-8317}:8317` to `${CLI_PROXY_PUBLIC_PORT:-8317}:8317`
