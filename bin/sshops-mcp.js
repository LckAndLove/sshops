#!/usr/bin/env node

const { spawnSync } = require("node:child_process");
const fs = require("node:fs");
const path = require("node:path");

function commandExists(command) {
  const checker = process.platform === "win32" ? "where" : "which";
  const result = spawnSync(checker, [command], { stdio: "ignore" });
  return result.status === 0;
}

function fileExists(targetPath) {
  if (!targetPath) {
    return false;
  }

  try {
    return fs.statSync(targetPath).isFile();
  } catch {
    return false;
  }
}

function resolveBinary() {
  const root = path.resolve(__dirname, "..");
  const ext = process.platform === "win32" ? ".exe" : "";

  const candidates = [
    process.env.SSHOPS_BIN,
    path.join(root, "bundle", `${process.platform}-${process.arch}`, `sshops${ext}`),
    path.join(root, "bundle", `sshops${ext}`),
  ].filter(Boolean);

  for (const candidate of candidates) {
    if (fileExists(candidate)) {
      return candidate;
    }
  }

  if (commandExists("sshops")) {
    return "sshops";
  }

  return "";
}

const binary = resolveBinary();
if (!binary) {
  console.error("[sshops-mcp] Cannot find sshops binary.");
  console.error("[sshops-mcp] Provide SSHOPS_BIN, bundle/<platform>-<arch>/sshops, or install sshops in PATH.");
  process.exit(1);
}

const extraArgs = process.argv.slice(2);
const launchArgs = ["mcp", "serve", "--transport", "stdio", ...extraArgs];

const result = spawnSync(binary, launchArgs, {
  stdio: "inherit",
  env: process.env,
});

if (result.error) {
  console.error(`[sshops-mcp] Failed to start: ${result.error.message}`);
  process.exit(1);
}

process.exit(result.status ?? 1);
