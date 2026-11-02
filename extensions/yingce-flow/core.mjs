export const CHANNEL = 'yingce-flow-bridge-v1';
export const CHUNK_SIZE = 262144;
export const MAX_FILE = 128 * 1024 * 1024;
export const MAX_BUNDLE = 512 * 1024 * 1024;
export const ACTIVE = ['preparing', 'submitting', 'submitted', 'downloading'];

export function assert(condition, message) {
  if (!condition) throw new Error(message);
}

export function isFlowUrl(value) {
  try {
    const url = new URL(value);
    return url.protocol === 'https:' && !url.username && !url.password &&
      (url.hostname === 'flow.google.com' || (url.hostname === 'labs.google' && /^\/fx\/(?:[a-z-]+\/)?tools\/flow(?:\/|$)/.test(url.pathname)));
  } catch { return false; }
}

export function isFlowProjectUrl(value) {
  if (!isFlowUrl(value)) return false;
  const url = new URL(value);
  return url.hostname === 'flow.google.com'
    ? /^(?:\/u\/\d+)?\/project\/[^/]+(?:\/|$)/.test(url.pathname)
    : /^\/fx\/(?:[a-z-]+\/)?tools\/flow\/project\/[^/]+(?:\/|$)/.test(url.pathname);
}

export function sourceOrigin(value) {
  const url = new URL(value);
  assert(!url.username && !url.password, '站点地址不能包含凭据');
  assert(url.protocol === 'https:' || (url.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(url.hostname)), '项目站点必须使用 HTTPS，本机开发可用 HTTP');
  return url.origin;
}

function string(value, label, max = 100000) {
  assert(typeof value === 'string' && value.length <= max, `${label}格式无效或过长`);
  return value;
}

function identifier(value, label) {
  const result = string(value, label, 200);
  assert(/^[\w-]+$/.test(result), `${label}格式无效`);
  return result;
}

function unique(values, label) {
  assert(new Set(values).size === values.length, `${label}存在重复 ID`);
}

export function validateBundle(input, origin) {
  assert(input?.version === 1, '不支持的镜头包版本');
  assert(input.source?.origin === origin, '镜头包来源与页面不符');
  assert(Array.isArray(input.shots) && input.shots.length > 0 && input.shots.length <= 500, '每包须包含 1–500 镜');
  assert(Array.isArray(input.media) && input.media.length <= 2000, '素材数量超过限制');
  const bundle = {
    version: 1, id: identifier(input.id, '包 ID'), createdAt: string(input.createdAt, '创建时间', 100),
    source: { origin, projectId: identifier(input.source.projectId, '项目 ID'), unitId: identifier(input.source.unitId, '章节 ID'),
      projectTitle: string(input.source.projectTitle, '项目名称', 500), unitTitle: string(input.source.unitTitle, '章节名称', 500) },
    shots: [], media: [],
  };
  for (const shot of input.shots) {
    assert(shot.revision && shot.revision.id === shot.revisionId && shot.revision.shotId === shot.id, '镜头与脚本版本不匹配');
    const revision = {};
    for (const key of ['id', 'shotId', 'plotDescription', 'action', 'dialogue', 'shotSize', 'cameraAngle', 'cameraMovement', 'imagePrompt', 'videoPrompt', 'negativePrompt', 'continuityNotes', 'actionBeatsJson', 'createdAt']) {
      revision[key] = string(shot.revision[key] ?? '', `脚本 ${key}`);
    }
    revision.version = shot.revision.version;
    revision.durationMs = shot.revision.durationMs;
    assert(Number.isFinite(shot.durationMs) && shot.durationMs > 0 && shot.durationMs <= 3600000, '镜头时长无效');
    assert(Number.isFinite(shot.position), '镜头顺序无效');
    assert(Array.isArray(shot.mediaIds) && Array.isArray(shot.warnings), '镜头素材/警告格式无效');
    const prompt = string(shot.prompt, '视频提示词');
    assert(prompt.trim() && !/@\[asset:/.test(prompt), '提示词为空或包含未解析的项目素材引用');
    bundle.shots.push({ id: identifier(shot.id, '镜头 ID'), title: string(shot.title, '镜头名称', 500), position: shot.position,
      revisionId: identifier(shot.revisionId, '脚本版本 ID'), revision, prompt, durationMs: shot.durationMs,
      settings: { aspectRatio: string(shot.settings?.aspectRatio ?? '', '画幅', 50), resolution: string(shot.settings?.resolution ?? '', '分辨率', 50) },
      mediaIds: shot.mediaIds.map(id => identifier(id, '素材 ID')), warnings: shot.warnings.map(w => string(w, '提示', 2000)) });
  }
  for (const media of input.media) {
    assert(Number.isSafeInteger(media.byteLength) && media.byteLength > 0 && media.byteLength <= MAX_FILE, '单素材需为 1 字节至 128 MiB');
    assert(typeof media.mimeType === 'string' && /^(image\/(png|jpeg|webp|gif|avif)|video\/(mp4|webm|quicktime)|audio\/[\w.+-]+)$/.test(media.mimeType), '不支持的素材类型');
    assert(/^[a-f0-9]{64}$/.test(media.sha256), '素材缺少 SHA-256');
    assert(Array.isArray(media.roles) && Array.isArray(media.shotIds), '素材角色无效');
    const item = { id: identifier(media.id, '素材 ID'), name: string(media.name, '素材名称', 500), mimeType: media.mimeType,
      roles: media.roles.map(x => string(x, '素材角色', 100)), shotIds: media.shotIds.map(id => identifier(id, '镜头 ID')),
      byteLength: media.byteLength, sha256: media.sha256 };
    for (const key of ['assetId', 'assetVersionId', 'artifactId', 'status']) if (media[key] != null) item[key] = string(media[key], key, 200);
    bundle.media.push(item);
  }
  unique(bundle.shots.map(s => s.id), '镜头');
  unique(bundle.media.map(m => m.id), '素材');
  const mediaById = new Map(bundle.media.map(m => [m.id, m]));
  const shotById = new Map(bundle.shots.map(s => [s.id, s]));
  for (const shot of bundle.shots) {
    unique(shot.mediaIds, '镜头引用');
    for (const id of shot.mediaIds) assert(mediaById.get(id)?.shotIds.includes(shot.id), '镜头引用了缺失素材或素材归属错误');
  }
  for (const media of bundle.media) for (const id of media.shotIds) assert(shotById.get(id)?.mediaIds.includes(media.id), '素材镜头引用不完整');
  assert(bundle.media.reduce((sum, m) => sum + m.byteLength, 0) <= MAX_BUNDLE, '镜头包超过 512 MiB，请分批提交');
  assert(JSON.stringify(bundle).length <= 8 * 1024 * 1024, '脚本元数据过大，请分批提交');
  return bundle;
}

export function decodeChunk(base64, media, index, total) {
  assert(Number.isInteger(index) && index >= 0 && total === Math.ceil(media.byteLength / CHUNK_SIZE) && index < total, '素材分块序号无效');
  assert(typeof base64 === 'string' && base64.length <= Math.ceil(CHUNK_SIZE / 3) * 4 && /^[A-Za-z0-9+/]*={0,2}$/.test(base64), '素材分块格式无效');
  const bytes = Uint8Array.from(atob(base64), c => c.charCodeAt(0));
  assert(bytes.length === Math.min(CHUNK_SIZE, media.byteLength - index * CHUNK_SIZE), '素材分块长度不符');
  return bytes;
}

export function base64(bytes) {
  let text = '';
  for (let i = 0; i < bytes.length; i += 8192) text += String.fromCharCode(...bytes.subarray(i, i + 8192));
  return btoa(text);
}

export async function sha256(bytes) {
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return [...new Uint8Array(digest)].map(b => b.toString(16).padStart(2, '0')).join('');
}

export function validateSelection(bundle, selections, mode) {
  assert(['ingredients', 'frame', 'text'].includes(mode), '请选择 Flow 输入模式');
  assert(Array.isArray(selections) && selections.length > 0, '请至少选择一个镜头');
  unique(selections.map(s => s.shotId), '运行镜头');
  return selections.map(selection => {
    const shot = bundle.shots.find(s => s.id === selection.shotId);
    assert(shot && Array.isArray(selection.mediaIds), '运行镜头无效');
    unique(selection.mediaIds, '运行素材');
    assert(selection.mediaIds.every(id => shot.mediaIds.includes(id)), '不能使用其他镜头的素材');
    const media = selection.mediaIds.map(id => bundle.media.find(m => m.id === id));
    assert(mode !== 'text' || media.length === 0, '文生视频模式不能选素材');
    assert(mode !== 'frame' || (media.length === 1 && media[0].mimeType.startsWith('image/')), '首帧模式每镜须选择一张图片');
    assert(mode !== 'ingredients' || media.length > 0, '参考素材模式每镜至少选择一个素材');
    return { shotId: shot.id, mediaIds: selection.mediaIds, status: 'queued', error: '' };
  });
}

export function downloadUrl(value) {
  const url = new URL(value);
  if (url.protocol === 'blob:') { assert(isFlowUrl(url.pathname), '仅允许 Flow 页面产生的临时视频'); return value; }
  assert(url.protocol === 'https:' && !url.username && !url.password &&
    ['googleusercontent.com', 'googleapis.com', 'googlevideo.com', 'google.com', 'labs.google'].some(host => url.hostname === host || url.hostname.endsWith(`.${host}`)), '结果地址不是 Google 视频资源');
  return value;
}

export function downloadName(bundle, shot) {
  const clean = text => text.replace(/[<>:"/\\|?*\x00-\x1f]/g, '_').replace(/^[. ]+|[. ]+$/g, '').slice(0, 70) || 'shot';
  return `Yingce-Flow/${clean(bundle.source.projectTitle)}/${String(shot.position + 1).padStart(3, '0')}-${clean(shot.title)}-${shot.id.slice(0, 8)}.mp4`;
}

export function uploadName(media) {
  const extension = { 'image/png': 'png', 'image/jpeg': 'jpg', 'image/webp': 'webp', 'image/gif': 'gif', 'image/avif': 'avif', 'video/mp4': 'mp4', 'video/webm': 'webm', 'video/quicktime': 'mov', 'audio/mpeg': 'mp3', 'audio/wav': 'wav', 'audio/x-wav': 'wav', 'audio/mp4': 'm4a', 'audio/ogg': 'ogg', 'audio/flac': 'flac', 'audio/webm': 'webm' }[media.mimeType];
  assert(extension, '当前上传适配器不支持此文件格式；原始素材仍保留在镜头包中');
  const name = media.name.replace(/[<>:"/\\|?*\x00-\x1f]/g, '_').replace(/^[. ]+|[. ]+$/g, '').slice(0, 90) || 'reference';
  return `${name.replace(/\.(?:png|jpe?g|webp|gif|avif|mp4|webm|mov|mp3|wav|m4a|ogg|flac)$/i, '')}-${media.id.slice(0, 8)}.${extension}`;
}
