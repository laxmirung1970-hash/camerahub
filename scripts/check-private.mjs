#!/usr/bin/env node
// Scans files that would be committed for private data (LAN addresses, accounts,
// tokens, owner-only paths). Exit 1 on any hit. Usage:
//   node scripts/check-private.mjs            # git ls-files (tracked + untracked, not ignored)
//   node scripts/check-private.mjs --staged   # only the index
//   node scripts/check-private.mjs --files a b
// Owner-specific patterns live in private/check-private.local.json (gitignored):
//   {"rules":[{"name":"owner-strings","pattern":"Backyard_|MyCamName"}]}
import {execFileSync} from 'node:child_process';
import {existsSync, readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';
import path from 'node:path';

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const BINARY = /\.(png|jpe?g|gif|webp|ico|mp4|webm|ttf|otf|woff2?|tar|zip|apk|p12|jks|exe|dll|so|dex|sqlite)$/i;
const SKIP = /(^|\/)(package-lock\.json|go\.sum|check-private\.test\.mjs)$/;

export const FORBIDDEN_PATHS = [
  /^(private|data|\.cache|runtime|releases|test-results|playwright-report)\//,
  /(^|\/)NEXT_SESSION\.md$/,
  /\.(p12|pfx|jks|keystore|cambackup|tar|pem|key)$/i,
  /(^|\/)\.env(\.(?!example$)[^/]*)?$/,
];

export const RULES = [
  {name: 'lan-ip', pattern: /\b192\.168\.\d{1,3}\.\d{1,3}\b/g, allow: /\b192\.168\.1\.(5|50|60|77)\b/},
  {name: 'email', pattern: /[\w.+-]+@(gmail|hotmail|outlook|yahoo|icloud)\.com\b/gi},
  {name: 'token-assign', pattern: /\b(token|secret|password|passwd|api[_-]?key)\s*[=:]\s*["']?[A-Za-z0-9_\-]{16,}/gi, allow: /fixture|private|do-not-expose|secret-camera-password|example/i},
  {name: 'hex64', pattern: /\b[a-f0-9]{64}\b/g, allow: /sha-?256|sha256sum|digest|image id|manifest|certificate|apk|'a'\.repeat|'b'\.repeat/i},
  {name: 'xiaomi-did', pattern: /\bdid=\d{6,}/g},
  {name: 'tailnet-host', pattern: /\b[a-z0-9-]+\.[a-z0-9-]+\.ts\.net\b/gi, allow: /example|your-tailnet|โดเมน/i},
  {name: 'cf-tunnel', pattern: /trycloudflare\.com|cfargotunnel\.com/g},
  {name: 'private-key', pattern: /-----BEGIN [A-Z ]*PRIVATE KEY-----/g},
  {name: 'windows-user-path', pattern: /[A-Z]:\\Users\\[^\\\s"']+/g},
];

function localRules() {
  const file = path.join(ROOT, 'private', 'check-private.local.json');
  if (!existsSync(file)) return [];
  try {
    const parsed = JSON.parse(readFileSync(file, 'utf8'));
    return (parsed.rules || []).map((r) => ({name: r.name, pattern: new RegExp(r.pattern, 'g'), allow: r.allow ? new RegExp(r.allow) : undefined}));
  } catch {
    return [];
  }
}

function mask(s) {
  return s.length <= 4 ? s : s.slice(0, 4) + '…';
}

export function scanText(rel, text, rules = RULES) {
  const hits = [];
  const lines = text.split(/\r?\n/);
  for (const rule of rules) {
    lines.forEach((line, i) => {
      rule.pattern.lastIndex = 0;
      let m;
      while ((m = rule.pattern.exec(line))) {
        if (rule.allow && rule.allow.test(line)) continue;
        hits.push(`${rel}:${i + 1} ${rule.name}: ${mask(m[0])}`);
        if (!rule.pattern.global) break;
      }
    });
  }
  return hits;
}

export function scanFiles(files, rules = RULES) {
  const hits = [];
  for (const file of files) {
    const rel = path.relative(ROOT, path.resolve(ROOT, file)).split(path.sep).join('/');
    if (FORBIDDEN_PATHS.some((re) => re.test(rel))) {
      hits.push(`${rel} forbidden-path`);
      continue;
    }
    if (BINARY.test(rel) || SKIP.test(rel)) continue;
    let buf;
    try {
      buf = readFileSync(path.resolve(ROOT, file));
    } catch {
      continue;
    }
    if (buf.subarray(0, 4096).includes(0)) continue;
    hits.push(...scanText(rel, buf.toString('utf8'), rules));
  }
  return hits;
}

function listFromGit(staged) {
  const args = staged ? ['diff', '--cached', '--name-only', '-z', '--diff-filter=ACMR'] : ['ls-files', '-z', '--cached', '--others', '--exclude-standard'];
  const out = execFileSync('git', args, {cwd: ROOT, encoding: 'utf8'});
  return out.split('\0').filter(Boolean);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const argv = process.argv.slice(2);
  let files;
  if (argv[0] === '--files') files = argv.slice(1);
  else files = listFromGit(argv.includes('--staged'));
  const hits = scanFiles(files, [...RULES, ...localRules()]);
  if (hits.length) {
    console.error('check-private: private data found\n' + hits.join('\n'));
    process.exit(1);
  }
  console.log(`check-private: ${files.length} files clean`);
}
