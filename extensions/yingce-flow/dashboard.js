import { all, read } from './db.mjs';
import { sourceOrigin, isFlowProjectUrl } from './core.mjs';

const $ = id => document.getElementById(id);
let bundles = [];
let bundle;
let previews = [];
let initialViewSet = false;
let activeCalibration;
const labels = { queued: '待生成', preparing: '上传与填写', submitting: '正在提交', submitted: 'Flow 生成中', downloading: '下载中', completed: '完成', failed: '提交前失败', uncertain: '须核对提交结果', download_failed: '须重试下载', skipped: '已跳过', running: '运行中', paused: '已暂停', stopped: '已停止' };

function element(tag, text, className) {
  const result = document.createElement(tag);
  if (text != null) result.textContent = text;
  if (className) result.className = className;
  return result;
}

function notice(text, error = false) { $('message').textContent = text; $('message').classList.toggle('error', error); }
function showCalibrationStatus(status) {
  if (!activeCalibration || status?.calibrationId !== activeCalibration.id || Number($('flow-tab').value) !== activeCalibration.tabId ||
    !Number.isSafeInteger(status.sequence) || status.sequence <= activeCalibration.sequence || typeof status.message !== 'string') return;
  activeCalibration.sequence = status.sequence;
  notice(status.message, status.error === true);
}
function showView(view) {
  document.querySelectorAll('[data-view]').forEach(button => {
    const active = button.dataset.view === view;
    button.setAttribute('aria-pressed', String(active));
    $(`view-${button.dataset.view}`).hidden = !active;
  });
  document.querySelector('main').scrollTop = 0;
}
async function send(type, data = {}) {
  let response;
  try { response = await chrome.runtime.sendMessage({ target: 'worker', type, data }); }
  catch (error) {
    if (/receiving end does not exist|could not establish connection|extension context invalidated/i.test(error.message || '')) {
      throw new Error('插件后台未连接。请在扩展管理页重新加载插件，关闭旧任务页，再点击工具栏的插件图标打开侧栏。');
    }
    throw error;
  }
  if (!response?.ok) throw Object.assign(new Error(response?.error || '插件未响应'), { reason: response?.reason, field: response?.field });
  return response.data;
}

function action(button, fn) {
  button.addEventListener('click', async () => {
    button.disabled = true;
    try { await fn(); }
    catch (error) {
      if (error.reason === 'flow_setup_required') { showView('settings'); $(error.field)?.focus(); }
      notice(error.message, true);
    }
    finally { button.disabled = false; }
  });
}

function button(text, fn) { const node = element('button', text); action(node, fn); return node; }
function selectedTab() {
  const id = Number($('flow-tab').value);
  if (!id) throw Object.assign(new Error('请在 Flow 打开具体项目，再在设置中点击刷新并选择该标签页。'), { reason: 'flow_setup_required', field: 'flow-tab' });
  return id;
}

async function refresh(receivedBundleId) {
  const previous = $('bundle').value;
  bundles = (await all('bundles')).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  $('bundle').replaceChildren(...bundles.map(b => new Option(`${b.source.projectTitle} / ${b.source.unitTitle} · ${b.shots.length} 镜 · ${new Date(b.createdAt).toLocaleString()}`, b.id)));
  if (!bundles.length) $('bundle').append(new Option('等待项目提交', ''));
  if (bundles.some(b => b.id === receivedBundleId)) $('bundle').value = receivedBundleId;
  else if (bundles.some(b => b.id === previous)) $('bundle').value = previous;
  if (bundle?.id !== $('bundle').value) await renderBundle();
  const oldTab = $('flow-tab').value;
  const tabs = await send('FLOW_TABS');
  $('flow-tab').replaceChildren(new Option('选择 Flow 项目', ''), ...tabs.map(t => new Option(`${isFlowProjectUrl(t.url) ? '项目' : '首页 / 请先进入项目'} · ${t.title || 'Flow'} · ${t.url}`, t.id)));
  if (tabs.some(t => String(t.id) === oldTab)) $('flow-tab').value = oldTab;
  else if (tabs.some(t => t.active)) $('flow-tab').value = String(tabs.find(t => t.active).id);
  else if (tabs.length === 1) $('flow-tab').value = String(tabs[0].id);
  const settings = await chrome.storage.local.get(['sourceOrigins', 'flowAdapterProfile']);
  $('origins').textContent = `已连接：${(settings.sourceOrigins || []).join('、') || '无'}`;
  const profile = settings.flowAdapterProfile || {};
  document.querySelectorAll('[data-field]').forEach(b => b.classList.toggle('ready', !!profile[b.dataset.field]));
  const fields = { prompt: '提示框', upload: '上传入口', generate: '生成按钮', results: '结果区域' };
  const missing = Object.entries(fields).filter(([field]) => !profile[field]).map(([, label]) => label);
  $('profile').textContent = `已保存标记 ${4 - missing.length}/4 项。${missing.length ? `尚未标记：${missing.join('、')}。` : ''}点击“检查页面”验证当前项目是否仍可使用这些标记。`;
  if (!initialViewSet) {
    showView(settings.sourceOrigins?.length && profile.prompt && profile.generate && profile.results ? 'shots' : 'settings');
    initialViewSet = true;
  }
  await renderRuns();
}

async function renderBundle() {
  bundle = bundles.find(b => b.id === $('bundle').value);
  previews.forEach(url => URL.revokeObjectURL(url)); previews = [];
  $('shots').replaceChildren(); $('confirm').checked = false;
  if (!bundle) { $('shots').append(element('p', '请从短剧分镜制作中「提交到 Flow」，镜头会显示在这里。', 'muted')); return; }
  for (const shot of bundle.shots) {
    const details = element('details', null, 'shot'); details.dataset.shotId = shot.id;
    const summary = element('summary');
    const checkbox = element('input'); checkbox.type = 'checkbox'; checkbox.checked = true; checkbox.className = 'choose-shot'; checkbox.setAttribute('aria-label', `选择${shot.title}`);
    checkbox.addEventListener('click', event => event.stopPropagation());
    summary.append(checkbox, element('span', shot.title, 'shot-title'), element('span', `${shot.durationMs / 1000} 秒 · ${shot.settings.aspectRatio} · 脚本 v${shot.revision.version}`, 'badge'));
    details.append(summary);
    if (shot.warnings.length) details.append(element('p', shot.warnings.join('\n'), 'warnings'));
    details.append(element('pre', shot.prompt, 'prompt'));
    const mediaList = element('div', null, 'media-list');
    for (const id of shot.mediaIds) {
      const media = bundle.media.find(m => m.id === id);
      const label = element('label', null, 'media');
      const check = element('input'); check.type = 'checkbox'; check.dataset.mediaId = id;
      check.checked = false;
      if (media.mimeType.startsWith('image/')) {
        const blob = await read('files', [bundle.id, id]);
        if (blob) { const url = URL.createObjectURL(blob); previews.push(url); const img = element('img'); img.src = url; img.alt = media.name; img.loading = 'lazy'; label.append(img); }
      }
      label.append(check, document.createTextNode(media.name), element('small', `${media.roles.join(' / ')} · ${(media.byteLength / 1048576).toFixed(1)} MiB${media.status === 'stale' ? ' · 已过期' : ''}`));
      const link = element('a', '下载原素材'); link.href = '#'; link.addEventListener('click', async event => {
        event.preventDefault();
        try { const file = await read('files', [bundle.id, id]); saveBlob(file, media.name); } catch (error) { notice(error.message, true); }
      }); label.append(link); mediaList.append(label);
    }
    details.append(mediaList); $('shots').append(details);
  }
}

function saveBlob(blob, name) {
  const url = URL.createObjectURL(blob);
  const a = element('a'); a.href = url; a.download = name; a.click();
  setTimeout(() => URL.revokeObjectURL(url), 30000);
}

async function renderRuns() {
  const runs = (await all('runs')).sort((a, b) => b.createdAt.localeCompare(a.createdAt));
  $('runs').replaceChildren();
  if (!runs.length) $('runs').append(element('p', '尚未启动队列。', 'muted'));
  for (const run of runs) {
    const source = bundles.find(b => b.id === run.bundleId);
    const card = element('div', null, 'run');
    card.append(element('strong', `${source?.source.unitTitle || '镜头包'} · ${labels[run.status] || run.status}`), element('p', run.settingsNote, 'muted'));
    const actions = element('div', null, 'row');
    if (run.status === 'running') actions.append(button('暂停后续镜头', async () => { await send('PAUSE', { runId: run.id }); await renderRuns(); }));
    if (run.status === 'paused') actions.append(button('继续队列', async () => { await send('RESUME', { runId: run.id }); await renderRuns(); }));
    if (!['completed', 'stopped'].includes(run.status)) actions.append(button('结束此队列', async () => {
      if (!confirm('结束后不再提交剩余镜头；Flow 已受理的生成仍会继续。')) return;
      await send('STOP', { runId: run.id }); await renderRuns();
    }));
    actions.append(button('下载结果清单', async () => saveBlob(new Blob([JSON.stringify({ source: source?.source, run }, null, 2)], { type: 'application/json' }), 'flow-results.json')));
    card.append(actions);
    for (const item of run.items) {
      const row = element('div', null, 'run-item');
      row.append(element('span', `${source?.shots.find(s => s.id === item.shotId)?.title || item.shotId} · ${labels[item.status] || item.status}`));
      if (item.error) row.append(element('p', item.error));
      if (item.downloadId != null && item.status === 'completed') row.append(button('在文件夹中显示', async () => chrome.downloads.show(item.downloadId)));
      if (item.status === 'download_failed') row.append(button('只重试下载', async () => { await send('RETRY_DOWNLOAD', { runId: run.id, shotId: item.shotId }); await renderRuns(); }));
      if (run.status === 'paused' && ['uncertain', 'failed'].includes(item.status)) {
        row.append(button('核对后重试', async () => {
          if (!confirm('请先在 Flow 核对：确定此镜头未被受理，或你明确要重新付费生成。重试可能产生额外费用。')) return;
          await send('RESOLVE', { runId: run.id, shotId: item.shotId, action: 'retry', confirmed: true }); await renderRuns();
        }), button('跳过此镜', async () => {
          if (!confirm('此镜头将标记为跳过，已有 Flow 结果请自行保留。')) return;
          await send('RESOLVE', { runId: run.id, shotId: item.shotId, action: 'skip', confirmed: true }); await renderRuns();
        }));
      }
      card.append(row);
    }
    $('runs').append(card);
  }
}

action($('connect'), async () => {
  const origin = sourceOrigin($('origin').value);
  if (!await chrome.permissions.request({ origins: [`${origin}/*`] })) throw new Error('未授予站点权限');
  await send('CONNECT_SOURCE', { origin }); await refresh(); notice('已连接。请刷新项目页面，再点击「提交到 Flow」。');
});
action($('refresh'), refresh);
document.querySelectorAll('[data-view]').forEach(button => button.addEventListener('click', () => showView(button.dataset.view)));
action($('inspect'), async () => {
  const state = await send('INSPECT', { tabId: selectedTab() });
  if (!state.configured) throw Object.assign(new Error(`页面已连接，控件尚未就绪：${state.problem || '请标记提示框、生成按钮和结果区域。'}`), { reason: 'flow_setup_required', field: 'calibration' });
  notice(`页面已连接，提示框、发送按钮、独立输入区和结果区域检查通过。${state.uploadReady ? '上传入口检查通过，可回到“镜头”核对参数并开始；已有暂停队列可在“队列”中核对后重试。' : '如需提交图片，请先展开并标记上传入口。'}`);
});
document.querySelectorAll('[data-field]').forEach(b => action(b, async () => {
  const tabId = selectedTab();
  activeCalibration = undefined;
  notice(b.dataset.field === 'upload'
    ? '请点击 Flow 输入框旁的“＋”，再点击素材弹窗左下角的“Upload media”（上传媒体）。“Uploads”只是素材分类。文件选择窗口可取消。'
    : '请在旁边的 Flow 页面按提示选择控件，侧栏可以保持打开。');
  const state = await send('CALIBRATE', { tabId, field: b.dataset.field });
  if (!state?.calibrationId) throw new Error('Flow 页面脚本尚未更新，请刷新 Flow 后重新标记。');
  activeCalibration = { id: state.calibrationId, tabId, sequence: 0 };
  const { flowCalibrationStatus } = await chrome.storage.local.get('flowCalibrationStatus');
  showCalibrationStatus(flowCalibrationStatus);
}));
$('bundle').addEventListener('change', () => { void renderBundle().catch(error => notice(error.message, true)); });
action($('select-all'), async () => document.querySelectorAll('.choose-shot').forEach(c => c.checked = true));
action($('select-none'), async () => document.querySelectorAll('.choose-shot').forEach(c => c.checked = false));
action($('export-json'), async () => { if (bundle) saveBlob(new Blob([JSON.stringify(bundle, null, 2)], { type: 'application/json' }), 'yingce-flow-bundle.json'); });
action($('start'), async () => {
  if (!bundle) throw new Error('请先从项目提交镜头');
  if (!$('confirm').checked) throw new Error('请先核对素材和 Flow 参数，并勾选确认');
  const selections = [...document.querySelectorAll('.shot')].filter(d => d.querySelector('.choose-shot').checked).map(d => ({ shotId: d.dataset.shotId, mediaIds: [...d.querySelectorAll('[data-media-id]:checked')].map(c => c.dataset.mediaId) }));
  await send('START', { bundleId: bundle.id, tabId: selectedTab(), mode: $('mode').value, settingsNote: $('settings').value, selections, confirmed: true });
  $('confirm').checked = false; await renderRuns(); showView('runs'); notice('队列已启动。请保留 Flow 项目标签页。');
});
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== 'local') return;
  showCalibrationStatus(changes.flowCalibrationStatus?.newValue);
  if (!['latestBundleId', 'flowAdapterProfile', 'sourceOrigins'].some(key => key in changes)) return;
  const receivedBundleId = changes.latestBundleId?.newValue;
  void refresh(receivedBundleId).then(() => {
    if (receivedBundleId) { showView('shots'); notice('已收到新的镜头包，请核对素材和参数。'); }
  }).catch(error => notice(error.message, true));
});
setInterval(() => { if (!document.hidden) void renderRuns().catch(error => notice(error.message, true)); }, 3000);
void refresh().catch(error => notice(error.message, true));
