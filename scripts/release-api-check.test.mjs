import assert from 'node:assert/strict';
import test from 'node:test';
import { unexpectedBreakingChanges } from './release-api-check.mjs';

test('additions, draft API and exact schema provenance value updates are allowed', () => {
  assert.deepEqual(unexpectedBreakingChanges(''), []);
  assert.deepEqual(unexpectedBreakingChanges('Incompatible changes:\n- SchemaTag: value changed from "old" to "new"\n- ./experimental/v2.PromptResponse.MessageId: changed\nCompatible changes:\n- ToolCall.Name: added\n'), []);
});
test('stable API removal, type changes and wire version changes block release', () => {
  for (const line of ['- Connection.Close: removed', '- SchemaTag: changed from string to int',
    '- WireProtocolVersion: value changed from 1 to 2', '- ./transport/stdio.Process: removed']) {
    assert.deepEqual(unexpectedBreakingChanges(`Incompatible changes:\n${line}\n`), [line]);
  }
  assert.throws(() => unexpectedBreakingChanges('unexpected tool failure'), /unrecognized/);
});
