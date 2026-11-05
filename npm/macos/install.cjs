'use strict';
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const API = 'https://api.github.com/repos/nooblk-98/burnbar';
const headers = { 'User-Agent': 'BurnBar-Installer', Accept: 'application/vnd.github+json' };
async function response(url) {
  const r = await fetch(url, { headers, signal: AbortSignal.timeout(60000) });
  if (!r.ok) throw new Error(`Download failed: HTTP ${r.status}`);
  return r;
}
function run(command, args, extra = {}) {
  const r = spawnSync(command, args, { shell: false, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, ...extra });
  if (r.error || r.status !== 0) throw new Error(`${path.basename(command)} failed${r.error ? ': ' + r.error.message : ''}`);
  return r;
}
function assetURL(asset) {
  const u = new URL(asset.browser_download_url);
  if (u.protocol !== 'https:' || u.hostname !== 'github.com' || u.username || u.password || !u.pathname.startsWith('/nooblk-98/burnbar/releases/download/')) throw new Error('Unexpected release asset URL');
  return u.toString();
}
async function releaseFor(channel) {
  if (channel === 'stable') return (await response(`${API}/releases/latest`)).json();
  for (let page = 1; page <= 10; page++) {
    const entries = await (await response(`${API}/releases?per_page=100&page=${page}`)).json();
    const release = entries.find(r => !r.draft && r.prerelease && /^dev-\d+-\d+$/.test(r.tag_name));
    if (release) return release;
    if (entries.length < 100) break;
  }
  throw new Error('No development release found');
}
async function install(options, runtime = { arch: process.arch }) {
  const arch = { arm64: 'arm64', x64: 'amd64' }[runtime.arch];
  if (!arch) throw new Error('macOS installer supports Apple Silicon and Intel only');
  const release = await releaseFor(options.channel);
  const name = `burnbar-macos-${arch}.zip`;
  const asset = release.assets.find(a => a.name === name);
  const checksum = release.assets.find(a => a.name === name + '.sha256');
  if (!asset || !checksum) throw new Error(`Release ${release.tag_name} has no macOS preview yet. No files were installed.`);
  const parent = options.installDir || path.join(os.homedir(), 'Applications');
  const target = path.join(parent, 'BurnBar.app');
  console.log(`${release.tag_name}: ${name} → ${target}`);
  if (options.dryRun) return;
  // Never replace an app while it might still be running. No process-name kills.
  const pids = spawnSync('/usr/bin/pgrep', ['-f', path.join(target, 'Contents/MacOS/burnbar')], { encoding: 'utf8', shell: false });
  if (pids.status === 0) throw new Error('Quit BurnBar before updating, then run this command again.');
  await fs.mkdir(parent, { recursive: true });
  const stage = await fs.mkdtemp(path.join(parent, '.burnbar-stage-'));
  let backedUp = false;
  const backup = path.join(stage, 'previous.app');
  try {
    const data = Buffer.from(await (await response(assetURL(asset))).arrayBuffer());
    if (data.length > 32 * 1024 * 1024) throw new Error('Unexpectedly large archive');
    const sum = await (await response(assetURL(checksum))).text();
    const hash = crypto.createHash('sha256').update(data).digest('hex');
    if (sum.trim().split(/\s+/)[0].toLowerCase() !== hash) throw new Error('Archive checksum mismatch');
    if (asset.digest && asset.digest !== `sha256:${hash}`) throw new Error('GitHub asset digest mismatch');
    const archive = path.join(stage, name); await fs.writeFile(archive, data);
    const bundle = path.join(stage, 'BurnBar.app');
    await fs.mkdir(path.join(bundle, 'Contents/MacOS'), { recursive: true });
    // Extract only these exact regular file payloads; never unpack arbitrary
    // archive paths, links, executables or install hooks from the release.
    for (const relative of ['Contents/MacOS/burnbar', 'Contents/Info.plist']) {
      const result = run('/usr/bin/unzip', ['-p', archive, `BurnBar.app/${relative}`], { encoding: null });
      await fs.writeFile(path.join(bundle, relative), result.stdout, { mode: relative.endsWith('/burnbar') ? 0o755 : 0o644 });
    }
    run('/usr/bin/plutil', ['-lint', path.join(bundle, 'Contents/Info.plist')]);
    // Ad-hoc signature is rebuilt after extracting the allowlisted files.
    run('/usr/bin/codesign', ['--force', '--deep', '--sign', '-', bundle]);
    try { await fs.rename(target, backup); backedUp = true; } catch (e) { if (e.code !== 'ENOENT') throw e; }
    try { await fs.rename(bundle, target); } catch (e) { if (backedUp) { await fs.rename(backup, target); backedUp = false; } throw e; }
    console.log('Installed. Settings and history were not modified.');
    if (!options.noStart) run('/usr/bin/open', [target]);
  } finally { await fs.rm(stage, { recursive: true, force: true }); }
}
module.exports = { install, releaseFor, assetURL };
