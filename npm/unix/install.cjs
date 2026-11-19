'use strict';

// macOS and Linux installer. Dependency-free: downloads a checksum-verified
// release asset from GitHub and installs it for the current user only.

const crypto = require('node:crypto');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

const REPO = 'nooblk-98/burnbar';
const API = `https://api.github.com/repos/${REPO}`;

function target(platform = process.platform, arch = process.arch) {
  if (platform === 'linux') {
    if (arch !== 'x64') throw new Error(`No Linux build for ${arch} yet (x64 only). No files were installed.`);
    return { kind: 'linux', asset: 'burnbar-linux-x64.AppImage' };
  }
  if (platform === 'darwin') {
    if (arch !== 'arm64' && arch !== 'x64') throw new Error(`No macOS build for ${arch}. No files were installed.`);
    return { kind: 'macos', asset: `burnbar-macos-${arch}.tar.gz` };
  }
  throw new Error('BurnBar installer supports Windows, macOS and Linux. No files were installed.');
}

async function getJson(url) {
  const headers = { 'User-Agent': 'BurnBar-Installer', Accept: 'application/vnd.github+json' };
  // The token only raises the API rate limit; it is never sent with downloads.
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(url, { headers, signal: AbortSignal.timeout(30_000) });
  if (!res.ok) throw new Error(`GitHub returned ${res.status} for ${url}`);
  return res.json();
}

async function findRelease(channel, assetName) {
  const hasAsset = (r) => r.assets?.some((a) => a.name === assetName) && r.assets.some((a) => a.name === `${assetName}.sha256`);
  if (channel === 'stable') {
    const release = await getJson(`${API}/releases/latest`);
    if (release.draft || release.prerelease || !release.tag_name) throw new Error('GitHub did not return a published stable release.');
    if (!hasAsset(release)) throw new Error(`Release ${release.tag_name} has no ${assetName} build yet. Try again after the next release, or use --channel development.`);
    return release;
  }
  for (let page = 1; page <= 20; page++) {
    const releases = await getJson(`${API}/releases?per_page=100&page=${page}`);
    const found = releases.find((r) => !r.draft && r.prerelease && /^dev-\d+-\d+$/.test(r.tag_name) && hasAsset(r));
    if (found) return found;
    if (releases.length < 100) break;
  }
  throw new Error(`No published development build with ${assetName} was found.`);
}

async function download(url, destination) {
  const res = await fetch(url, { headers: { 'User-Agent': 'BurnBar-Installer' }, redirect: 'follow', signal: AbortSignal.timeout(300_000) });
  if (!res.ok) throw new Error(`Download failed (${res.status}): ${url}`);
  fs.writeFileSync(destination, Buffer.from(await res.arrayBuffer()));
}

function sha256(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

async function fetchVerified(release, assetName, directory) {
  const asset = release.assets.find((a) => a.name === assetName);
  const sum = release.assets.find((a) => a.name === `${assetName}.sha256`);
  const file = path.join(directory, assetName);
  await download(asset.browser_download_url, file);
  const sumFile = path.join(directory, `${assetName}.sha256`);
  await download(sum.browser_download_url, sumFile);
  const expected = fs.readFileSync(sumFile, 'utf8').trim().split(/\s+/)[0].toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(expected)) throw new Error('The published checksum is malformed.');
  if (sha256(file) !== expected) throw new Error('Checksum mismatch. The download was discarded and nothing was installed.');
  return file;
}

function replaceFile(from, to) {
  fs.mkdirSync(path.dirname(to), { recursive: true });
  const staged = `${to}.new-${process.pid}`;
  fs.copyFileSync(from, staged);
  fs.renameSync(staged, to);
}

function installLinux(file, tag, options, home) {
  const dir = options.installDir || path.join(home, '.local', 'share', 'BurnBar');
  const appImage = path.join(dir, 'BurnBar.AppImage');
  replaceFile(file, appImage);
  fs.chmodSync(appImage, 0o755);
  fs.writeFileSync(path.join(dir, '.version'), `${tag}\n`);
  const icon = path.join(dir, 'burnbar.png');
  fs.copyFileSync(path.resolve(__dirname, 'burnbar.png'), icon);
  const bin = path.join(home, '.local', 'bin', 'burnbar');
  fs.mkdirSync(path.dirname(bin), { recursive: true });
  fs.rmSync(bin, { force: true });
  fs.symlinkSync(appImage, bin);
  const desktop = path.join(home, '.local', 'share', 'applications', 'burnbar.desktop');
  fs.mkdirSync(path.dirname(desktop), { recursive: true });
  fs.writeFileSync(desktop, `[Desktop Entry]\nType=Application\nName=BurnBar\nComment=AI coding plan usage in your tray\nExec=${appImage}\nIcon=${icon}\nCategories=Utility;\nTerminal=false\n`);
  return { launch: () => spawn(appImage, [], { detached: true, stdio: 'ignore' }).unref(), location: appImage, note: fs.existsSync(path.join(home, '.local', 'bin')) ? 'Run `burnbar` or find BurnBar in your app launcher. AppImages need FUSE (libfuse2) on some distros.' : '' };
}

function installMac(file, tag, options, home) {
  const dir = options.installDir || path.join(home, 'Applications');
  const app = path.join(dir, 'BurnBar.app');
  fs.mkdirSync(dir, { recursive: true });
  const stage = fs.mkdtempSync(path.join(dir, '.burnbar-stage-'));
  try {
    const result = spawnSync('tar', ['-xzf', file, '-C', stage], { stdio: 'inherit' });
    if (result.status !== 0) throw new Error('Could not extract the BurnBar app bundle.');
    if (!fs.existsSync(path.join(stage, 'BurnBar.app'))) throw new Error('The archive does not contain BurnBar.app.');
    spawnSync('pkill', ['-f', `${app}/Contents/MacOS/`]);
    fs.rmSync(app, { recursive: true, force: true });
    fs.renameSync(path.join(stage, 'BurnBar.app'), app);
    fs.writeFileSync(path.join(app, 'Contents', '.burnbar-version'), `${tag}\n`);
    spawnSync('xattr', ['-dr', 'com.apple.quarantine', app]);
  } finally {
    fs.rmSync(stage, { recursive: true, force: true });
  }
  return { launch: () => spawnSync('open', [app]), location: app, note: 'BurnBar is unsigned. If macOS blocks it, right-click the app and choose Open.' };
}

function installedVersion(kind, options, home) {
  const file = kind === 'linux'
    ? path.join(options.installDir || path.join(home, '.local', 'share', 'BurnBar'), '.version')
    : path.join(options.installDir || path.join(home, 'Applications'), 'BurnBar.app', 'Contents', '.burnbar-version');
  try { return fs.readFileSync(file, 'utf8').trim(); } catch { return null; }
}

async function run(options, runtime = {}) {
  const { kind, asset } = target(runtime.platform, runtime.arch);
  const home = runtime.home || os.homedir();
  console.log(`Checking the latest BurnBar ${options.channel} release...`);
  const release = await findRelease(options.channel, asset);
  if (options.channel === 'development') console.warn('Installing an unreleased development build. Switch to --channel stable to return to a stable release.');
  if (installedVersion(kind, options, home) === release.tag_name) {
    console.log(`BurnBar ${release.tag_name} is already installed.`);
    return 0;
  }
  if (options.dryRun) {
    console.log(`Would install ${release.tag_name} (${asset}). Dry run: nothing changed.`);
    return 0;
  }
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'burnbar-'));
  try {
    console.log(`Downloading ${asset} from ${release.tag_name}...`);
    const file = await fetchVerified(release, asset, tmp);
    const installed = (kind === 'linux' ? installLinux : installMac)(file, release.tag_name, options, home);
    console.log(`Installed BurnBar ${release.tag_name} to ${installed.location}`);
    if (installed.note) console.log(installed.note);
    if (!options.noStart) installed.launch();
  } finally {
    fs.rmSync(tmp, { recursive: true, force: true });
  }
  return 0;
}

function uninstall(options, runtime = {}) {
  const { kind } = target(runtime.platform, runtime.arch);
  const home = runtime.home || os.homedir();
  const remove = (p, recursive = false) => {
    if (!fs.existsSync(p) && !isLink(p)) return false;
    fs.rmSync(p, { recursive, force: true });
    console.log(`Removed ${p}`);
    return true;
  };
  const isLink = (p) => { try { return fs.lstatSync(p).isSymbolicLink(); } catch { return false; } };
  let removed = false;
  let settings;
  if (kind === 'linux') {
    const dir = options.installDir || path.join(home, '.local', 'share', 'BurnBar');
    spawnSync('pkill', ['-f', path.join(dir, 'BurnBar.AppImage')]);
    // Only the files the installer creates, never the whole directory tree.
    for (const name of ['BurnBar.AppImage', '.version', 'burnbar.png']) removed = remove(path.join(dir, name)) || removed;
    try { fs.rmdirSync(dir); } catch { /* not empty or missing */ }
    const bin = path.join(home, '.local', 'bin', 'burnbar');
    if (isLink(bin) && fs.readlinkSync(bin) === path.join(dir, 'BurnBar.AppImage')) removed = remove(bin) || removed;
    removed = remove(path.join(home, '.local', 'share', 'applications', 'burnbar.desktop')) || removed;
    remove(path.join(home, '.config', 'autostart', 'BurnBar.desktop'));
    settings = path.join(process.env.XDG_CONFIG_HOME || path.join(home, '.config'), 'BurnBar');
  } else {
    const app = path.join(options.installDir || path.join(home, 'Applications'), 'BurnBar.app');
    spawnSync('pkill', ['-f', `${app}/Contents/MacOS/`]);
    removed = remove(app, true);
    remove(path.join(home, 'Library', 'LaunchAgents', 'BurnBar.plist'));
    settings = path.join(home, 'Library', 'Application Support', 'BurnBar');
  }
  if (options.purge) {
    remove(settings, true);
  } else if (fs.existsSync(settings)) {
    console.log(`Settings and history were kept in ${settings}. Use --purge to delete them.`);
  }
  console.log(removed ? 'BurnBar has been uninstalled.' : 'BurnBar was not found; nothing to uninstall.');
  return 0;
}

module.exports = { target, run, uninstall, findRelease };
