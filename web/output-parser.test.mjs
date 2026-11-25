import {test} from 'node:test';
import assert from 'node:assert/strict';
import {parseOutput} from './output-parser.js';
test('mixed blocks retain source and type', () => {
  const result = parseOutput('Hello\n```mermaid\ngraph TD\n```\n```chart\n{}\n```');
  assert.deepEqual(result.blocks.map(b => b.type), ['markdown','mermaid','chart']);
  assert.equal(result.blocks[1].source, 'graph TD');
});
test('incomplete fences remain incomplete', () => {
  assert.equal(parseOutput('```svg\n<svg>').blocks[0].complete, false);
});
test('long fences and ordinary code', () => {
  const block = parseOutput('~~~~js\n~~~\n~~~~').blocks[0];
  assert.equal(block.type, 'code');
  assert.equal(block.source, '~~~');
  assert.equal(block.complete, true);
});
