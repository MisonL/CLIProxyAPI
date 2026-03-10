# CLIProxyAPI Docker Release Bundle

这个发布包用于直接从 GitHub Release 下载后部署，不依赖源码目录。

## 文件说明

- `.env.example`：镜像标签、平台数据层与容器名默认值
- `docker-compose.yml`：server + worker + PostgreSQL + Redis + NATS 模板
- `config/config.yaml`：运行配置模板
- `data/`：日志、静态页、usage 备份、PostgreSQL、Redis、NATS 数据目录

平台数据库现在是凭证运行时唯一真源；旧的文件型 bootstrap 目录不再参与新版本运行。

## 快速启动

```bash
cp .env.example .env
docker compose up -d
```

启动后访问：`http://127.0.0.1:8317/management.html`

首次使用前至少要改：

- `config/config.yaml` 里的 `remote-management.secret-key`
- `config/config.yaml` 里的 `api-keys`
- `.env` 里的 `CPA_MASTER_KEY`

## 旧版 Release 迁移

旧凭证 JSON 只作为迁移源，不会被新服务直接读取。推荐流程：

1. 停旧服务前，先备份旧凭证目录并导出 usage 统计。
2. 下载同一后端 tag 对应的 `CLIProxyAPI-migrate` 二进制，并确保本机可执行。
3. `cp .env.example .env` 后先执行 `docker compose up -d postgres`，只启动数据库。
4. 运行本地迁移命令：

```bash
CLIProxyAPI-migrate credentials \
  --dir ./migration-backup/legacy-credentials \
  --database-url 'postgresql://postgres:postgres@127.0.0.1:15432/cliproxy?sslmode=disable' \
  --database-schema controlplane \
  --master-key '<与 .env 中 CPA_MASTER_KEY 相同>' \
  --tenant-slug default \
  --tenant-name 'Default Tenant' \
  --workspace-slug default \
  --workspace-name 'Default Workspace'
```

5. 导入成功后再执行 `docker compose up -d` 启动完整服务。
6. 最后把旧 usage 导出文件导入新服务管理接口。

如果你就在本仓工作区里迁移本机旧容器，优先使用 `deploy/scripts/migrate-release-local.sh`；它会自动完成备份、停旧服务、先起 PostgreSQL、执行本地 CLI 导入，再恢复 usage。

## Usage 持久化

默认关闭。开启方式：

```yaml
usage-persistence-file: /workspace/usage-backups/usage-statistics.json
```

宿主机文件会落到：`./data/usage-backups/usage-statistics.json`

## 远程访问

如需远程访问管理面：

1. 把 `remote-management.allow-remote` 改为 `true`
2. 把 `docker-compose.yml` 中 `127.0.0.1:${CLI_PROXY_PUBLIC_PORT:-8317}:8317` 改为 `${CLI_PROXY_PUBLIC_PORT:-8317}:8317`


## 维护者打包

仓库内 `dist/docker-release` 与 `dist/CLIProxyAPI_docker_release_<version>.tar.gz` 由源码模板生成，维护时执行：

```bash
./release/build-docker-release.sh <version>
```
