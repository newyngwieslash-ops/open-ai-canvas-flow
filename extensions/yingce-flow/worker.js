import { CHANNEL, CHUNK_SIZE, ACTIVE, assert, isFlowUrl, isFlowProjectUrl, sourceOrigin, validateBundle, decodeChunk, base64, sha256, validateSelection, downloadUrl, downloadName, uploadName } from './core.mjs';
import { read, all, write, transact, removeTransfer } from './db.mjs';

let pending = Promise.resolve();
const serial = action => { const result = pending.then(action); pending = result.catch(() => {}); return result; };

async function saveRun(run) {
  run.updatedAt = new Date().toISOString();
  await write('runs', run.id, run);
}

async function sourceMessage(message, sender) {
  assert(sender.tab && sender.frameId === 0 && sender.url, '只接收项目主页面');
  const page = new URL(sender.url);
  const { sourceOrigins = [] } = await chrome.storage.local.get('sourceOrigins');
  assert(sourceOrigins.includes(page.origin), '请先在插件中连接此项目站点，再刷新项目页面');
  assert(/^\/projects\/[^/]+/.test(page.pathname), '请从短剧项目页面提交镜头');
  if (message.type === 'PING') return { version: 1, channel: CHANNEL };
  const data = message.data || {};
  if (message.type === 'BEGIN') {
    const bundle = validateBundle(data.bundle, page.origin);
    assert(page.pathname.split('/')[2] === bundle.source.projectId, '页面项目与镜头包不一致');
    assert(!(await read('bundles', bundle.id)), '此镜头包已接收，请在插件中查看');
    const transfers = await all('transfers');
    for (const transfer of transfers) if (Date.now() - transfer.createdAt > 3600000) await removeTransfer(transfer);
    assert(!transfers.some(t => t.bundle.id === bundle.id && Date.now() - t.createdAt <= 3600000), '同一镜头包已在传输，禁止覆盖');
    assert(!transfers.some(t => t.tabId === sender.tab.id && Date.now() - t.createdAt < 3600000), '已有素材正在传输，请稍后再试');
    const transfer = { id: crypto.randomUUID(), tabId: sender.tab.id, origin: page.origin, createdAt: Date.now(), bundle };
    await write('transfers', transfer.id, transfer);
    return { transferId: transfer.id };
  }
  const transfer = await read('transfers', data.transferId);
  assert(transfer && transfer.tabId === sender.tab.id && transfer.origin === page.origin, '传输不存在或不属于此页面');
  if (message.type === 'ABORT') { await removeTransfer(transfer); return {}; }
  if (message.type === 'MEDIA_CHUNK') {
    const media = transfer.bundle.media.find(m => m.id === data.mediaId);
    assert(media, '传输了未声明的素材');
    const bytes = decodeChunk(data.data, media, data.index, data.total);
    await write('chunks', [transfer.id, media.id, data.index], bytes);
    return {};
  }
  assert(message.type === 'COMMIT', '未知传输命令');
  assert(!(await read('bundles', transfer.bundle.id)), '此镜头包已发布，禁止覆盖');
  // Each file is verified before publishing the bundle; interrupted commits stay invisible.
  for (const media of transfer.bundle.media) {
    const parts = [];
    for (let i = 0; i < Math.ceil(media.byteLength / CHUNK_SIZE); i++) {
      const part = await read('chunks', [transfer.id, media.id, i]);
      assert(part, `素材“${media.name}”尚未完整传输`);
      parts.push(part);
    }
    const blob = new Blob(parts, { type: media.mimeType });
    assert(blob.size === media.byteLength && await sha256(await blob.arrayBuffer()) === media.sha256, `素材“${media.name}”校验失败`);
    await write('files', [transfer.bundle.id, media.id], blob);
  }
  await write('bundles', transfer.bundle.id, transfer.bundle);
  await removeTransfer(transfer);
  await chrome.action.setBadgeText({ text: String(transfer.bundle.shots.length) });
  // A completed transfer no longer has a user gesture to open a native side panel.
  await chrome.storage.local.set({ latestBundleId: transfer.bundle.id });
  return { bundleId: transfer.bundle.id, shotCount: transfer.bundle.shots.length };
}

function fromDashboard(sender) {
  assert(sender.url === chrome.runtime.getURL('dashboard.html') && sender.id === chrome.runtime.id, '此操作只能从插件任务页发起');
}

async function flowCommand(tabId, type, data) {
  let response;
  try { response = await chrome.tabs.sendMessage(tabId, { target: 'flow', type, data }, { frameId: 0 }); }
  catch (error) {
    if (missingReceiver(error)) throw new Error('Flow 页面尚未连接。请在设置中点击“检查页面”；仍失败时刷新 Flow 页面后重试。');
    throw error;
  }
  assert(response?.ok, response?.error || 'Flow 页面未响应，请刷新后重新校准');
  return response.data;
}

function missingReceiver(error) {
  return /receiving end does not exist|could not establish connection/i.test(error?.message || '');
}

function setupError(message, field = 'calibration') {
  return Object.assign(new Error(message), { reason: 'flow_setup_required', field });
}

function requireFlowProject(url) {
  if (!isFlowProjectUrl(url)) throw setupError('请先在 Flow 打开具体项目：点击项目卡片或 New project，进入视频生成页面后再检查和标记控件。首页不能开始队列。', 'flow-tab');
}

async function inspectFlow(tabId) {
  const tab = await chrome.tabs.get(tabId);
  assert(isFlowUrl(tab.url), '请选择 Google Flow 标签页');
  requireFlowProject(tab.url);
  try {
    const response = await chrome.tabs.sendMessage(tabId, { target: 'flow', type: 'INSPECT' }, { frameId: 0 });
    assert(response?.ok, response?.error || 'Flow 页面未响应');
    return response.data;
  } catch (error) {
    if (!missingReceiver(error)) throw error;
    // Attach only for a read-only handshake. Never retry RUN after a missing acknowledgement.
    try { await chrome.scripting.executeScript({ target: { tabId, frameIds: [0] }, files: ['flow-content.js'] }); }
    catch { throw new Error('无法接入 Flow 页面，请允许插件访问该站点并刷新 Flow 页面后重试。'); }
    return flowCommand(tabId, 'INSPECT');
  }
}

async function checkedProfile(tabId, items, expectedUrl) {
  const tab = await chrome.tabs.get(tabId);
  requireFlowProject(tab.url);
  if (expectedUrl && tab.url !== expectedUrl) throw setupError('Flow 页面已切换，请回到队列原来的项目标签页后检查和继续。', 'flow-tab');
  const usesMedia = items.some(i => i.mediaIds.length);
  const { flowAdapterProfile: saved = {} } = await chrome.storage.local.get('flowAdapterProfile');
  const fields = { prompt: '提示框', generate: '生成按钮', results: '结果区域', ...(usesMedia ? { upload: '上传入口' } : {}) };
  const missing = Object.entries(fields).filter(([field]) => !saved[field]).map(([, label]) => label);
  if (missing.length) throw setupError(`请先标记：${missing.join('、')}。点击下方对应“标记”按钮，再在旁边的 Flow 项目中选择控件；完成后检查页面。`);
  const state = await inspectFlow(tab.id);
  if (state?.configured !== true) throw setupError(`当前 Flow 页面校准未通过：${state?.problem || '请重新标记提示框、生成按钮和结果区域。'}`);
  if (usesMedia && state.uploadReady !== true) throw setupError(`当前 Flow 页面上传入口未就绪：${state.uploadProblem || '请展开上传菜单并重新标记上传入口。'}`);
  return { tab, profile: state.profile };
}

async function advance(run) {
  try { await advanceReady(run); }
  catch (error) {
    const item = run.items.find(i => ACTIVE.includes(i.status) || i.status === 'queued');
    if (item) { item.error = error.message; item.status = item.status === 'queued' ? 'failed' : 'uncertain'; }
    run.status = 'paused'; await saveRun(run);
  }
}

async function advanceReady(run) {
  if (run.status !== 'running' || run.items.some(i => ACTIVE.includes(i.status) || i.status === 'uncertain' || i.status === 'failed')) return;
  const item = run.items.find(i => i.status === 'queued');
  if (!item) { run.status = 'completed'; await saveRun(run); return; }
  const tab = await chrome.tabs.get(run.tabId);
  assert(tab.url === run.pageUrl && isFlowUrl(tab.url), 'Flow 页面已切换，请回到原项目后继续');
  const pageStatus = await flowCommand(run.tabId, 'STATUS');
  if (pageStatus?.active) return;
  const bundle = await read('bundles', run.bundleId);
  const shot = bundle.shots.find(s => s.id === item.shotId);
  item.status = 'preparing';
  item.attemptId = crypto.randomUUID();
  item.error = '';
  await saveRun(run);
  try {
    const media = item.mediaIds.map(id => bundle.media.find(m => m.id === id));
    const mapping = media.length ? `\n\n【本次实际上传的参考文件】\n${media.map(m => `文件“${uploadName(m)}”：${m.name}（${m.roles.join('、')}）`).join('\n')}` : '';
    await flowCommand(run.tabId, 'RUN', {
      runId: run.id, shotId: item.shotId, attemptId: item.attemptId, prompt: shot.prompt + mapping,
      files: media.map(m => ({ id: m.id, name: uploadName(m), mimeType: m.mimeType, byteLength: m.byteLength })),
      profile: run.profile, timeoutMs: 15 * 60 * 1000,
    });
  } catch (error) {
    // A lost RUN acknowledgement does not prove the content script did not start.
    item.status = 'uncertain'; item.error = `页面接收状态不明：${error.message}`; run.status = 'paused'; await saveRun(run);
  }
}

async function downloadResult(run, item) {
  const bundle = await read('bundles', run.bundleId);
  const shot = bundle.shots.find(s => s.id === item.shotId);
  item.status = 'downloading';
  delete item.downloadId;
  await saveRun(run);
  try {
    item.downloadId = await chrome.downloads.download({ url: downloadUrl(item.resultUrl), filename: downloadName(bundle, shot), conflictAction: 'uniquify', saveAs: false });
    await saveRun(run);
  } catch (error) {
    item.status = 'download_failed'; item.error = `视频已生成，下载失败：${error.message}`;
    if (run.status !== 'stopped') run.status = 'paused';
    await saveRun(run);
  }
}

async function workerMessage(message, sender) {
  const data = message.data || {};
  if (['CALIBRATED', 'MEDIA_READ', 'BEFORE_SUBMIT', 'PROGRESS', 'RESULT', 'ERROR'].includes(message.type)) {
    assert(sender.tab && sender.frameId === 0 && isFlowUrl(sender.url), '无效的 Flow 页面消息');
    if (message.type === 'CALIBRATED') {
      assert(['prompt', 'generate', 'results', 'upload'].includes(data.field) && typeof data.selector === 'string' && data.selector.length <= 2000, '校准信息无效');
      const { flowAdapterProfile = {} } = await chrome.storage.local.get('flowAdapterProfile');
      flowAdapterProfile[data.field] = data.selector;
      await chrome.storage.local.set({ flowAdapterProfile });
      return {};
    }
    const run = await read('runs', data.runId);
    assert(run && run.tabId === sender.tab.id && run.pageUrl === sender.url, '消息不是来自当前任务的 Flow 项目');
    const item = run.items.find(i => i.shotId === data.shotId);
    assert(item && (ACTIVE.includes(item.status) || item.status === 'uncertain'), '镜头已结束或未开始');
    assert(item.attemptId === data.attemptId, '此回执属于已结束的旧提交');
    if (message.type === 'MEDIA_READ') {
      assert(run.status === 'running' && item.status === 'preparing' && item.mediaIds.includes(data.mediaId), '素材不在当前提交范围');
      const blob = await read('files', [run.bundleId, data.mediaId]);
      assert(blob && Number.isInteger(data.index) && data.index >= 0 && data.index < Math.ceil(blob.size / CHUNK_SIZE), '素材分块不存在');
      const bytes = new Uint8Array(await blob.slice(data.index * CHUNK_SIZE, (data.index + 1) * CHUNK_SIZE).arrayBuffer());
      return { base64: base64(bytes), total: Math.ceil(blob.size / CHUNK_SIZE) };
    }
    if (message.type === 'BEFORE_SUBMIT') {
      assert(run.status === 'running' && item.status === 'preparing', '队列已暂停或此镜头已提交，禁止重复生成');
      item.status = 'submitting'; item.submittedAt = new Date().toISOString(); await saveRun(run); return {};
    }
    if (message.type === 'PROGRESS') {
      if (data.phase === 'submitted' && item.status === 'submitting') { item.status = 'submitted'; await saveRun(run); }
      return {};
    }
    if (message.type === 'RESULT') {
      assert(['submitting', 'submitted', 'uncertain'].includes(item.status), '此镜头尚未提交生成');
      item.resultUrl = downloadUrl(data.url);
      await downloadResult(run, item);
      return {};
    }
    assert(typeof data.message === 'string', '错误信息无效');
    if (item.status === 'downloading') return { ignored: true };
    item.status = data.afterSubmit || ['submitting', 'submitted', 'uncertain'].includes(item.status) ? 'uncertain' : 'failed';
    item.error = data.message.slice(0, 1500); if (run.status !== 'stopped') run.status = 'paused'; await saveRun(run); return {};
  }

  fromDashboard(sender);
  if (message.type === 'CONNECT_SOURCE') {
    const origin = sourceOrigin(data.origin);
    assert(await chrome.permissions.contains({ origins: [`${origin}/*`] }), '尚未授予项目站点权限');
    const { sourceOrigins = [] } = await chrome.storage.local.get('sourceOrigins');
    const parsed = new URL(origin);
    if (!(parsed.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(parsed.hostname))) {
      const id = `source-${await sha256(new TextEncoder().encode(origin))}`;
      const existing = await chrome.scripting.getRegisteredContentScripts({ ids: [id] });
      if (!existing.length) await chrome.scripting.registerContentScripts([{ id, matches: [`${origin}/*`], js: ['source-bridge.js'], runAt: 'document_idle' }]);
    }
    await chrome.storage.local.set({ sourceOrigins: [...new Set([...sourceOrigins, origin])] });
    return { origin };
  }
  if (message.type === 'FLOW_TABS') return (await chrome.tabs.query({ currentWindow: true })).filter(t => isFlowUrl(t.url)).map(t => ({ id: t.id, title: t.title, url: t.url, active: t.active }));
  if (['CALIBRATE', 'INSPECT'].includes(message.type)) {
    const state = await inspectFlow(data.tabId);
    if (message.type === 'CALIBRATE') { await chrome.tabs.update(data.tabId, { active: true }); return flowCommand(data.tabId, 'CALIBRATE', { field: data.field }); }
    return state;
  }
  if (message.type === 'START') {
    assert(data.confirmed === true, '请核对 Flow 模型、时长、素材用途和费用后开始');
    const existing = (await all('runs')).find(r => !['completed', 'stopped'].includes(r.status));
    assert(!existing, '请先处理未结束的队列');
    const bundle = await read('bundles', data.bundleId);
    assert(bundle, '镜头包不存在');
    const items = validateSelection(bundle, data.selections, data.mode);
    assert(typeof data.settingsNote === 'string' && data.settingsNote.trim().length >= 4 && data.settingsNote.length <= 500, '请记录已在 Flow 设置的模型、时长和画幅');
    const { tab, profile } = await checkedProfile(data.tabId, items);
    const run = { id: crypto.randomUUID(), bundleId: bundle.id, tabId: tab.id, pageUrl: tab.url, profile,
      mode: data.mode, settingsNote: data.settingsNote, items, status: 'running', createdAt: new Date().toISOString() };
    await saveRun(run);
    await advance(run);
    return { runId: run.id };
  }
  const run = await read('runs', data.runId);
  assert(run, '队列不存在');
  if (message.type === 'PAUSE' || message.type === 'STOP') {
    run.status = message.type === 'STOP' ? 'stopped' : 'paused';
    for (const item of run.items) if (ACTIVE.includes(item.status) && item.status !== 'downloading') {
      item.status = item.status === 'preparing' ? 'queued' : 'uncertain'; item.error = '已停止自动操作，请在 Flow 核对本镜是否已提交';
    }
    await saveRun(run);
    await flowCommand(run.tabId, 'STOP').catch(() => {});
    return {};
  }
  if (message.type === 'RETRY_DOWNLOAD') {
    const item = run.items.find(i => i.shotId === data.shotId);
    assert(item?.status === 'download_failed' && item.resultUrl, '没有可重试的下载');
    await downloadResult(run, item); return {};
  }
  if (message.type === 'RESOLVE') {
    assert(run.status === 'paused' && ['retry', 'skip'].includes(data.action) && data.confirmed, '请先核对 Flow 结果');
    const item = run.items.find(i => i.shotId === data.shotId);
    assert(item && ['uncertain', 'failed'].includes(item.status), '此镜头无需处理');
    if (data.action === 'retry') {
      const pending = run.items.filter(i => ['queued', 'failed', 'uncertain'].includes(i.status));
      const { profile } = await checkedProfile(run.tabId, pending, run.pageUrl);
      run.profile = profile;
    }
    await flowCommand(run.tabId, 'STOP').catch(() => {});
    item.status = data.action === 'retry' ? 'queued' : 'skipped'; item.error = ''; await saveRun(run); return {};
  }
  assert(message.type === 'RESUME' && run.status === 'paused', '队列操作无效');
  assert(!run.items.some(i => ['uncertain', 'failed', 'download_failed'].includes(i.status)), '请先处理失败或状态不明的镜头');
  const pending = run.items.filter(i => i.status === 'queued');
  if (pending.length) {
    const { profile } = await checkedProfile(run.tabId, pending, run.pageUrl);
    run.profile = profile;
  }
  run.status = 'running'; await saveRun(run); await advance(run); return {};
}

async function reconcile() {
  for (const run of await all('runs')) {
    for (const item of run.items.filter(i => i.status === 'downloading')) {
      if (item.downloadId == null) {
        item.status = 'download_failed'; item.error = '下载启动回执丢失，请检查下载列表后仅重试下载';
        if (run.status !== 'stopped') run.status = 'paused';
        await saveRun(run); continue;
      }
      const [download] = await chrome.downloads.search({ id: item.downloadId });
      if (download?.state === 'complete') { item.status = 'completed'; item.error = ''; item.filename = download.filename; await saveRun(run); }
      else if (!download || download.state === 'interrupted') { item.status = 'download_failed'; item.error = '下载未完成，视频已生成；请只重试下载'; if (run.status !== 'stopped') run.status = 'paused'; await saveRun(run); }
    }
    if (run.status !== 'running') continue;
    const item = run.items.find(i => ['preparing', 'submitting', 'submitted'].includes(i.status));
    if (item) {
      try {
        const status = await flowCommand(run.tabId, 'STATUS');
        assert(status?.runId === run.id && status?.shotId === item.shotId && status?.attemptId === item.attemptId, '页面已刷新或任务中断');
        assert(status.active !== false && status.outcome !== 'error', status.message || '页面任务已结束但缺少结果回执');
      } catch (error) {
        item.status = 'uncertain'; item.error = `需要核对 Flow：${error.message}`; run.status = 'paused'; await saveRun(run);
      }
    } else await advance(run);
  }
}

chrome.runtime.onMessage.addListener((message, sender, respond) => {
  if (!['source', 'worker'].includes(message?.target)) return;
  serial(() => message.target === 'source' ? sourceMessage(message, sender) : workerMessage(message, sender))
    .then(data => respond({ ok: true, data }), error => respond({ ok: false, error: error.message || '操作失败', reason: error.reason, field: error.field }));
  return true;
});
void chrome.sidePanel.setPanelBehavior({ openPanelOnActionClick: true }).catch(console.error);
chrome.alarms.onAlarm.addListener(alarm => { if (alarm.name === 'flow-queue') void serial(reconcile).catch(console.error); });
chrome.downloads.onChanged.addListener(() => { void serial(reconcile).catch(console.error); });
chrome.runtime.onStartup.addListener(() => { void chrome.alarms.create('flow-queue', { periodInMinutes: 0.5 }); });
chrome.runtime.onInstalled.addListener(() => { void chrome.alarms.create('flow-queue', { periodInMinutes: 0.5 }); });
