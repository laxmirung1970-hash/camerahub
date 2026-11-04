import {test} from 'node:test';
import assert from 'node:assert/strict';
import {scanText, FORBIDDEN_PATHS} from './check-private.mjs';

const names = (hits) => hits.map((h) => h.split(' ')[1].replace(':', ''));

test('flags real LAN addresses but allows documented fixtures', () => {
  assert.deepEqual(names(scanText('a.md', 'camera at 192.168.1.250')), ['lan-ip']);
  assert.deepEqual(scanText('a.md', 'placeholder="192.168.1.50"'), []);
  assert.deepEqual(scanText('a.md', 'doc example 192.0.2.91 and fixture 172.16.0.202'), []);
});

test('flags accounts, tokens and hosts; allows hashes and fixtures', () => {
  assert.deepEqual(names(scanText('a', 'owner@gmail.com')), ['email']);
  assert.deepEqual(names(scanText('a', 'token = abcdefghijklmnopqrstuvwxyz')), ['token-assign']);
  assert.deepEqual(scanText('a', "token: 'fixture-secret-value-long-enough'"), []);
  assert.deepEqual(names(scanText('a', 'x '.repeat(1) + 'f'.repeat(64))), ['hex64']);
  assert.deepEqual(scanText('a', 'sha256 ' + 'f'.repeat(64)), []);
  assert.deepEqual(names(scanText('a', 'xiaomi://u@h?did=123456789')), ['xiaomi-did']);
  assert.deepEqual(names(scanText('a', 'https://nas.tail1234.ts.net')), ['tailnet-host']);
  assert.deepEqual(scanText('a', 'https://camerahub.example.ts.net'), []);
  assert.deepEqual(names(scanText('a', 'C:\\Users\\someone\\x')), ['windows-user-path']);
});

test('masks the match in the report', () => {
  const [hit] = scanText('a.md', '192.168.1.250');
  assert.ok(hit.endsWith('192.…'), hit);
});

test('forbidden paths', () => {
  const bad = ['private/NEXT_SESSION.md', 'data/x.json', 'NEXT_SESSION.md', 'a/b.p12', 'web/.env.local', '.env'];
  for (const p of bad) assert.ok(FORBIDDEN_PATHS.some((re) => re.test(p)), p);
  for (const p of ['web/.env.example', 'docs/CAMERAS.th.md', 'backend/app.go']) assert.ok(!FORBIDDEN_PATHS.some((re) => re.test(p)), p);
});
