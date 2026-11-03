import test from 'node:test';
import assert from 'node:assert/strict';
import { CHUNK_SIZE, isFlowUrl, isFlowProjectUrl, sourceOrigin, validateBundle, decodeChunk, base64, sha256, validateSelection, downloadUrl, downloadName } from '../core.mjs';

const origin = 'http://localhost:5178';
function fixture() {
  return { version: 1, id: 'bundle-1', createdAt: '2026-11-01T00:00:00Z', source: { origin, projectId: 'project-1', unitId: 'unit-1', projectTitle: '裂变之血', unitTitle: '第一章' },
    shots: [{ id: 'shot-1', title: '镜头01', position: 0, revisionId: 'rev-1', revision: { id: 'rev-1', shotId: 'shot-1', version: 1, durationMs: 5000, action: '叙事意图：亲近。\n表演调度：递碗。\n时间节拍：0-2秒。\n情绪：自然\n光线与氛围：晨光\n声音：碗声。' }, prompt: '桑若递碗，陶碗落在石面的轻响。', durationMs: 5000, settings: { aspectRatio: '16:9', resolution: '720p' }, mediaIds: ['media-1'], warnings: ['图片已过期'] }],
    media: [{ id: 'media-1', name: '陶碗.png', mimeType: 'image/png', byteLength: 3, sha256: 'a'.repeat(64), roles: ['prop'], shotIds: ['shot-1'], status: 'stale', assetVersionId: 'asset-v1' }] };
}

test('accept complete saved script and media identities; discard undeclared configuration', () => {
  const input = fixture(); input.apiKey = 'must-not-cross'; input.shots[0].revision.createdBy = 'private-user';
  const bundle = validateBundle(input, origin);
  assert.equal(bundle.apiKey, undefined);
  assert.equal(bundle.shots[0].revision.createdBy, undefined);
  assert.equal(bundle.shots[0].revision.action, input.shots[0].revision.action);
  assert.equal(bundle.media[0].assetVersionId, 'asset-v1');
  assert.equal(bundle.media[0].status, 'stale');
});

test('reject wrong origin, invalid revision, dangling media, duplicate shot and oversized file', () => {
  assert.throws(() => validateBundle(fixture(), 'https://wrong.example'), /来源/);
  for (const mutate of [b => b.shots[0].revisionId = 'new-rev', b => b.shots[0].mediaIds.push('missing'), b => b.shots.push(b.shots[0]), b => b.media[0].byteLength = 129 * 1024 * 1024, b => b.shots[0].prompt = '@[asset:unresolved]']) {
    const input = fixture(); mutate(input); assert.throws(() => validateBundle(input, origin));
  }
});

test('chunk boundaries, payload size and hashes detect corrupted or incomplete media', async () => {
  const bytes = new Uint8Array(CHUNK_SIZE + 3).fill(42);
  const meta = { byteLength: bytes.length };
  assert.deepEqual(decodeChunk(base64(bytes.subarray(0, CHUNK_SIZE)), meta, 0, 2), bytes.subarray(0, CHUNK_SIZE));
  assert.deepEqual(decodeChunk(base64(bytes.subarray(CHUNK_SIZE)), meta, 1, 2), bytes.subarray(CHUNK_SIZE));
  assert.throws(() => decodeChunk('AAAA', meta, 0, 2), /长度/);
  assert.throws(() => decodeChunk('AAAA', meta, 2, 2), /序号/);
  assert.throws(() => decodeChunk('@@@@', { byteLength: 3 }, 0, 1), /格式/);
  assert.equal(await sha256(new TextEncoder().encode('abc')), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad');
});

test('Flow host and result URLs cannot redirect to arbitrary credentials or origins', () => {
  assert(isFlowUrl('https://flow.google.com/u/6/project/123'));
  assert(isFlowUrl('https://labs.google/fx/tools/flow/project/123'));
  assert(!isFlowUrl('https://flow.google.com.evil.example'));
  assert(!isFlowUrl('https://labs.google/fx/tools/other'));
  assert(!isFlowUrl('https://secret@flow.google.com/'));
  assert.equal(sourceOrigin(origin + '/projects'), origin);
  assert.throws(() => sourceOrigin('http://untrusted.example'));
  assert.throws(() => downloadUrl('https://googleapis.com.evil.example/video'));
  assert.throws(() => downloadUrl('file:///private/path'));
  assert.throws(() => downloadUrl('https://secret@storage.googleapis.com/file'));
  assert.equal(downloadUrl('https://storage.googleapis.com/file'), 'https://storage.googleapis.com/file');
});

test('only concrete Flow project routes can be calibrated or started', () => {
  for (const url of ['https://flow.google.com/u/6/project/123', 'https://flow.google.com/project/123?view=videos', 'https://labs.google/fx/tools/flow/project/123', 'https://labs.google/fx/en/tools/flow/project/123']) assert(isFlowProjectUrl(url), url);
  for (const url of ['https://flow.google.com/', 'https://flow.google.com/u/6/?pli=1', 'https://flow.google.com/u/6/project/', 'https://flow.google.com/u/6/?project=123', 'https://labs.google/fx/tools/flow', 'https://flow.google.com.evil.example/project/123', 'http://flow.google.com/project/123']) assert(!isFlowProjectUrl(url), url);
});

test('mode selection never silently drops media or mixes another shot', () => {
  const bundle = validateBundle(fixture(), origin);
  assert.equal(validateSelection(bundle, [{ shotId: 'shot-1', mediaIds: ['media-1'] }], 'frame')[0].status, 'queued');
  assert.throws(() => validateSelection(bundle, [{ shotId: 'shot-1', mediaIds: ['media-1'] }], 'text'));
  assert.throws(() => validateSelection(bundle, [{ shotId: 'shot-1', mediaIds: ['foreign'] }], 'ingredients'));
  assert.throws(() => validateSelection(bundle, [{ shotId: 'shot-1', mediaIds: [] }], 'frame'));
  assert.throws(() => validateSelection(bundle, [{ shotId: 'shot-1', mediaIds: [] }], 'ingredients'));
});

test('download path is confined to named project files', () => {
  const bundle = fixture(); bundle.source.projectTitle = '../恶意/路径';
  bundle.shots[0].title = '../../镜头';
  const name = downloadName(bundle, bundle.shots[0]);
  assert.equal(name.split('/').length, 3);
  assert(!name.split('/').some(part => part === '..'));
  assert(name.endsWith('.mp4'));
});
