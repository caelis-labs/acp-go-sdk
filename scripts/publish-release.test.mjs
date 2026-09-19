import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { chmodSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

function publication(t, scenario) {
  const cwd = mkdtempSync(join(tmpdir(), 'acp-publish-test-'));
  t.after(() => rmSync(cwd, {recursive: true, force: true}));
  mkdirSync(join(cwd, 'bin')); mkdirSync(join(cwd, '.artifacts/publication'), {recursive: true});
  writeFileSync(join(cwd, '.artifacts/publication/plan.json'), JSON.stringify({repository:'owner/sdk', pr:8, version:'1.4.0', commit:'a'.repeat(40), run:123}));
  const stub = `#!/usr/bin/env node
const fs = require('node:fs');
const args = process.argv.slice(2), scenario = process.env.SCENARIO;
const input = fs.readFileSync(0, 'utf8');
fs.appendFileSync(process.env.CALLS, JSON.stringify({args,input})+'\\n');
const result = value => { console.log(JSON.stringify(value)); process.exit(0); };
const missing = message => { console.error(message); process.exit(1); };
if (args[0] === 'api') {
  if (args[1].includes('/git/ref/tags/')) {
    if (scenario === 'new') missing('gh: Not Found (HTTP 404)');
    result({object:{type:'commit',sha:(scenario === 'wrong-tag' ? 'b' : 'a').repeat(40)}});
  }
  if (args[1].endsWith('/git/tags') && args.includes('POST')) result({sha:'c'.repeat(40)});
  if (args[1].endsWith('/git/refs') && args.includes('POST')) result({});
}
if (args[0] === 'release' && args[1] === 'view') {
  if (scenario === 'new') missing('release not found');
  result({isDraft:scenario === 'draft',isPrerelease:false,url:'https://example.invalid/release'});
}
if (args[0] === 'release' && ['create','upload','edit'].includes(args[1])) process.exit(0);
if (['label','pr'].includes(args[0])) process.exit(0);
missing('unexpected gh call: '+args.join(' '));
`;
  writeFileSync(join(cwd,'bin/gh'),stub);chmodSync(join(cwd,'bin/gh'),0o755);
  const callsPath = join(cwd,'calls.jsonl');
  const result = spawnSync(process.execPath,[fileURLToPath(new URL('./publish-release.mjs',import.meta.url)),'publish'],{
    cwd,encoding:'utf8',env:{...process.env,PATH:`${join(cwd,'bin')}:${process.env.PATH}`,GITHUB_REPOSITORY:'owner/sdk',SCENARIO:scenario,CALLS:callsPath},
  });
  return {result,calls:readFileSync(callsPath,'utf8').trim().split('\n').map(JSON.parse)};
}

test('first publication creates an immutable annotated tag and stages assets before publishing', t => {
  const {result,calls}=publication(t,'new');assert.equal(result.status,0,result.stderr);
  const reference=calls.find(call=>call.args[1]?.endsWith('/git/refs'));
  assert.deepEqual(JSON.parse(reference.input),{ref:'refs/tags/v1.4.0',sha:'c'.repeat(40)});
  const create=calls.findIndex(call=>call.args[0]==='release'&&call.args[1]==='create');
  const upload=calls.findIndex(call=>call.args[0]==='release'&&call.args[1]==='upload');
  const publish=calls.findIndex(call=>call.args[0]==='release'&&call.args[1]==='edit');
  assert.ok(calls[create].args.includes('--draft'));
  assert.ok(create<upload&&upload<publish);
  assert.ok(calls[publish].args.includes('--draft=false'));
});
test('a draft is found through gh and completed without another tag or draft', t => {
  const {result,calls}=publication(t,'draft');assert.equal(result.status,0,result.stderr);
  assert.equal(calls.some(call=>call.args.includes('POST')||call.args.includes('create')&&call.args[0]==='release'),false);
  assert.ok(calls.some(call=>call.args[1]==='upload'));
});
test('published release is left intact when retrying bookkeeping', t => {
  const {result,calls}=publication(t,'published');assert.equal(result.status,0,result.stderr);
  assert.equal(calls.some(call=>call.args[0]==='release'&&['create','upload','edit'].includes(call.args[1])),false);
});
test('a conflicting public tag blocks every publication mutation', t => {
  const {result,calls}=publication(t,'wrong-tag');assert.notEqual(result.status,0);
  assert.match(result.stderr,/existing tag points elsewhere/);
  assert.equal(calls.length,1);
});
