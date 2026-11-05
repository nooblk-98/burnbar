#!/usr/bin/env node
'use strict';

const path = require('node:path');
const { spawnSync } = require('node:child_process');
const pkg = require('../../package.json');

const help = `BurnBar installer ${pkg.version}

Usage: burnbar [install|update] [options]
  --channel stable|development  Release channel (default: matches installer package)
  --install-dir <directory>     Override Windows install folder or macOS app parent folder
  --no-start                    Install without launching BurnBar
  --dry-run                     Check release metadata without modifying the installation
  --help                        Show this help
  --version                     Show the installer package version

Install and update use the same verified replacement process. Switching channels
replaces the executable but preserves your settings and history.
Requires Node.js 22+. Windows: PowerShell 5.1. macOS preview: macOS 15+, Apple Silicon or Intel.
`;

function parseArgs(args) {
  const options = { channel: pkg.version.includes('-dev.') ? 'development' : 'stable' };
  const seen = new Set();
  if (args[0] === 'install' || args[0] === 'update') args = args.slice(1);
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (seen.has(arg)) throw new Error(`Duplicate option: ${arg}`);
    seen.add(arg);
    switch (arg) {
      case '--channel':
      case '--install-dir': {
        const value = args[++i];
        if (!value || value.startsWith('-')) throw new Error(`${arg} needs a value`);
        if (arg === '--channel') {
          if (!['stable', 'development'].includes(value)) throw new Error('Channel must be stable or development');
          options.channel = value;
        } else {
          options.installDir = path.resolve(value);
        }
        break;
      }
      case '--no-start': options.noStart = true; break;
      case '--dry-run': options.dryRun = true; break;
      case '--help': options.help = true; break;
      case '--version': options.version = true; break;
      default: throw new Error(`Unknown argument: ${arg}. Use --help for usage.`);
    }
  }
  return options;
}

function invocation(options, env = process.env) {
  if (!env.SystemRoot || !path.win32.isAbsolute(env.SystemRoot)) {
    throw new Error('Cannot locate Windows PowerShell: SystemRoot is missing or invalid');
  }
  // Use the system executable and a bundled script. Never fetch/execute a remote
  // script or interpolate user input into a PowerShell command string.
  const executable = path.win32.join(env.SystemRoot, 'System32', 'WindowsPowerShell', 'v1.0', 'powershell.exe');
  const args = ['-NoLogo', '-NoProfile', '-NonInteractive', '-ExecutionPolicy', 'Bypass',
    '-File', path.resolve(__dirname, '../windows/install.ps1'), '-Channel', options.channel];
  if (options.installDir) args.push('-InstallDir', options.installDir);
  if (options.noStart) args.push('-NoStart');
  if (options.dryRun) args.push('-WhatIf');
  return { executable, args };
}

function main(args = process.argv.slice(2), runtime = { platform: process.platform, env: process.env, spawnSync }) {
  try {
    const options = parseArgs(args);
    if (options.help) { console.log(help); return 0; }
    if (options.version) { console.log(pkg.version); return 0; }
    if (runtime.platform === 'darwin') { return require('../macos/install.cjs').install(options).then(() => 0).catch(error => { console.error(`BurnBar: ${error.message}`); return 1; }); }
    if (runtime.platform !== 'win32') throw new Error('BurnBar requires Windows or macOS. No files were installed.');
    const { executable, args: psArgs } = invocation(options, runtime.env);
    // PowerShell 7 can pass its incompatible module search path to Windows
    // PowerShell 5.1 (e.g. when npx is launched from pwsh). Let 5.1 construct
    // its own module path so standard cmdlets such as Get-FileHash resolve.
    const childEnv = { ...runtime.env };
    for (const key of Object.keys(childEnv)) {
      if (key.toLowerCase() === 'psmodulepath') delete childEnv[key];
    }
    const result = runtime.spawnSync(executable, psArgs, {
      stdio: 'inherit', shell: false, windowsHide: true, env: childEnv,
    });
    if (result.error) throw new Error(`Could not run Windows PowerShell: ${result.error.message}`);
    return result.status ?? 1;
  } catch (error) {
    console.error(`BurnBar: ${error.message}`);
    return 1;
  }
}

if (require.main === module) Promise.resolve(main()).then(code => { process.exitCode = code; });
module.exports = { parseArgs, invocation, main };
