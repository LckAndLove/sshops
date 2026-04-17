# sshops-mcp

`sshops-mcp` is an MCP launcher for `sshops`.

It starts:

- `sshops mcp serve --transport stdio`

Target clients:

- Codex
- Claude Code

## Recommended Architecture

- Core implementation: Go binary (`sshops`)
- Distribution and one-command onboarding: Node/npm wrapper (`sshops-mcp`)

This keeps runtime performance and portability in Go, while providing a standard package install/update flow for users.

Windows x64 includes a bundled `sshops.exe`, so users can usually start with one `npx` command.

## Quick Start

Codex:

```bash
codex mcp add sshops -- npx -y sshops-mcp@0.2.1
```

Claude Code:

```bash
claude mcp add sshops -- npx -y sshops-mcp@0.2.1
```

If you do not need version pinning, you can use `sshops-mcp` without `@0.2.1`.

## Update

Windows:

```powershell
npm i -g sshops-mcp@latest; $bin=(npm prefix -g).Trim(); codex mcp remove sshops 2>$null; codex mcp add sshops -- "$bin\sshops-mcp.cmd"
```

macOS/Linux:

```bash
npm i -g sshops-mcp@latest && codex mcp remove sshops >/dev/null 2>&1 || true && codex mcp add sshops -- sshops-mcp
```

If you want controlled rollout, replace `@latest` with a fixed version (for example `@0.2.1`).

## Pass-through Args

Arguments after `--` are forwarded to `sshops mcp serve`.

Example:

```bash
npx -y sshops-mcp@0.2.1 -- --vault-password YOUR_PASSWORD
```

## Binary Resolution Order

At runtime, this launcher resolves `sshops` in order:

1. `SSHOPS_BIN`
2. `bundle/<platform>-<arch>/sshops(.exe)`
3. `bundle/sshops(.exe)`
4. `sshops` from system `PATH`

## Platform Notes

- Bundled binary: `win32-x64`
- Other platforms: install `sshops` in `PATH`, or set `SSHOPS_BIN`


