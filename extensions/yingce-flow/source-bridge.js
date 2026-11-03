(() => {
  if (globalThis.__yingceFlowBridge) return;
  globalThis.__yingceFlowBridge = true;
  const channel = 'yingce-flow-bridge-v1';
  const accepted = new Set(['PING', 'BEGIN', 'MEDIA_CHUNK', 'COMMIT', 'ABORT']);
  window.addEventListener('message', async event => {
    const message = event.data;
    if (event.source !== window || event.origin !== location.origin || message?.channel !== channel ||
      !accepted.has(message.type) || typeof message.requestId !== 'string' || message.requestId.length > 100 || message.replyTo) return;
    let response;
    try { response = await chrome.runtime.sendMessage({ target: 'source', type: message.type, data: message.data }); }
    catch { response = { ok: false, error: '插件已重新加载，请刷新项目页面后重试' }; }
    window.postMessage({ channel, replyTo: message.requestId, ...response }, location.origin);
  });
})();
