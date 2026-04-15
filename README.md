# sshops

轻量级、无侵入的 SSH 运维工具，支持批量执行与 AI 工具集成。

## 项目概览

`sshops` 是一个面向日常运维与自动化场景的命令行工具，通过标准 SSH 在远程主机上执行命令、传输文件并记录审计信息。

核心卖点：

- 零 Agent 接入：目标机器只需开启 SSH，不需要安装任何额外服务。
- AI 原生协作：内置 MCP Server，可直接接入 Claude Code、Claude Desktop 与团队 SSE 模式。

## 功能清单

- 非侵入式运维：不改造服务器环境，不部署驻留进程。
- 跨平台单二进制：支持 Windows、macOS、Linux。
- AI 友好：可作为 MCP Server 暴露工具能力。
- 批量并发执行：按分组和标签过滤目标主机并发运行。
- 安全凭据存储：本地 Vault 加密保存密钥/密码信息。
- 完整审计日志：执行记录写入 SQLite，便于追踪与复盘。

## 安装

### 方式一：下载预编译版本（推荐）

从 Releases 页面下载对应平台二进制：

https://github.com/LckAndLove/sshops/releases

Windows:

```bash
# Download and unzip release package, then add sshops.exe to PATH
```

macOS / Linux:

```bash
tar -xzf sshops-<os>-<arch>.tar.gz
sudo mv sshops /usr/local/bin/
sshops version
```

### 方式二：从源码构建

选项 A：使用 `go install`

```bash
go install github.com/LckAndLove/sshops@latest
sshops version
```

选项 B：`git clone` + `go build`

```bash
git clone https://github.com/LckAndLove/sshops.git
cd sshops
go build -o sshops .
./sshops version
```

Windows 构建示例：

```bash
go build -o sshops.exe .
.\sshops.exe version
```

带构建信息注入（可选）：

```bash
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse --short HEAD) -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o sshops .
```

## 快速开始

### 1. 添加主机到 inventory

```bash
sshops inventory add \
  --name prod-01 \
  --host 203.0.113.10 \
  --port 22 \
  --user root \
  --key ~/.ssh/id_rsa \
  --group prod \
  --tag env=prod,region=ap
```

### 2. 执行远程命令

```bash
sshops exec --host prod-01 "uname -a"
sshops exec --group prod --concurrency 20 "df -h"
sshops exec --group prod --tag env=prod "uptime"
```

### 3. 上传与下载文件

```bash
sshops upload --host prod-01 --src ./app.tar.gz --dst /tmp/app.tar.gz
sshops download --host prod-01 --src /var/log/syslog --dst ./logs/syslog
```

分组批量传输：

```bash
sshops upload --group prod --src ./scripts --dst /opt/scripts
sshops download --group prod --src /etc/hosts --dst ./collected-hosts
```

### 4. 集成 Claude Code

```bash
claude mcp add sshops -- /absolute/path/to/sshops mcp serve
```

验证可见工具后，即可在 Claude Code 中直接发起运维请求，例如批量巡检、日志收集与文件分发。

## 命令参考

- `sshops version`：显示版本、提交哈希、构建时间与 Go 版本。
- `sshops inventory add --name --host [flags]`：添加主机到清单。
- `sshops inventory list`：列出全部主机。
- `sshops inventory show --name <host-name>`：查看单台主机详细信息。
- `sshops inventory remove --name <host-name>`：删除主机并尝试清理 Vault 条目。
- `sshops exec --host <host> "<command>"`：对单主机执行命令。
- `sshops exec --group <group> "<command>"`：按分组并发执行命令。
- `sshops exec --group <group> --tag <k=v> "<command>"`：按分组与标签过滤执行。
- `sshops exec logs --limit <N>`：查看最近 N 条审计日志。
- `sshops upload --host <host> --src <local> --dst <remote>`：上传文件或目录。
- `sshops upload --group <group> --src <local> --dst <remote>`：按分组批量上传。
- `sshops download --host <host> --src <remote> --dst <local>`：下载文件或目录。
- `sshops download --group <group> --src <remote> --dst <local>`：按分组批量下载。
- `sshops mcp serve --transport stdio`：以 stdio 模式启动 MCP Server。
- `sshops mcp serve --transport sse --port 3000`：以 SSE 模式启动 MCP Server。

## AI 工具集成

### Claude Code

用于本地开发协同，推荐使用 `stdio` 方式接入。

```bash
claude mcp add sshops -- /absolute/path/to/sshops mcp serve --transport stdio
```

### Claude Desktop

在 Claude Desktop 的 MCP 配置中加入如下服务定义：

```json
{
  "mcpServers": {
    "sshops": {
      "command": "C:\\tools\\sshops.exe",
      "args": ["mcp", "serve", "--transport", "stdio", "--vault-password", "YOUR_VAULT_PASSWORD"]
    }
  }
}
```

### SSE 团队模式

适用于团队共享同一 MCP 服务端点，集中接入与统一权限管理更方便。

服务端启动：

```bash
sshops mcp serve --transport sse --port 3000
```

客户端配置示例：

```json
{
  "mcpServers": {
    "sshops-team": {
      "url": "http://your-server:3000/sse"
    }
  }
}
```

## 配置文件说明

默认配置文件路径：

- Windows: `%APPDATA%\sshops\config.yaml`
- macOS/Linux: `~/.sshops/config.yaml`

示例配置：

```yaml
default_user: root
default_port: 22
default_key_path: ~/.ssh/id_rsa
connect_timeout: 30
inventory_path: ~/.sshops/inventory.yaml
vault_path: ~/.sshops/vault.enc
audit_db_path: ~/.sshops/audit.db
```

字段解释：

- `default_user`：默认 SSH 用户名。
- `default_port`：默认 SSH 端口。
- `default_key_path`：默认私钥路径。
- `connect_timeout`：连接超时秒数。
- `inventory_path`：主机清单文件路径。
- `vault_path`：本地加密凭据文件路径。
- `audit_db_path`：审计日志 SQLite 数据库路径。

补充说明：

- 可通过全局参数 `--config` 指定自定义配置文件位置。
- 未配置字段会自动回退到程序内置默认值。
- 建议将 `vault.enc` 与 `audit.db` 放在受权限控制的目录。

## 安全与审计建议

- 优先使用密钥认证，减少明文密码输入。
- 为生产环境主机配置分组与标签，降低误操作范围。
- 使用 `sshops exec logs --limit 100` 定期复核关键操作。
- 在团队场景下，优先采用 SSE 服务集中化接入并配合网络访问控制。

## License

This project is licensed under the MIT License.
