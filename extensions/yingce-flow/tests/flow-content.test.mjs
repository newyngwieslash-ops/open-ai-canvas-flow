import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { test } from "node:test";
import { pathToFileURL } from "node:url";
import { runInContext } from "node:vm";

const require = createRequire(new URL("../../../web/package.json", import.meta.url));
const { Window } = await import(pathToFileURL(require.resolve("happy-dom")).href);
const source = await readFile(new URL("../flow-content.js", import.meta.url), "utf8");
const prompt = "镜头从老槐树下的热粥缓慢推向半开的屋门，织布机梭子规律穿行，结尾停在屋内劳作剪影。";
const profile = { prompt: "#prompt", generate: "#generate", results: "#results", upload: "#upload" };
const fixture = '<form id="composer"><textarea id="prompt"></textarea><input id="upload" type="file" accept="image/png,image/jpeg" multiple><button id="generate" type="button">Generate</button></form><section id="results" aria-label="Generated videos"></section>';

function setup(t, { html = fixture, url = "https://flow.google.com/u/6/project/example", storage = {}, worker = () => ({ ok: true, data: null }) } = {}) {
  storage = structuredClone(storage);
  const window = new Window({ url });
  const document = window.document;
  document.body.innerHTML = html;
  // happy-dom has no layout engine; hidden/display checks still run in the real adapter.
  window.HTMLElement.prototype.getClientRects = function () { return [{ width: 120, height: 40 }]; };
  const listeners = [];
  const messages = [];
  const errors = [];
  window.chrome = {
    storage: { local: {
      async get(keys) { return Object.fromEntries(keys.filter((key) => key in storage).map((key) => [key, structuredClone(storage[key])])); },
      async set(values) { Object.assign(storage, structuredClone(values)); },
    } },
    runtime: {
      id: "yingce-flow-test",
      onMessage: { addListener(listener) { listeners.push(listener); } },
      async sendMessage(message) {
        messages.push(structuredClone(message));
        return worker(message, { window, document, messages });
      },
    },
  };
  window.fetch = () => { throw new Error("The adapter must not use a private/network API."); };
  window.addEventListener("error", (event) => errors.push(event.error));
  runInContext(source, window);
  t.after(async () => {
    for (const listener of listeners) listener({ target: "flow", type: "STOP" }, { id: window.chrome.runtime.id }, () => {});
    await new Promise((resolve) => setTimeout(resolve, 5));
    await window.happyDOM.close();
    assert.deepEqual(errors, []);
  });
  const request = (type, data) => new Promise((resolve) => {
    assert.equal(listeners.length, 1, "content listener installed only on supported top-level Flow pages");
    assert.equal(listeners[0]({ target: "flow", type, data }, { id: window.chrome.runtime.id }, resolve), true);
  });
  return { window, document, storage, messages, listeners, request, api: window.YingceFlowAdapter };
}

async function until(predicate, label = "condition", timeout = 4000) {
  const deadline = Date.now() + timeout;
  while (!predicate()) {
    if (Date.now() > deadline) assert.fail(`Timed out waiting for ${label}`);
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  return predicate();
}

function runData(overrides = {}) {
  return { runId: "batch-1", shotId: "shot-1", attemptId: "attempt-1", prompt, files: [], profile, timeoutMs: 6000, ...overrides };
}

function addResult(document, { text = prompt, src = "https://lh3.googleusercontent.com/generated.mp4", parent = document.querySelector("#results") } = {}) {
  const card = document.createElement("article");
  const caption = document.createElement("p");
  caption.textContent = text;
  const video = document.createElement("video");
  if (src) video.src = src;
  card.append(caption, video);
  parent.append(card);
  return { card, video };
}

test("accepts only explicit Flow HTTPS routes and rejects foreign extension messages", (t) => {
  const { api, listeners } = setup(t);
  for (const url of ["https://flow.google.com/u/6/?pli=1", "https://labs.google/fx/tools/flow/project/123", "https://labs.google/fx/zh-CN/tools/flow/project/123"]) {
    assert.equal(api.allowedUrl(url), true, url);
  }
  for (const url of ["http://flow.google.com/", "https://flow.google.com.evil.test/", "https://flow.google.com:444/", "https://user@flow.google.com/", "https://labs.google/fx/tools/flow-other", "https://labs.google/fx/tools/other"]) {
    assert.equal(api.allowedUrl(url), false, url);
  }
  assert.equal(listeners[0]({ target: "flow", type: "RUN", data: runData() }, { id: "other-extension" }, () => assert.fail("must ignore sender")), false);
  assert.equal(listeners[0]({ target: "worker", type: "STATUS" }, { id: "yingce-flow-test" }, () => assert.fail("must ignore target")), false);
});

test("does not register its listener on another host", (t) => {
  const { listeners } = setup(t, { url: "https://example.com/flow" });
  assert.equal(listeners.length, 0);
});

test("unique selectors prefer escaped IDs, then exact aria-label, then a unique path", (t) => {
  const { api, document } = setup(t);
  const textarea = document.querySelector("textarea");
  textarea.id = "prompt:1";
  assert.equal(api.selectorFor(textarea), "#prompt\\:1");
  textarea.removeAttribute("id");
  textarea.setAttribute("aria-label", "Movie prompt");
  assert.equal(api.resolveSelector(api.selectorFor(textarea)), textarea);
  assert.match(api.selectorFor(textarea), /aria-label/);
  textarea.setAttribute("aria-label", 'Prompt "movie"');
  assert.equal(api.resolveSelector(api.selectorFor(textarea)), textarea);
  const duplicate = textarea.cloneNode();
  textarea.parentElement.append(duplicate);
  assert.match(api.selectorFor(textarea), /nth-of-type/);
  assert.equal(api.resolveSelector(api.selectorFor(textarea)), textarea);
  assert.throws(() => api.resolveSelector("textarea"), /不唯一/);
  assert.throws(() => api.resolveSelector("[broken"), /无效/);
});

test("calibration consumes the click, persists flat selectors, and reports the selected field", async (t) => {
  const h = setup(t);
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  const response = await h.request("CALIBRATE", { field: "generate" });
  assert.equal(response.ok, true);
  assert.equal(response.data.calibrating, true);
  const click = new h.window.MouseEvent("click", { bubbles: true, cancelable: true });
  assert.equal(h.document.querySelector("#generate").dispatchEvent(click), false);
  await until(() => h.messages.some((message) => message.type === "CALIBRATED"));
  assert.equal(clicks, 0);
  assert.equal(h.storage.flowAdapterProfile.generate, "#generate");
  assert.equal(h.storage.flowAdapterOrigin, "https://flow.google.com");
  assert.deepEqual(h.messages.at(-1), { target: "worker", type: "CALIBRATED", data: { field: "generate", selector: "#generate" } });
  assert.equal(h.document.querySelector("[data-yingce-flow-calibration]"), null);
});

test("positional markers stay anchored to their container when another toolbar appears", (t) => {
  const h = setup(t, { html: '<section id="editor"><div><button>Send</button></div></section>' });
  const send = h.document.querySelector('button');
  const selector = h.api.selectorFor(send);
  const toolbar = h.document.createElement('aside');
  toolbar.innerHTML = '<div><button>Upload media</button></div>';
  h.document.body.prepend(toolbar);
  assert.equal(h.api.resolveSelector(selector), send);
  send.remove();
  assert.throws(() => h.api.resolveSelector(selector), /不存在或不唯一/);
});

test("Escape cancels calibration and body cannot be selected as the result container", async (t) => {
  const h = setup(t);
  await h.request("CALIBRATE", { field: "results" });
  h.document.body.dispatchEvent(new h.window.MouseEvent("click", { bubbles: true, cancelable: true }));
  assert.equal(h.storage.flowAdapterProfile, undefined);
  h.window.dispatchEvent(new h.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
  assert.equal(h.document.querySelector("[data-yingce-flow-calibration]"), null);
  assert.equal(h.messages.length, 0);
  assert.equal((await h.request("CALIBRATE", { field: "private-app-state" })).ok, false);
});

test("upload calibration observes the actual file input opened by the selected button", async (t) => {
  const h = setup(t, { html: '<button id="open-upload">Upload</button>' });
  let clicks = 0;
  h.document.querySelector("button").addEventListener("click", () => {
    clicks++;
    const input = h.document.createElement("input");
    input.type = "file";
    input.id = "created-input";
    input.accept = "image/*";
    h.document.body.append(input);
  });
  assert.equal((await h.request("CALIBRATE", { field: "upload" })).ok, true);
  h.document.querySelector("button").click();
  await until(() => h.messages.some((message) => message.type === "CALIBRATED"));
  assert.equal(clicks, 1);
  assert.equal(h.storage.flowAdapterProfile.upload, "#created-input");
  const inspected = await h.request("INSPECT");
  assert.equal(inspected.ok, true);
  assert.equal(inspected.data.inputs[0].selector, "#created-input");
  assert.equal(inspected.data.inputs[0].accept, "image/*");
});

function userClick(window, element) {
  for (const type of ["pointerdown", "mousedown", "pointerup", "mouseup", "click"]) {
    element.dispatchEvent(new window.MouseEvent(type, { bubbles: true, cancelable: true, button: 0 }));
  }
}

test('Upload media can be a custom text control and reports the current calibration attempt', async (t) => {
  const h = setup(t, { html: '<div id="upload-control" tabindex="0"><i>upload</i><span>Upload media</span></div>' });
  let clicks = 0;
  h.document.querySelector('#upload-control').addEventListener('click', () => {
    clicks++;
    const input = h.document.createElement('input'); input.type = 'file'; input.id = 'actual-media';
    h.document.body.append(input);
  });
  const response = await h.request('CALIBRATE', { field: 'upload' });
  userClick(h.window, h.document.querySelector('i'));
  assert.equal(clicks, 1);
  await until(() => h.storage.flowAdapterProfile?.upload);
  assert.equal(h.storage.flowAdapterProfile.upload, '#actual-media');
  assert.equal(typeof response.data.calibrationId, 'string');
  assert.equal(h.storage.flowCalibrationStatus.calibrationId, response.data.calibrationId);
  assert(h.storage.flowCalibrationStatus.sequence > 1);
});

test('Uploads is a category, and selecting it guides to Upload media without saving a marker', async (t) => {
  const h = setup(t, { html: '<div role="tab" id="category">Uploads</div>' });
  let clicks = 0;
  h.document.querySelector('#category').addEventListener('click', () => clicks++);
  await h.request('CALIBRATE', { field: 'upload' });
  userClick(h.window, h.document.querySelector('#category'));
  assert.equal(clicks, 1);
  assert.equal(h.storage.flowAdapterProfile, undefined);
  assert.match(h.storage.flowCalibrationStatus.message, /分类.*Upload media/);
  assert.equal(h.storage.flowCalibrationStatus.error, false);
});

test('a stale Generate marker colliding with Upload media gives a specific correction without clicking', async (t) => {
  const h = setup(t, { html: '<button id="old-position">Upload media</button>', storage: { flowAdapterProfile: { generate: '#old-position' } } });
  let clicks = 0;
  h.document.querySelector('button').addEventListener('click', () => clicks++);
  await h.request('CALIBRATE', { field: 'upload' });
  userClick(h.window, h.document.querySelector('button'));
  assert.equal(clicks, 0);
  assert.equal(h.storage.flowCalibrationStatus.error, true);
  assert.match(h.storage.flowCalibrationStatus.message, /重新标记.*生成按钮/);
});

test("upload calibration lets a pointerdown add menu and its portal menuitem receive the real events once", async (t) => {
  const h = setup(t, { html: '<button id="add" aria-haspopup="menu"><svg aria-hidden="true"></svg></button>' });
  let opened = 0, uploads = 0;
  h.document.querySelector("#add").addEventListener("pointerdown", () => {
    opened++;
    const menu = h.document.createElement("div"); menu.setAttribute("role", "menu");
    menu.innerHTML = '<div role="menuitem" id="upload-menu">Upload from device</div>';
    menu.querySelector("#upload-menu").addEventListener("pointerup", () => {
      uploads++;
      const input = h.document.createElement("input"); input.type = "file"; input.id = "menu-input";
      h.document.body.append(input);
    });
    h.document.body.append(menu);
  });
  await h.request("CALIBRATE", { field: "upload" });
  userClick(h.window, h.document.querySelector("svg"));
  assert(h.document.querySelector("#upload-menu"), "upload calibration must not swallow the pointerdown which opens the menu");
  userClick(h.window, h.document.querySelector("#upload-menu"));
  await until(() => h.storage.flowAdapterProfile?.upload);
  assert.equal(opened, 1); assert.equal(uploads, 1);
  assert.equal(h.storage.flowAdapterProfile.upload, "#menu-input");
  assert.match(h.storage.flowCalibrationStatus.message, /已标记/);
});

test("a literal plus can open an upload menu without an English upload label", async (t) => {
  const h = setup(t, { html: '<button id="add">＋</button>' });
  h.document.querySelector("#add").addEventListener("click", () => {
    const input = h.document.createElement("input"); input.type = "file"; input.id = "plus-input";
    h.document.body.append(input);
  });
  await h.request("CALIBRATE", { field: "upload" });
  userClick(h.window, h.document.querySelector("#add"));
  await until(() => h.storage.flowAdapterProfile?.upload);
  assert.equal(h.storage.flowAdapterProfile.upload, "#plus-input");
});

test("upload calibration does not bind an unrelated existing input or let a send button submit", async (t) => {
  const h = setup(t, { html: '<input id="library-upload" type="file"><button id="send" aria-label="Send">Send</button>' });
  let submissions = 0;
  for (const type of ["pointerdown", "click"]) h.document.querySelector("#send").addEventListener(type, () => submissions++);
  await h.request("CALIBRATE", { field: "upload" });
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(h.storage.flowAdapterProfile, undefined);
  userClick(h.window, h.document.querySelector("#send"));
  assert.equal(submissions, 0);
  await until(() => h.storage.flowCalibrationStatus?.error);
  assert.match(h.storage.flowCalibrationStatus.message, /上传/);
});

test("an upload button can activate its existing hidden input without submitting its form", async (t) => {
  const h = setup(t, { html: '<input id="unrelated" type="file"><form id="composer"><button id="add">Upload file</button><input id="actual" type="file" hidden></form>' });
  let uploads = 0, submits = 0;
  h.document.querySelector("form").addEventListener("submit", (event) => { event.preventDefault(); submits++; });
  h.document.querySelector("#add").addEventListener("click", () => { uploads++; h.document.querySelector("#actual").click(); });
  await h.request("CALIBRATE", { field: "upload" });
  userClick(h.window, h.document.querySelector("#add"));
  await until(() => h.storage.flowAdapterProfile?.upload);
  assert.equal(h.storage.flowAdapterProfile.upload, "#actual");
  assert.equal(uploads, 1);
  assert.equal(submits, 0);
});

test("ambiguous upload inputs are not guessed and a directly activated input resolves the choice", async (t) => {
  const h = setup(t, { html: '<button id="upload">Upload</button>' });
  h.document.querySelector("#upload").addEventListener("click", () => {
    for (const id of ["image-input", "audio-input"]) {
      const input = h.document.createElement("input"); input.type = "file"; input.id = id; h.document.body.append(input);
    }
  });
  await h.request("CALIBRATE", { field: "upload" });
  userClick(h.window, h.document.querySelector("#upload"));
  await until(() => h.storage.flowCalibrationStatus?.message.includes("多个"));
  assert.equal(h.storage.flowAdapterProfile, undefined);
  h.document.querySelector("#image-input").click();
  await until(() => h.storage.flowAdapterProfile?.upload);
  assert.equal(h.storage.flowAdapterProfile.upload, "#image-input");
});

test("cancelling a multi-step upload calibration stops observing later file controls", async (t) => {
  const h = setup(t, { html: '<button id="add" aria-haspopup="menu">+</button>' });
  await h.request("CALIBRATE", { field: "upload" });
  userClick(h.window, h.document.querySelector("#add"));
  h.window.dispatchEvent(new h.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
  const input = h.document.createElement("input"); input.type = "file"; h.document.body.append(input);
  await new Promise(resolve => setTimeout(resolve, 20));
  assert.equal(h.storage.flowAdapterProfile, undefined);
  assert.equal(h.document.querySelector("[data-yingce-flow-calibration]"), null);
});

test("INSPECT reports usable profile only in its calibrated origin and rejects broad result roots", async (t) => {
  const h = setup(t, { storage: { flowAdapterProfile: profile, flowAdapterOrigin: "https://flow.google.com" } });
  const response = await h.request("INSPECT");
  assert.equal(response.ok, true);
  assert.equal(response.data.configured, true);
  assert.equal(response.data.profile.prompt, "#prompt");
  assert.equal(response.data.uploadReady, true);
  h.storage.flowAdapterProfile.results = "body";
  const invalid = await h.request("INSPECT");
  assert.equal(invalid.data.configured, false);
  assert.match(invalid.data.problem, /专用结果列表/);
  h.storage.flowAdapterProfile = profile;
  h.storage.flowAdapterOrigin = "https://labs.google";
  assert.equal((await h.request("INSPECT")).data.configured, false);
});

test("INSPECT distinguishes a missing upload control from valid text generation controls", async (t) => {
  const h = setup(t, { storage: { flowAdapterProfile: profile, flowAdapterOrigin: "https://flow.google.com" } });
  h.document.querySelector("#upload").remove();
  const response = await h.request("INSPECT");
  assert.equal(response.data.configured, true);
  assert.equal(response.data.uploadReady, false);
  assert(response.data.uploadProblem.length > 0);
  assert.equal(response.data.inputs.length, 0);
});

test("INSPECT rejects an editor boundary that would include results before RUN", async (t) => {
  const h = setup(t, { html: '<main><section id="results"><img src="history.png"></section><form><textarea id="prompt"></textarea></form><aside><button id="generate">Upload media</button></aside></main>', storage: { flowAdapterProfile: profile } });
  const checked = await h.request('INSPECT');
  assert.equal(checked.data.configured, false);
  assert.match(checked.data.problem, /独立.*输入区/);
  assert.match(checked.data.problem, /Upload media/);
  assert.match(checked.data.problem, /发送箭头/);
  let clicks = 0;
  h.document.querySelector('button').addEventListener('click', () => clicks++);
  await h.request('RUN', runData());
  await until(() => h.messages.some(m => m.type === 'ERROR'));
  assert.equal(h.messages.find(m => m.type === 'ERROR').data.afterSubmit, false);
  assert.equal(h.messages.some(m => m.type === 'MEDIA_READ' || m.type === 'BEFORE_SUBMIT'), false);
  assert.equal(h.document.querySelector('#prompt').value, '');
  assert(h.document.querySelector('#results img'));
  assert.equal(clicks, 0);
});

test("form-associated send button outside the editor uses only its own form for preparation", async (t) => {
  const html = '<main><section id="results"><img src="history.png"><progress></progress></section><form id="composer"><textarea id="prompt"></textarea></form><button id="generate" type="button" form="composer">Send</button></main>';
  const h = setup(t, { html, storage: { flowAdapterProfile: profile } });
  assert.equal((await h.request('INSPECT')).data.configured, true);
  h.document.querySelector('#generate').addEventListener('click', () => addResult(h.document));
  await h.request('RUN', runData());
  await until(() => h.messages.some(m => m.type === 'RESULT'), 'form-associated generation');
  assert(h.document.querySelector('#results img'));
  assert.equal(h.messages.some(m => m.type === 'ERROR'), false);
});

test("a form association cannot allow result cards inside the attachment cleanup scope", async (t) => {
  const h = setup(t, { html: '<form id="composer"><textarea id="prompt"></textarea><section id="results"></section></form><button id="generate" form="composer">Send</button>', storage: { flowAdapterProfile: profile } });
  assert.equal((await h.request('INSPECT')).data.configured, false);
});

test("a nested editor stays separate when a surrounding form also contains the result list", async (t) => {
  const h = setup(t, { html: '<form><section id="results"><img src="history.png"></section><div id="composer"><textarea id="prompt"></textarea><button id="generate" type="button">Send</button></div></form>', storage: { flowAdapterProfile: profile } });
  assert.equal((await h.request('INSPECT')).data.configured, true);
  h.document.querySelector('#generate').addEventListener('click', () => addResult(h.document));
  await h.request('RUN', runData());
  await until(() => h.messages.some(m => m.type === 'RESULT'));
  assert(h.document.querySelector('#results img'));
});

test("file selection respects MIME, extension, multiplicity, and rejects ambiguous inputs", (t) => {
  const { api, document } = setup(t);
  const file = { name: "scene.PNG", mimeType: "image/png" };
  const input = document.querySelector("#upload");
  input.accept = ".png";
  assert.equal(api.accepts(input, file), true);
  input.accept = "image/*";
  assert.equal(api.accepts(input, file), true);
  assert.equal(api.accepts(input, { name: "voice.wav", mimeType: "audio/wav" }), false);
  input.accept = "audio/*";
  assert.equal(api.accepts(input, { name: "voice.wav", mimeType: "audio/wav" }), true, "capability is the selected input, not a guessed model whitelist");
  input.accept = "image/png";
  input.multiple = false;
  assert.throws(() => api.uploadInput(profile, [file, file]), /类型或数量/);
  assert.equal(api.uploadInput({}, [file]), input);
  const extra = input.cloneNode();
  extra.id = "second-upload";
  input.parentElement.append(extra);
  assert.throws(() => api.uploadInput({}, [file]), /唯一/);
});

test("prompt filling uses native textarea events and verifies contenteditable insertText", (t) => {
  const { api, document } = setup(t);
  const textarea = document.querySelector("#prompt");
  let nativeValue = "";
  textarea.addEventListener("input", () => { nativeValue = textarea.value; });
  api.fillPrompt(textarea, prompt);
  assert.equal(nativeValue, prompt);
  const editor = document.createElement("div");
  editor.setAttribute("contenteditable", "true");
  editor.textContent = "Previous prompt";
  document.body.append(editor);
  document.execCommand = (command, showUI, value) => {
    assert.equal(command, "insertText");
    assert.equal(showUI, false);
    assert.equal(document.getSelection().toString(), "Previous prompt");
    editor.textContent = value;
    return true;
  };
  api.fillPrompt(editor, prompt);
  assert.equal(editor.textContent, prompt);
  document.execCommand = () => false;
  assert.throws(() => api.fillPrompt(editor, prompt), /未接受/);
  document.execCommand = () => { editor.textContent = "truncated"; return true; };
  assert.throws(() => api.fillPrompt(editor, prompt), /不完整/);
});

test("correlation ignores baseline videos, late old cards, reused URLs, unrelated cards and global last video", (t) => {
  const { api, document } = setup(t);
  const results = document.querySelector("#results");
  const old = addResult(document, { src: "https://lh3.googleusercontent.com/old.mp4" });
  const pending = addResult(document, { src: "" });
  const baseline = api.snapshot(results);
  const key = api.promptKey(prompt);
  old.video.src = "https://lh3.googleusercontent.com/old-refreshed.mp4";
  pending.video.src = "https://lh3.googleusercontent.com/late-old.mp4";
  addResult(document, { src: "https://lh3.googleusercontent.com/old.mp4" });
  addResult(document, { text: "Another shot unrelated to this request", src: "https://lh3.googleusercontent.com/unrelated.mp4" });
  addResult(document, { parent: document.body, src: "https://lh3.googleusercontent.com/outside.mp4" });
  assert.equal(api.findResult(results, baseline, key), null);
  const fresh = addResult(document);
  assert.equal(api.findResult(results, baseline, key).url, fresh.video.src);
  addResult(document, { src: "https://lh3.googleusercontent.com/second.mp4" });
  assert.throws(() => api.findResult(results, baseline, key), /多个结果/);
});

test("result selection rejects grouped alternatives and unsafe URLs and uses a visible download link", (t) => {
  const { api, document } = setup(t);
  const results = document.querySelector("#results");
  const baseline = api.snapshot(results);
  const key = api.promptKey(prompt);
  const fresh = addResult(document);
  const extra = document.createElement("video");
  extra.src = "https://lh3.googleusercontent.com/alternate.mp4";
  fresh.card.append(extra);
  assert.throws(() => api.findResult(results, baseline, key), /结果不唯一/);
  extra.remove();
  fresh.video.src = "https://evil.example/steal.mp4";
  assert.throws(() => api.findResult(results, baseline, key), /不在允许范围/);
  fresh.video.src = "blob:https://flow.google.com/123456";
  assert.equal(api.findResult(results, baseline, key).url, fresh.video.src);
  const download = document.createElement("a");
  download.download = "shot.mp4";
  download.href = "https://storage.googleapis.com/flow/shot.mp4";
  fresh.card.append(download);
  assert.equal(api.findResult(results, baseline, key).url, download.href);
  download.hidden = true;
  assert.equal(api.findResult(results, baseline, key).url, fresh.video.src);
});

test("RUN responds immediately, requires durable acknowledgement before one click, and rejects duplicate RUN", async (t) => {
  let approve;
  const h = setup(t, { worker(message) {
    if (message.type === "BEFORE_SUBMIT") return new Promise((resolve) => { approve = resolve; });
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => { clicks++; addResult(h.document); });
  const response = await h.request("RUN", runData());
  assert.equal(response.ok, true);
  assert.equal(response.data.phase, "preparing");
  assert.equal((await h.request("RUN", runData({ shotId: "other-shot" }))).ok, false);
  await until(() => approve, "durable submission request");
  assert.equal(clicks, 0);
  assert.equal((await h.request("STATUS")).data.phase, "submitting");
  approve({ ok: true, data: null });
  await until(() => h.messages.some((message) => message.type === "RESULT"), "correlated result");
  assert.equal(clicks, 1);
  assert.deepEqual(h.messages.filter((message) => message.type !== "CALIBRATED").map((message) => message.type), ["BEFORE_SUBMIT", "PROGRESS", "RESULT"]);
  assert.equal((await h.request("RUN", runData())).ok, false);
  assert.equal((await h.request("STATUS")).data.afterSubmit, true);
});

test("worker refusal prevents submission and another shot in the same batch remains runnable", async (t) => {
  const h = setup(t, { worker(message) {
    return message.type === "BEFORE_SUBMIT" ? { ok: false, data: null, error: "Queue no longer owns this shot" } : { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  assert.equal((await h.request("RUN", runData())).ok, true);
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(clicks, 0);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
  assert.match(h.messages.at(-1).data.message, /Queue no longer owns/);
  assert.equal((await h.request("STATUS")).data.active, false);
  assert.equal((await h.request("RUN", runData({ shotId: "shot-2", attemptId: "attempt-2" }))).ok, true);
  await h.request("STOP");
});

test("STOP during the worker handshake prevents even a late approved click", async (t) => {
  let approve;
  const h = setup(t, { worker(message) {
    if (message.type === "BEFORE_SUBMIT") return new Promise((resolve) => { approve = resolve; });
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  await h.request("RUN", runData());
  await until(() => approve);
  assert.equal((await h.request("STOP", { runId: "wrong-run" })).ok, false);
  const stopped = await h.request("STOP", { runId: "batch-1", shotId: "shot-1" });
  assert.equal(stopped.data.afterSubmit, false);
  assert.equal(stopped.data.cancelled, true);
  assert.equal(stopped.data.attemptId, "attempt-1");
  approve({ ok: true, data: null });
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(clicks, 0);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
});

test("STOP after submission reports uncertainty, never cancels Flow or repeats Generate", async (t) => {
  const h = setup(t);
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  await h.request("RUN", runData());
  await until(() => h.messages.some((message) => message.type === "PROGRESS"));
  const stopped = await h.request("STOP");
  assert.equal(stopped.data.phase, "submitted");
  assert.equal(stopped.data.afterSubmit, true);
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(h.messages.at(-1).data.afterSubmit, true);
  assert.equal((await h.request("RUN", runData())).ok, false);
  assert.equal(clicks, 1);
});

test("MEDIA_READ reconstructs real files from 256 KiB chunks and waits for visible upload progress", async (t) => {
  const bytes = Buffer.alloc(262144 + 31, 19);
  bytes[262144] = 200;
  const h = setup(t, { worker(message) {
    if (message.type === "MEDIA_READ") {
      const { runId, shotId, attemptId, mediaId, index } = message.data;
      assert.equal(runId, "batch-1");
      assert.equal(shotId, "shot-1");
      assert.equal(attemptId, "attempt-1");
      assert.equal(mediaId, "media-1");
      return { ok: true, data: { total: 2, base64: bytes.subarray(index * 262144, (index + 1) * 262144).toString("base64") } };
    }
    return { ok: true, data: null };
  } });
  const input = h.document.querySelector("#upload");
  let finishUpload;
  let uploaded;
  input.addEventListener("change", () => {
    uploaded = input.files[0];
    const filename = h.document.createElement("span");
    filename.textContent = uploaded.name;
    h.document.querySelector("#composer").append(filename);
    const progress = h.document.createElement("progress");
    h.document.querySelector("#composer").append(progress);
    h.document.querySelector("#generate").disabled = true;
    finishUpload = () => { progress.remove(); h.document.querySelector("#generate").disabled = false; };
  });
  h.document.querySelector("#generate").addEventListener("click", () => addResult(h.document));
  assert.equal((await h.request("RUN", runData({ files: [{ id: "media-1", name: "scene.png", mimeType: "image/png", byteLength: bytes.length }] }))).ok, true);
  await until(() => uploaded, "file upload");
  assert.equal(uploaded.name, "scene.png");
  assert.equal(uploaded.type, "image/png");
  assert.equal(uploaded.size, bytes.length);
  assert.deepEqual(Buffer.from(await uploaded.arrayBuffer()), bytes);
  assert.deepEqual(h.messages.filter((message) => message.type === "MEDIA_READ").map((message) => message.data.index), [0, 1]);
  assert.equal(h.messages.some((message) => message.type === "BEFORE_SUBMIT"), false);
  finishUpload();
  await until(() => h.messages.some((message) => message.type === "RESULT"));
});

test("invalid media chunks fail before changing the composer or contacting BEFORE_SUBMIT", async (t) => {
  const h = setup(t, { worker(message) {
    return message.type === "MEDIA_READ" ? { ok: true, data: { total: 2, base64: "AQ==" } } : { ok: true, data: null };
  } });
  await h.request("RUN", runData({ files: [{ id: "bad-file", name: "bad.png", mimeType: "image/png", byteLength: 1 }] }));
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(h.document.querySelector("#prompt").value, "");
  assert.equal(h.messages.some((message) => message.type === "BEFORE_SUBMIT"), false);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
  assert.match(h.messages.at(-1).data.message, /分片元数据不一致/);
});

test("missing or incompatible upload targets fail without reading files or generating", async (t) => {
  const h = setup(t);
  await h.request("RUN", runData({ files: [{ id: "voice", name: "voice.wav", mimeType: "audio/wav", byteLength: 10 }] }));
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.deepEqual(h.messages.map((message) => message.type), ["ERROR"]);
  assert.equal(h.messages[0].data.afterSubmit, false);
});

test("SPA navigation during confirmation prevents clicking the new project's controls", async (t) => {
  let approve;
  const h = setup(t, { worker(message) {
    if (message.type === "BEFORE_SUBMIT") return new Promise((resolve) => { approve = resolve; });
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  await h.request("RUN", runData());
  await until(() => approve);
  h.window.history.pushState({}, "", "/u/2/project/different");
  approve({ ok: true, data: null });
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(clicks, 0);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
});

test("prompt mutation after the worker acknowledgement blocks submission", async (t) => {
  const h = setup(t, { worker(message, { document }) {
    if (message.type === "BEFORE_SUBMIT") document.querySelector("#prompt").value = "Page changed the prompt";
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  await h.request("RUN", runData());
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(clicks, 0);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
});

test("ambiguous post-submit results report ERROR afterSubmit=true and never RESULT", async (t) => {
  const h = setup(t);
  h.document.querySelector("#generate").addEventListener("click", () => {
    addResult(h.document);
    addResult(h.document, { src: "https://lh3.googleusercontent.com/second-result.mp4" });
  });
  await h.request("RUN", runData());
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(h.messages.at(-1).data.afterSubmit, true);
  assert.equal(h.messages.some((message) => message.type === "RESULT"), false);
});

test("CALIBRATE responds before a chosen input notifies the worker to avoid serial-queue deadlock", async (t) => {
  let acknowledge;
  const h = setup(t, { worker(message) {
    if (message.type === "CALIBRATED") return new Promise((resolve) => { acknowledge = resolve; });
    return { ok: true, data: null };
  } });
  const response = await Promise.race([
    h.request("CALIBRATE", { field: "upload" }),
    new Promise((_, reject) => setTimeout(() => reject(new Error("CALIBRATE waited for its own notification")), 500)),
  ]);
  assert.equal(response.ok, true);
  h.document.querySelector("#upload").click();
  await until(() => acknowledge);
  assert.equal(h.storage.flowAdapterProfile.upload, "#upload");
  acknowledge({ ok: true, data: null });
});

test("a stopped attempt cannot click or overwrite STATUS after a replacement attempt starts", async (t) => {
  let approveOld;
  let acknowledgeOldError;
  const h = setup(t, { worker(message) {
    if (message.type === "BEFORE_SUBMIT" && message.data.attemptId === "attempt-1") {
      return new Promise((resolve) => { approveOld = resolve; });
    }
    if (message.type === "ERROR" && message.data.attemptId === "attempt-1") {
      return new Promise((resolve) => { acknowledgeOldError = resolve; });
    }
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => { clicks++; addResult(h.document); });
  await h.request("RUN", runData());
  await until(() => approveOld);
  const stopped = await h.request("STOP", { attemptId: "attempt-1" });
  assert.equal(stopped.data.cancelled, true);
  assert.equal((await h.request("RUN", runData({ attemptId: "attempt-2" }))).ok, true);
  approveOld({ ok: true, data: null });
  await until(() => h.messages.some((message) => message.type === "RESULT" && message.data.attemptId === "attempt-2"));
  await until(() => acknowledgeOldError);
  acknowledgeOldError({ ok: true, data: null });
  await new Promise((resolve) => setTimeout(resolve, 10));
  const status = (await h.request("STATUS")).data;
  assert.equal(status.attemptId, "attempt-2");
  assert.equal(status.active, false);
  assert.equal(status.outcome, "complete");
  assert.equal(clicks, 1);
  assert.equal(h.messages.find((message) => message.type === "ERROR").data.attemptId, "attempt-1");
});

test("an initial composer containing an unknown image or text chip is rejected before media reads", async (t) => {
  const h = setup(t);
  const chip = h.document.createElement("div");
  chip.innerHTML = '<span>previous-user-file.png</span><button type="button" aria-label="Remove attachment">×</button>';
  h.document.querySelector("#composer").append(chip);
  await h.request("RUN", runData());
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.match(h.messages.at(-1).data.message, /已有未知/);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
  assert.equal(h.messages.some((message) => message.type === "BEFORE_SUBMIT"), false);
  assert.equal(chip.isConnected, true);
});

test("the next shot removes only previously uploaded named chips inside its composer", async (t) => {
  const h = setup(t, { worker(message) {
    if (message.type === "MEDIA_READ") return { ok: true, data: { total: 1, base64: "AQ==" } };
    return { ok: true, data: null };
  } });
  const composer = h.document.querySelector("#composer");
  let removals = 0;
  let generations = 0;
  h.document.querySelector("#upload").addEventListener("change", (event) => {
    for (const file of event.target.files) {
      const chip = h.document.createElement("div");
      chip.className = "reference-chip";
      const name = h.document.createElement("span");
      name.textContent = file.name;
      const image = h.document.createElement("img");
      image.src = "blob:https://flow.google.com/" + file.name;
      image.alt = file.name;
      const remove = h.document.createElement("button");
      remove.type = "button";
      remove.setAttribute("aria-label", "Remove attachment");
      remove.addEventListener("click", () => { removals++; chip.remove(); });
      chip.append(name, image, remove);
      composer.append(chip);
    }
  });
  const outside = h.document.createElement("button");
  outside.setAttribute("aria-label", "Remove attachment");
  outside.textContent = "scene-1.png";
  outside.addEventListener("click", () => assert.fail("must not remove anything from result history"));
  h.document.querySelector("#results").append(outside);
  h.document.querySelector("#generate").addEventListener("click", () => {
    generations++;
    assert.equal(composer.querySelectorAll(".reference-chip").length, 1);
    assert.equal(composer.querySelector(".reference-chip span").textContent, `scene-${generations}.png`);
    addResult(h.document, { src: `https://lh3.googleusercontent.com/result-${generations}.mp4` });
  });
  const file = (index) => [{ id: `media-${index}`, name: `scene-${index}.png`, mimeType: "image/png", byteLength: 1 }];
  await h.request("RUN", runData({ files: file(1) }));
  await until(() => h.messages.some((message) => message.type === "RESULT"));
  assert.equal((await h.request("RUN", runData({ shotId: "shot-2", attemptId: "attempt-2", files: file(2) }))).ok, true);
  await until(() => h.messages.filter((message) => message.type === "RESULT").length === 2);
  assert.equal(removals, 1);
  assert.equal(generations, 2);
  assert.equal(outside.isConnected, true);
  assert.equal(h.messages.some((message) => message.type === "ERROR"), false);
});

test("missing semantic removal for a previous uploaded reference blocks the next submit", async (t) => {
  const h = setup(t, { worker(message) {
    if (message.type === "MEDIA_READ") return { ok: true, data: { total: 1, base64: "AQ==" } };
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#upload").addEventListener("change", () => {
    const chip = h.document.createElement("span");
    chip.textContent = "scene.png";
    h.document.querySelector("#composer").append(chip);
  });
  h.document.querySelector("#generate").addEventListener("click", () => { clicks++; addResult(h.document); });
  await h.request("RUN", runData({ files: [{ id: "media", name: "scene.png", mimeType: "image/png", byteLength: 1 }] }));
  await until(() => h.messages.some((message) => message.type === "RESULT"));
  await h.request("RUN", runData({ shotId: "shot-2", attemptId: "attempt-2" }));
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.match(h.messages.at(-1).data.message, /移除按钮/);
  assert.equal(h.messages.at(-1).data.attemptId, "attempt-2");
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
  assert.equal(clicks, 1);
});

test("FileList alone and filenames inside the prompt never count as confirmed references", async (t) => {
  const h = setup(t, { worker(message) {
    if (message.type === "MEDIA_READ") return { ok: true, data: { total: 1, base64: "AQ==" } };
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  await h.request("RUN", runData({
    prompt: `${prompt}\n参考文件：scene.png`,
    files: [{ id: "media", name: "scene.png", mimeType: "image/png", byteLength: 1 }],
  }));
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.equal(h.document.querySelector("#upload").files.length, 1);
  assert.match(h.messages.at(-1).data.message, /未在编辑器中确认素材/);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
  assert.equal(h.messages.some((message) => message.type === "BEFORE_SUBMIT"), false);
  assert.equal(clicks, 0);
});

test("unknown late references added during durable confirmation prevent Generate", async (t) => {
  const h = setup(t, { worker(message, { document }) {
    if (message.type === "BEFORE_SUBMIT") {
      const image = document.createElement("img");
      image.src = "blob:https://flow.google.com/unexpected-reference";
      document.querySelector("#composer").append(image);
    }
    return { ok: true, data: null };
  } });
  let clicks = 0;
  h.document.querySelector("#generate").addEventListener("click", () => clicks++);
  await h.request("RUN", runData());
  await until(() => h.messages.some((message) => message.type === "ERROR"));
  assert.match(h.messages.at(-1).data.message, /与当前镜头不一致/);
  assert.equal(h.messages.at(-1).data.afterSubmit, false);
  assert.equal(clicks, 0);
});
