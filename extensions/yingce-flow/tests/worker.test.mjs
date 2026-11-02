import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import * as core from '../core.mjs';

const workerSource = (await readFile(new URL('../worker.js', import.meta.url), 'utf8')).replace(/^import .*;\n/gm, '');
const origin = 'http://localhost:5178';
const pageUrl = 'https://flow.google.com/u/6/project/one';
const extensionUrl = 'chrome-extension://test/dashboard.html';
function event() { return { listeners: [], addListener(fn) { this.listeners.push(fn); } }; }

function setup({ loseRunAck = false } = {}) {
  const stores = new Map();
  const key = (store, id) => JSON.stringify([store, id]);
  const db = {
    async read(store, id) { return structuredClone(stores.get(key(store, id))); },
    async write(store, id, value) { stores.set(key(store, id), structuredClone(value)); },
    async all(store) { return [...stores].filter(([id]) => JSON.parse(id)[0] === store).map(([, value]) => structuredClone(value)); },
    async transact(changes) { for (const c of changes) if (c.remove) stores.delete(key(c.store, c.key)); else await db.write(c.store, c.key, c.value); },
    async removeTransfer(transfer) { stores.delete(key('transfers', transfer.id)); },
  };
  const storage = { sourceOrigins: [origin], flowAdapterProfile: { prompt: '#prompt', generate: '#generate', results: '#results', upload: '#file' } };
  const commands = [];
  const openedTabs = [];
  const panelBehavior = [];
  const injections = [];
  const downloads = new Map();
  let downloadId = 0;
  const chrome = {
    runtime: { id: 'test', getURL: path => `chrome-extension://test/${path}`, async getContexts() { return []; }, onMessage: event(), onStartup: event(), onInstalled: event() },
    action: { onClicked: event(), async setBadgeText() {} },
    sidePanel: { async setPanelBehavior(options) { panelBehavior.push(options); } },
    storage: { local: { async get() { return structuredClone(storage); }, async set(data) { Object.assign(storage, data); } } },
    tabs: { async query() { return []; }, async get(id) { return { id, url: pageUrl }; }, async create(options) { openedTabs.push(options); }, async update() {}, async sendMessage(tab, command) {
      commands.push(command);
      if (command.type === 'RUN' && loseRunAck) throw new Error(typeof loseRunAck === 'string' ? loseRunAck : 'channel closed');
      return { ok: true, data: command.type === 'INSPECT' ? { configured: true, uploadReady: true, profile: structuredClone(storage.flowAdapterProfile), url: pageUrl } : {} };
    } },
    downloads: { onChanged: event(), async download(options) { const id = ++downloadId; downloads.set(id, { id, ...options, state: 'in_progress' }); return id; }, async search({ id }) { return downloads.has(id) ? [downloads.get(id)] : []; } },
    alarms: { onAlarm: event(), async create() {} },
    permissions: { async contains() { return true; } },
    scripting: { async getRegisteredContentScripts() { return []; }, async registerContentScripts() {}, async executeScript(options) { injections.push(structuredClone(options)); } },
  };
  vm.runInNewContext(workerSource, { ...core, ...db, chrome, crypto, Blob, URL, Uint8Array, TextEncoder, console, setTimeout, structuredClone });
  const ui = { url: extensionUrl, id: 'test' };
  const flow = { url: pageUrl, id: 'test', frameId: 0, tab: { id: 7 } };
  const source = { url: `${origin}/projects/project-1/workflow/example`, frameId: 0, tab: { id: 9 } };
  function message(type, data = {}, sender = ui, target = 'worker') {
    return new Promise(resolve => chrome.runtime.onMessage.listeners[0]({ type, data, target }, sender, resolve));
  }
  async function idle() { await new Promise(resolve => setTimeout(resolve, 15)); }
  return { db, chrome, ui, flow, source, message, commands, downloads, idle, openedTabs, panelBehavior, injections };
}

function fixture() {
  return { version: 1, id: 'bundle-1', createdAt: '2026-11-01T00:00:00Z', source: { origin, projectId: 'project-1', unitId: 'unit-1', projectTitle: '项目', unitTitle: '章节' },
    shots: ['one', 'two'].map((id, position) => ({ id, position, title: id, revisionId: `rev-${id}`, revision: { id: `rev-${id}`, shotId: id, version: 1 }, prompt: '镜头从树下的热粥缓慢推向屋门，温柔的晨光透过树叶照亮石桌。', durationMs: 5000, settings: { aspectRatio: '16:9', resolution: '720' }, mediaIds: [], warnings: [] })), media: [] };
}

async function start(h) {
  await h.db.write('bundles', 'bundle-1', fixture());
  const response = await h.message('START', { bundleId: 'bundle-1', tabId: 7, selections: [{ shotId: 'one', mediaIds: [] }, { shotId: 'two', mediaIds: [] }], mode: 'text', confirmed: true, settingsNote: 'Veo / 8s / 16:9 / 1个输出' });
  assert(response.ok, response.error);
  const run = await h.db.read('runs', response.data.runId);
  return { runId: run.id, shotId: 'one', attemptId: run.items[0].attemptId };
}

test('source bridge verifies exact origin, page project and complete file digest', async () => {
  const h = setup();
  const bad = await h.message('PING', {}, { ...h.source, url: 'https://evil.example/projects/project-1' }, 'source');
  assert(!bad.ok);
  const bundle = fixture();
  const bytes = new Uint8Array([1, 2, 3]);
  bundle.media = [{ id: 'file-1', name: 'image.png', mimeType: 'image/png', byteLength: 3, sha256: await core.sha256(bytes), roles: ['storyboard'], shotIds: ['one'] }];
  bundle.shots[0].mediaIds = ['file-1'];
  const begin = await h.message('BEGIN', { bundle }, h.source, 'source');
  assert(begin.ok, begin.error);
  const data = { transferId: begin.data.transferId };
  assert(!(await h.message('COMMIT', data, h.source, 'source')).ok);
  assert((await h.message('MEDIA_CHUNK', { ...data, mediaId: 'file-1', index: 0, total: 1, data: core.base64(bytes) }, h.source, 'source')).ok);
  assert((await h.message('COMMIT', data, h.source, 'source')).ok);
  assert.deepEqual(new Uint8Array(await (await h.db.read('files', ['bundle-1', 'file-1'])).arrayBuffer()), bytes);
});

test('one-shot durable submission gate rejects duplicates and foreign sender/attempt', async () => {
  const h = setup(); const data = await start(h);
  assert.equal(h.commands.filter(c => c.type === 'RUN').length, 1);
  assert(!(await h.message('BEFORE_SUBMIT', { ...data, attemptId: 'old-attempt' }, h.flow)).ok);
  assert(!(await h.message('BEFORE_SUBMIT', data, { ...h.flow, tab: { id: 88 } })).ok);
  assert((await h.message('BEFORE_SUBMIT', data, h.flow)).ok);
  assert(!(await h.message('BEFORE_SUBMIT', data, h.flow)).ok);
  const run = await h.db.read('runs', data.runId);
  assert.equal(run.items[0].status, 'submitting');
});

test('only completed downloads advance the queue; one result cannot download twice', async () => {
  const h = setup(); const data = await start(h);
  await h.message('BEFORE_SUBMIT', data, h.flow);
  await h.message('PROGRESS', { ...data, phase: 'submitted' }, h.flow);
  const result = await h.message('RESULT', { ...data, url: 'https://storage.googleapis.com/video.mp4' }, h.flow);
  assert(result.ok, result.error);
  assert(!(await h.message('RESULT', { ...data, url: 'https://storage.googleapis.com/video.mp4' }, h.flow)).ok);
  assert.equal(h.commands.filter(c => c.type === 'RUN').length, 1);
  h.downloads.get(1).state = 'complete';
  h.chrome.downloads.onChanged.listeners[0]({ id: 1, state: { current: 'complete' } }); await h.idle();
  assert.equal(h.commands.filter(c => c.type === 'RUN').length, 2);
  assert.equal((await h.db.read('runs', data.runId)).items[0].status, 'completed');
});

test('lost page acknowledgement pauses without resubmitting', async () => {
  const h = setup({ loseRunAck: true }); const data = await start(h);
  const run = await h.db.read('runs', data.runId);
  assert.equal(run.status, 'paused'); assert.equal(run.items[0].status, 'uncertain');
  assert(!(await h.message('RESUME', { runId: run.id })).ok);
  assert.equal(h.commands.filter(c => c.type === 'RUN').length, 1);
});

test('pause after submit requires explicit resolution; old callback cannot affect retry', async () => {
  const h = setup(); const data = await start(h);
  await h.message('BEFORE_SUBMIT', data, h.flow);
  await h.message('PAUSE', { runId: data.runId });
  assert.equal((await h.db.read('runs', data.runId)).items[0].status, 'uncertain');
  assert(!(await h.message('RESUME', { runId: data.runId })).ok);
  assert((await h.message('RESOLVE', { ...data, action: 'retry', confirmed: true })).ok);
  assert((await h.message('RESUME', { runId: data.runId })).ok);
  const run = await h.db.read('runs', data.runId);
  assert.notEqual(run.items[0].attemptId, data.attemptId);
  assert(!(await h.message('ERROR', { ...data, afterSubmit: true, message: 'stale callback' }, h.flow)).ok);
  assert.equal((await h.db.read('runs', data.runId)).items[0].status, 'preparing');
});

test('retry and resume validate and use the latest calibration in the original Flow project', async () => {
  const h = setup(); const data = await start(h);
  await h.message('ERROR', { ...data, afterSubmit: false, message: 'editor scope' }, h.flow);
  const updated = { prompt: '#new-prompt', generate: '#new-send', results: '#new-results', upload: '#new-file' };
  await h.chrome.storage.local.set({ flowAdapterProfile: updated });
  const resolved = await h.message('RESOLVE', { ...data, action: 'retry', confirmed: true });
  assert(resolved.ok, resolved.error);
  assert.deepEqual((await h.db.read('runs', data.runId)).profile, updated);
  const latest = { ...updated, generate: '#corrected-send' };
  await h.chrome.storage.local.set({ flowAdapterProfile: latest });
  const resumed = await h.message('RESUME', { runId: data.runId });
  assert(resumed.ok, resumed.error);
  assert.deepEqual(h.commands.filter(c => c.type === 'RUN').at(-1).data.profile, latest);
});

test('invalid editor calibration leaves failed items and paused queues untouched on retry or resume', async () => {
  for (const action of ['retry', 'resume']) {
    const h = setup(); const data = await start(h);
    if (action === 'retry') await h.message('ERROR', { ...data, afterSubmit: false, message: 'editor scope' }, h.flow);
    else await h.message('PAUSE', { runId: data.runId });
    const before = await h.db.read('runs', data.runId);
    const send = h.chrome.tabs.sendMessage;
    h.chrome.tabs.sendMessage = async (tab, message) => message.type === 'INSPECT'
      ? { ok: true, data: { configured: false, problem: '提示框与发送按钮不在独立输入区' } } : send(tab, message);
    const response = action === 'retry'
      ? await h.message('RESOLVE', { ...data, action: 'retry', confirmed: true })
      : await h.message('RESUME', { runId: data.runId });
    assert.equal(response.reason, 'flow_setup_required');
    assert.deepEqual(await h.db.read('runs', data.runId), before);
    assert.equal(h.commands.filter(c => c.type === 'RUN').length, 1);
  }
});

test('retry cannot bind a paused run to a different Flow project', async () => {
  const h = setup(); const data = await start(h);
  await h.message('ERROR', { ...data, afterSubmit: false, message: 'editor scope' }, h.flow);
  const before = await h.db.read('runs', data.runId);
  h.chrome.tabs.get = async id => ({ id, url: 'https://flow.google.com/u/6/project/other' });
  const response = await h.message('RESOLVE', { ...data, action: 'retry', confirmed: true });
  assert.equal(response.reason, 'flow_setup_required');
  assert.equal(response.field, 'flow-tab');
  assert.deepEqual(await h.db.read('runs', data.runId), before);
  assert.equal(h.commands.filter(c => c.type === 'RUN').length, 1);
});

test('retry checks media requirements for pending shots instead of treating every run as text-only', async () => {
  const h = setup(); const data = await start(h);
  await h.message('ERROR', { ...data, afterSubmit: false, message: 'editor scope' }, h.flow);
  const run = await h.db.read('runs', data.runId);
  run.items[1].mediaIds = ['image-1'];
  await h.db.write('runs', data.runId, run);
  const send = h.chrome.tabs.sendMessage;
  h.chrome.tabs.sendMessage = async (tab, message) => {
    const response = await send(tab, message);
    if (message.type === 'INSPECT') response.data.uploadReady = false;
    return response;
  };
  const response = await h.message('RESOLVE', { ...data, action: 'retry', confirmed: true });
  assert.equal(response.reason, 'flow_setup_required');
  assert.match(response.error, /上传入口/);
  assert.deepEqual(await h.db.read('runs', data.runId), run);
});

test('dashboard-only controls cannot be invoked by project content scripts', async () => {
  const h = setup();
  assert(!(await h.message('CONNECT_SOURCE', { origin }, h.source)).ok);
  assert(!(await h.message('START', { confirmed: true }, h.source)).ok);
});

test('late page errors cannot turn a generated result into a retry or revive a stopped queue', async () => {
  const h = setup(); const data = await start(h);
  await h.message('BEFORE_SUBMIT', data, h.flow);
  await h.message('RESULT', { ...data, url: 'https://storage.googleapis.com/video.mp4' }, h.flow);
  await h.message('ERROR', { ...data, message: 'late timeout', afterSubmit: true }, h.flow);
  let run = await h.db.read('runs', data.runId);
  assert.equal(run.items[0].status, 'downloading'); assert.equal(run.status, 'running');
  const h2 = setup(); const data2 = await start(h2);
  await h2.message('BEFORE_SUBMIT', data2, h2.flow);
  await h2.message('STOP', { runId: data2.runId });
  await h2.message('ERROR', { ...data2, message: 'cancelled', afterSubmit: true }, h2.flow);
  run = await h2.db.read('runs', data2.runId);
  assert.equal(run.status, 'stopped');
});

test('same bundle cannot be imported concurrently or overwrite an already reviewed package', async () => {
  const h = setup(); const bundle = fixture();
  const first = await h.message('BEGIN', { bundle }, h.source, 'source');
  assert(first.ok);
  const second = await h.message('BEGIN', { bundle }, { ...h.source, tab: { id: 10 } }, 'source');
  assert(!second.ok);
  await h.db.write('bundles', bundle.id, { ...bundle, createdAt: 'original' });
  assert(!(await h.message('COMMIT', { transferId: first.data.transferId }, h.source, 'source')).ok);
  assert.equal((await h.db.read('bundles', bundle.id)).createdAt, 'original');
});

test('download retry failure preserves a stopped queue', async () => {
  const h = setup(); const data = await start(h);
  await h.message('BEFORE_SUBMIT', data, h.flow);
  h.chrome.downloads.download = async () => { throw new Error('network unavailable'); };
  await h.message('RESULT', { ...data, url: 'https://storage.googleapis.com/video.mp4' }, h.flow);
  await h.message('STOP', { runId: data.runId });
  await h.message('RETRY_DOWNLOAD', data);
  const run = await h.db.read('runs', data.runId);
  assert.equal(run.status, 'stopped'); assert.equal(run.items[0].status, 'download_failed');
});

test('dynamic source registration covers HTTPS localhost and distinguishes origins before marking connected', async () => {
  const h = setup(); const registered = [];
  h.chrome.scripting.registerContentScripts = async scripts => registered.push(...scripts);
  for (const value of ['https://localhost:5178', 'https://a-b.example', 'https://a.b.example', 'https://a.b.example:8443']) {
    const response = await h.message('CONNECT_SOURCE', { origin: value }); assert(response.ok, response.error);
  }
  assert.equal(registered.length, 4); assert.equal(new Set(registered.map(s => s.id)).size, 4);
  h.chrome.scripting.registerContentScripts = async () => { throw new Error('registration denied'); };
  assert(!(await h.message('CONNECT_SOURCE', { origin: 'https://failed.example' })).ok);
  const storage = await h.chrome.storage.local.get();
  assert(!storage.sourceOrigins.includes('https://failed.example'));
});

test('toolbar uses the native side panel; installation and completed import never open a full tab', async () => {
  const h = setup();
  const manifest = JSON.parse(await readFile(new URL('../manifest.json', import.meta.url), 'utf8'));
  assert(manifest.permissions.includes('sidePanel'));
  assert.equal(manifest.side_panel.default_path, 'dashboard.html');
  assert.equal(h.panelBehavior.length, 1);
  assert.equal(h.panelBehavior[0].openPanelOnActionClick, true);
  assert.equal(h.chrome.action.onClicked.listeners.length, 0);
  h.chrome.runtime.onInstalled.listeners[0]();
  const begin = await h.message('BEGIN', { bundle: fixture() }, h.source, 'source');
  assert(begin.ok, begin.error);
  const result = await h.message('COMMIT', { transferId: begin.data.transferId }, h.source, 'source');
  assert(result.ok, result.error);
  assert.equal((await h.chrome.storage.local.get()).latestBundleId, 'bundle-1');
  assert.deepEqual(h.openedTabs, []);
});

test('inspection attaches a missing adapter to an already open Flow tab once', async () => {
  const h = setup(); let attempts = 0;
  h.chrome.tabs.sendMessage = async (tabId, message, options) => {
    assert.equal(tabId, 7); assert.equal(message.type, 'INSPECT'); assert.equal(options.frameId, 0);
    if (++attempts === 1) throw new Error('Could not establish connection. Receiving end does not exist.');
    return { ok: true, data: { configured: true } };
  };
  const result = await h.message('INSPECT', { tabId: 7 });
  assert(result.ok, result.error); assert.equal(result.data.configured, true);
  assert.equal(attempts, 2);
  assert.deepEqual(h.injections, [{ target: { tabId: 7, frameIds: [0] }, files: ['flow-content.js'] }]);
});

test('inspection never injects into a foreign tab or reinjects an already connected adapter', async () => {
  const h = setup();
  assert((await h.message('INSPECT', { tabId: 7 })).ok);
  h.chrome.tabs.get = async id => ({ id, url: 'https://example.com/' });
  assert(!(await h.message('INSPECT', { tabId: 7 })).ok);
  assert.deepEqual(h.injections, []);
});

test('blocked adapter injection returns recovery instructions without attempting generation', async () => {
  const h = setup();
  h.chrome.tabs.sendMessage = async () => { throw new Error('Receiving end does not exist.'); };
  h.chrome.scripting.executeScript = async () => { throw new Error('Cannot access contents of the page'); };
  const response = await h.message('INSPECT', { tabId: 7 });
  assert(!response.ok); assert.match(response.error, /允许插件访问.*刷新 Flow/);
  assert.equal((await h.db.all('runs')).length, 0);
});

test('a missing RUN receiver never triggers reinjection or automatic resubmission', async () => {
  const h = setup({ loseRunAck: 'Could not establish connection. Receiving end does not exist.' });
  const data = await start(h);
  const run = await h.db.read('runs', data.runId);
  assert.equal(run.status, 'paused'); assert.equal(run.items[0].status, 'uncertain');
  assert.match(run.items[0].error, /检查页面/);
  assert.equal(h.commands.filter(c => c.type === 'RUN').length, 1);
  assert.deepEqual(h.injections, []);
});

const startRequest = { bundleId: 'bundle-1', tabId: 7, selections: [{ shotId: 'one', mediaIds: [] }], mode: 'text', confirmed: true, settingsNote: 'Veo / 8s / 16:9 / 1个输出' };

test('Flow home cannot be calibrated or used to start a queue', async () => {
  const h = setup();
  await h.db.write('bundles', 'bundle-1', fixture());
  h.chrome.tabs.get = async id => ({ id, url: 'https://flow.google.com/u/6/?pli=1' });
  for (const [type, data] of [['START', startRequest], ['CALIBRATE', { tabId: 7, field: 'generate' }], ['INSPECT', { tabId: 7 }]]) {
    const response = await h.message(type, data);
    assert.equal(response.ok, false);
    assert.equal(response.reason, 'flow_setup_required');
    assert.equal(response.field, 'flow-tab');
    assert.match(response.error, /具体项目/);
  }
  assert.equal((await h.db.all('runs')).length, 0);
  assert.equal(h.commands.length, 0);
});

test('missing calibration identifies the exact controls without creating a failed queue', async () => {
  const h = setup();
  await h.db.write('bundles', 'bundle-1', fixture());
  await h.chrome.storage.local.set({ flowAdapterProfile: { prompt: '#prompt' } });
  const response = await h.message('START', startRequest);
  assert.equal(response.reason, 'flow_setup_required');
  assert.equal(response.field, 'calibration');
  assert.match(response.error, /生成按钮、结果区域/);
  assert.doesNotMatch(response.error, /提示框/);
  assert.equal((await h.db.all('runs')).length, 0);
});

test('saved selectors must pass live inspection before a queue is persisted or RUN is sent', async () => {
  const h = setup();
  await h.db.write('bundles', 'bundle-1', fixture());
  h.chrome.tabs.sendMessage = async (_tab, message) => {
    h.commands.push(message);
    return { ok: true, data: { configured: false, problem: '校准元素当前不可见。' } };
  };
  const response = await h.message('START', startRequest);
  assert.equal(response.ok, false);
  assert.equal(response.reason, 'flow_setup_required');
  assert.match(response.error, /当前不可见/);
  assert.equal((await h.db.all('runs')).length, 0);
  assert.equal(h.commands.some(command => command.type === 'RUN'), false);
});

test('media queues require a live upload control but text-only queues do not', async () => {
  const h = setup();
  const bundle = fixture();
  bundle.media = [{ id: 'image-1', name: '角色', mimeType: 'image/png', byteLength: 3, roles: ['reference'], shotIds: ['one'] }];
  bundle.shots[0].mediaIds = ['image-1'];
  await h.db.write('bundles', bundle.id, bundle);
  const send = h.chrome.tabs.sendMessage;
  h.chrome.tabs.sendMessage = async (tab, message) => message.type === 'INSPECT'
    ? { ok: true, data: { configured: true, uploadReady: false, uploadProblem: '请展开添加素材菜单。', profile: (await h.chrome.storage.local.get()).flowAdapterProfile } }
    : send(tab, message);
  const response = await h.message('START', { ...startRequest, mode: 'ingredients', selections: [{ shotId: 'one', mediaIds: ['image-1'] }] });
  assert.equal(response.ok, false); assert.equal(response.reason, 'flow_setup_required');
  assert.match(response.error, /上传入口未就绪/);
  assert.equal((await h.db.all('runs')).length, 0);
  assert.equal(h.commands.some(command => command.type === 'RUN'), false);
  const text = await h.message('START', startRequest);
  assert(text.ok, text.error);
  assert.equal(h.commands.filter(command => command.type === 'RUN').length, 1);
});
