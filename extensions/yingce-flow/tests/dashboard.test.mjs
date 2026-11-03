import assert from 'node:assert/strict';
import test from 'node:test';
import { readFile } from 'node:fs/promises';
import { createRequire } from 'node:module';
import { pathToFileURL } from 'node:url';
import { runInContext } from 'node:vm';
import { sourceOrigin, isFlowProjectUrl } from '../core.mjs';

const require = createRequire(new URL('../../../web/package.json', import.meta.url));
const { Window } = await import(pathToFileURL(require.resolve('happy-dom')).href);
const html = await readFile(new URL('../dashboard.html', import.meta.url), 'utf8');
const source = (await readFile(new URL('../dashboard.js', import.meta.url), 'utf8')).replace(/^import .*;\r?\n/gm, '');
const configured = { sourceOrigins: ['http://localhost:5178'], flowAdapterProfile: { prompt: '#prompt', generate: '#generate', results: '#results', upload: '#upload' } };

function setup(t, { storage = {}, bundles = [], failWorker = false } = {}) {
  const window = new Window({ url: 'chrome-extension://test/dashboard.html', settings: { disableJavaScriptFileLoading: true, disableCSSFileLoading: true } });
  window.document.write(html);
  // happy-dom omits the browser's Option convenience constructor.
  window.Option = function (text, value) { const option = window.document.createElement('option'); option.text = text; option.value = String(value); return option; };
  const messages = [], listeners = [];
  let calibrationNumber = 0;
  window.all = async store => structuredClone(store === 'bundles' ? bundles : []);
  window.read = async () => undefined;
  window.sourceOrigin = sourceOrigin;
  window.isFlowProjectUrl = isFlowProjectUrl;
  window.chrome = {
    runtime: { async sendMessage(message) {
      messages.push(structuredClone(message));
      if (failWorker) throw new Error('Could not establish connection. Receiving end does not exist.');
      if (message.type === 'CALIBRATE') return { ok: true, data: { calibrationId: `calibration-${++calibrationNumber}`, url: 'https://flow.google.com/project/current?session=changed', calibrating: true } };
      return { ok: true, data: message.type === 'FLOW_TABS' ? [{ id: 6, title: '旧项目', url: 'https://flow.google.com/project/old', active: false }, { id: 7, title: '当前项目', url: 'https://flow.google.com/project/current', active: true }] : { configured: true } };
    } },
    storage: { local: { async get() { return structuredClone(storage); } }, onChanged: { addListener(fn) { listeners.push(fn); } } },
  };
  runInContext(source, window);
  t.after(() => window.happyDOM.close());
  const $ = id => window.document.getElementById(id);
  const tab = name => window.document.querySelector(`[data-view="${name}"]`);
  return { window, $, tab, messages, bundles, storage, notify: (changes, area = 'local') => listeners.forEach(fn => fn(changes, area)) };
}

async function until(predicate) {
  const deadline = Date.now() + 1500;
  while (!predicate()) {
    assert(Date.now() < deadline, 'Side panel update timed out');
    await new Promise(resolve => setTimeout(resolve, 5));
  }
}

function bundle(id) {
  return { id, createdAt: '2026-11-01T00:00:00Z', source: { projectTitle: '项目', unitTitle: '第一章' },
    shots: [{ id: 'shot-1', title: '镜头01', durationMs: 5000, settings: { aspectRatio: '16:9' }, revision: { version: 1 }, prompt: '镜头推向门口。', mediaIds: [], warnings: [] }], media: [] };
}

test('first use shows settings beside the active Flow tab; navigation exposes one section at a time', async t => {
  const h = setup(t);
  await until(() => h.$('profile').textContent.includes('/4'));
  assert.equal(h.$('flow-tab').value, '7');
  assert.equal(h.$('view-settings').hidden, false);
  assert.equal(h.tab('settings').getAttribute('aria-pressed'), 'true');
  for (const name of ['runs', 'shots', 'settings']) {
    h.tab(name).click();
    for (const other of ['runs', 'shots', 'settings']) assert.equal(h.$(`view-${other}`).hidden, name !== other);
    assert.equal(h.tab(name).getAttribute('aria-pressed'), 'true');
  }
  h.$('inspect').click();
  await until(() => h.$('message').textContent.includes('页面已连接'));
  assert.equal(h.messages.find(m => m.type === 'INSPECT').data.tabId, 7);
});

test('a received bundle updates the open side panel without starting generation', async t => {
  const h = setup(t, { storage: configured, bundles: [bundle('old')] });
  await until(() => h.$('profile').textContent.includes('/4'));
  h.$('shots').querySelector('.choose-shot').checked = false;
  h.notify({ flowAdapterProfile: { newValue: configured.flowAdapterProfile } });
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(h.$('shots').querySelector('.choose-shot').checked, false, 'calibration updates preserve reviewed selections');
  h.tab('settings').click();
  h.bundles.push(bundle('new'));
  h.notify({ latestBundleId: { newValue: 'new' } });
  await until(() => h.$('message').textContent.includes('已收到新的镜头包'));
  assert.equal(h.$('bundle').value, 'new');
  assert.equal(h.$('view-shots').hidden, false);
  assert.equal(h.messages.some(m => m.type === 'START'), false);
});

test('calibration progress follows the attempt despite a stale cached URL and ignores old updates', async t => {
  const h = setup(t, { storage: configured, bundles: [bundle('one')] });
  await until(() => h.$('profile').textContent.includes('/4'));
  h.$('shots').querySelector('.choose-shot').checked = false;
  h.$('confirm').checked = true;
  const mark = h.window.document.querySelector('[data-field="upload"]');
  mark.click();
  await until(() => !mark.disabled);
  const update = (calibrationId, sequence, message, error = false) => h.notify({ flowCalibrationStatus: { newValue: { calibrationId, sequence, url: 'https://flow.google.com/project/current?session=changed', field: 'upload', message, error } } });
  update('calibration-1', 1, '正在等待上传控件');
  assert.equal(h.$('message').textContent, '正在等待上传控件');
  update('unrelated-attempt', 2, '其他项目的状态', true);
  assert.equal(h.$('message').textContent, '正在等待上传控件');
  update('calibration-1', 2, '出现多个文件上传控件', true);
  assert.equal(h.$('message').classList.contains('error'), true);
  update('calibration-1', 3, '已标记上传入口');
  update('calibration-1', 1, '迟到的等待提示');
  assert.equal(h.$('message').classList.contains('error'), false);
  assert.equal(h.$('message').textContent, '已标记上传入口');
  assert.equal(h.$('shots').querySelector('.choose-shot').checked, false);
  assert.equal(h.$('confirm').checked, true);
  assert.equal(h.messages.some(m => m.type === 'START'), false);
});

test('an error published before the CALIBRATE reply is recovered and shown in the side panel', async t => {
  const h = setup(t, { storage: structuredClone(configured) });
  await until(() => h.$('profile').textContent.includes('/4'));
  const send = h.window.chrome.runtime.sendMessage;
  h.window.chrome.runtime.sendMessage = async message => {
    const response = await send(message);
    if (message.type === 'CALIBRATE') {
      h.storage.flowCalibrationStatus = { calibrationId: response.data.calibrationId, sequence: 2, field: 'upload', url: response.data.url, message: '请点击左下角的 Upload media', error: true };
      h.notify({ flowCalibrationStatus: { newValue: h.storage.flowCalibrationStatus } });
    }
    return response;
  };
  const mark = h.window.document.querySelector('[data-field="upload"]');
  mark.click();
  await until(() => !mark.disabled);
  assert.equal(h.$('message').textContent, '请点击左下角的 Upload media');
  assert.equal(h.$('message').classList.contains('error'), true);
});

test('queue start still requires review and switches to queue status after submission', async t => {
  const h = setup(t, { storage: configured, bundles: [bundle('one')] });
  await until(() => h.$('profile').textContent.includes('/4'));
  h.$('start').click();
  await until(() => h.$('message').classList.contains('error'));
  assert.equal(h.messages.some(m => m.type === 'START'), false);
  h.$('confirm').checked = true;
  h.$('mode').value = 'text';
  h.$('settings').value = 'Veo / 8s / 16:9 / 1个输出';
  h.$('start').click();
  await until(() => h.$('message').textContent.includes('队列已启动'));
  const request = h.messages.find(m => m.type === 'START');
  assert.equal(request.data.tabId, 7);
  assert.deepEqual(request.data.selections, [{ shotId: 'shot-1', mediaIds: [] }]);
  assert.equal(h.$('view-runs').hidden, false);
  assert.equal(h.$('confirm').checked, false);
});

test('an invalidated extension page gives reload instructions instead of a raw connection exception', async t => {
  const h = setup(t, { failWorker: true });
  await until(() => h.$('message').classList.contains('error'));
  assert.match(h.$('message').textContent, /重新加载插件/);
  assert.doesNotMatch(h.$('message').textContent, /Receiving end/);
});

test('a setup rejection opens settings from a scrolled shot list and preserves selected media', async t => {
  const h = setup(t, { storage: configured, bundles: [bundle('one')] });
  await until(() => h.$('profile').textContent.includes('/4'));
  const send = h.window.chrome.runtime.sendMessage;
  h.window.chrome.runtime.sendMessage = async message => message.type === 'START'
    ? { ok: false, error: '请先标记：生成按钮、结果区域。', reason: 'flow_setup_required', field: 'calibration' }
    : send(message);
  h.$('confirm').checked = true;
  h.window.document.querySelector('main').scrollTop = 900;
  h.$('start').click();
  await until(() => h.$('message').classList.contains('error'));
  assert.equal(h.$('view-settings').hidden, false);
  assert.equal(h.window.document.querySelector('main').scrollTop, 0);
  assert.equal(h.window.document.activeElement.id, 'calibration');
  assert.equal(h.$('confirm').checked, true);
  assert.equal(h.$('shots').querySelector('.choose-shot').checked, true);
  assert.equal(h.$('start').disabled, false);
});
