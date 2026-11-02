(() => {
  "use strict";
  if (globalThis.YingceFlowAdapter) return;

  const CHUNK_BYTES = 262144;
  const FIELDS = ["prompt", "generate", "results", "upload"];
  const PROMPT_SELECTOR = 'textarea,[contenteditable="true"],[contenteditable="plaintext-only"]';
  const BUTTON_SELECTOR = 'button,[role="button"],input[type="submit"],input[type="button"]';
  let active = null;
  let last = null;
  let calibration = null;
  let uploadedAttachments = null;
  const seenAttempts = new Set();

  function allowedUrl(value) {
    try {
      const url = new URL(value);
      return url.protocol === "https:" && !url.username && !url.password && !url.port &&
        (url.hostname === "flow.google.com" ||
          (url.hostname === "labs.google" && /^\/fx\/(?:[a-z]{2}(?:-[A-Z]{2})?\/)?tools\/flow(?:\/|$)/.test(url.pathname)));
    } catch { return false; }
  }

  function assertPage() {
    if (window.top !== window || !allowedUrl(location.href)) throw new Error("仅支持 Flow 顶层页面。");
  }

  function normalizeText(value) {
    return String(value || "").replace(/\s+/gu, " ").trim();
  }

  function promptKey(prompt) {
    const key = Array.from(normalizeText(prompt)).slice(0, 96).join("");
    if (Array.from(key).length < 24) throw new Error("提示词至少需要 24 个字符，才能与生成结果可靠对应。");
    return key;
  }

  function resolveSelector(selector) {
    if (typeof selector !== "string" || !selector.trim() || selector.length > 4096) {
      throw new Error("缺少有效校准选择器，请重新校准。");
    }
    let matches;
    try { matches = document.querySelectorAll(selector); }
    catch { throw new Error("校准选择器无效，请重新校准。"); }
    if (matches.length !== 1) throw new Error("校准元素不存在或不唯一，请重新校准。");
    return matches[0];
  }

  function selectorFor(element) {
    if (!(element instanceof Element) || !element.isConnected || element.getRootNode() !== document) {
      throw new Error("请选择页面中的元素；暂不支持 Shadow DOM 或 iframe 内的控件。");
    }
    // A short positional suffix can silently move to a different toolbar after a re-render.
    const parts = [];
    for (let node = element; node; node = node.parentElement) {
      const anchors = [];
      if (node.id) anchors.push(`#${CSS.escape(node.id)}`);
      const label = node.getAttribute("aria-label");
      if (label) anchors.push(`${node.localName}[aria-label="${CSS.escape(label)}"]`);
      if (node === document.documentElement) anchors.push("html");
      for (const anchor of anchors) {
        try {
          if (document.querySelectorAll(anchor).length !== 1) continue;
          const selector = [anchor, ...parts].join(" > ");
          if (selector.length <= 2000 && document.querySelector(selector) === element) return selector;
        } catch { /* Fall back to an anchored DOM path for unsupported attribute selectors. */ }
      }
      if (!node.parentElement) break;
      const siblings = [...node.parentElement.children].filter((sibling) => sibling.localName === node.localName);
      parts.unshift(`${node.localName}:nth-of-type(${siblings.indexOf(node) + 1})`);
    }
    throw new Error("无法为这个元素建立唯一选择器。");
  }

  function visible(element) {
    if (!element?.isConnected || element.closest('[hidden],[inert],[aria-hidden="true"]')) return false;
    for (let node = element; node; node = node.parentElement) {
      const style = getComputedStyle(node);
      if (style.display === "none" || style.visibility === "hidden" || style.visibility === "collapse") return false;
    }
    return element.getClientRects().length > 0;
  }

  function disabled(element) {
    return !!element.disabled || element.getAttribute("aria-disabled") === "true" || !!element.closest("[inert]");
  }

  function validateField(field, element) {
    if (field === "prompt" && (!element.matches(PROMPT_SELECTOR) || element.readOnly || disabled(element))) {
      throw new Error("请选择可编辑的提示词输入框。");
    }
    if (field === "generate" && !element.matches(BUTTON_SELECTOR)) throw new Error("请选择生成按钮。");
    if (field === "upload" && (!element.matches('input[type="file"]') || disabled(element))) {
      throw new Error("请选择实际的文件上传控件。");
    }
    if (field === "results" && (element === document.body || element === document.documentElement ||
      element.matches('video,img,source,a,button,input,textarea') || element.matches(PROMPT_SELECTOR))) {
      throw new Error("请选择专用结果列表的空白处，不能选择整个页面或单个媒体元素。");
    }
    if (field !== "upload" && !visible(element)) throw new Error("校准元素当前不可见。");
    return element;
  }

  async function savedProfile() {
    const stored = await chrome.storage.local.get(["flowAdapterProfile", "flowAdapterOrigin"]);
    if (stored.flowAdapterOrigin && stored.flowAdapterOrigin !== location.origin) return {};
    return stored.flowAdapterProfile || {};
  }

  async function inspect() {
    const profile = await savedProfile();
    let problem = "", uploadProblem = "";
    try { composerFor(elementsFor(profile)); } catch (error) { problem = error.message; }
    try { validateField("upload", resolveSelector(profile.upload)); } catch (error) { uploadProblem = error.message; }
    return {
      url: location.href,
      profile,
      configured: !problem,
      problem,
      uploadReady: !uploadProblem,
      uploadProblem,
      inputs: [...document.querySelectorAll('input[type="file"]')].map((input) => ({
        selector: selectorFor(input), accept: input.accept, multiple: input.multiple,
      })),
    };
  }

  function worker(type, data, signal) {
    return new Promise((resolve, reject) => {
      let settled = false;
      const finish = (error, result) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        signal?.removeEventListener("abort", abort);
        if (error) reject(error); else resolve(result);
      };
      const abort = () => finish(new Error("已停止等待；已提交的 Flow 任务不会被取消。"));
      const timer = setTimeout(() => finish(new Error("插件后台未响应，请检查队列状态；不要重复提交。")), 30000);
      if (signal?.aborted) { abort(); return; }
      signal?.addEventListener("abort", abort, { once: true });
      Promise.resolve().then(() => chrome.runtime.sendMessage({ target: "worker", type, data })).then((reply) => {
        if (!reply?.ok) finish(new Error(typeof reply?.error === "string" ? reply.error : "插件后台拒绝了操作。"));
        else finish(null, reply.data);
      }, (error) => finish(error));
    });
  }

  async function calibrate(field) {
    if (!FIELDS.includes(field)) throw new Error("未知校准字段。");
    if (active) throw new Error("请先停止当前任务，再校准页面。");
    calibration?.();
    const startUrl = location.href;
    const calibrationId = crypto.randomUUID();
    const previousProfile = await savedProfile();
    let selecting = true;
    let observer;
    let lastNotice = "";
    let noticeSequence = 0;
    const host = document.createElement("div");
    host.dataset.yingceFlowCalibration = "true";
    host.style.cssText = "position:fixed;top:12px;left:12px;right:12px;z-index:2147483647;pointer-events:none";
    const shadow = host.attachShadow({ mode: "closed" });
    const panel = document.createElement("div");
    panel.style.cssText = "font:14px/1.6 system-ui;background:#172033;color:white;padding:12px 16px;border:2px solid #93c5fd;border-radius:8px;box-shadow:0 4px 18px #0006";
    panel.setAttribute("role", "status");
    panel.setAttribute("aria-live", "polite");
    const text = document.createElement("span");
    const instructions = {
      prompt: "请点击 Flow 提示词输入框。此次点击只用于校准。",
      generate: "请点击 Flow 生成按钮。此次点击不会生成视频。",
      results: "请点击专用结果列表的空白处，使所选容器包含结果卡片及其提示词。",
      upload: "请点击 Flow 输入框旁的“＋”，再点击素材弹窗左下角的“Upload media”（上传媒体）。“Uploads”是素材分类。文件选择窗口可取消。",
    };
    const report = (message, error = false) => {
      text.textContent = `影策 Flow 校准：${message}`;
      if (lastNotice === `${error}:${message}`) return;
      lastNotice = `${error}:${message}`;
      void chrome.storage.local.set({ flowCalibrationStatus: { calibrationId, sequence: ++noticeSequence, url: startUrl, field, message, error } }).catch((reason) => {
        text.textContent = `${message} 侧栏状态同步失败，请检查插件连接。`;
        console.warn("Flow calibration status could not be saved", reason);
      });
    };
    const cancel = document.createElement("button");
    cancel.textContent = "取消校准";
    cancel.style.cssText = "pointer-events:auto;margin-left:16px;padding:4px 12px;color:#172033;background:white;border-radius:4px";
    panel.append(text, cancel);
    shadow.append(panel);
    document.documentElement.append(host);

    const cleanup = () => {
      selecting = false;
      clearInterval(expiry);
      observer?.disconnect();
      window.removeEventListener("click", click, true);
      window.removeEventListener("keydown", keydown, true);
      for (const type of ["pointerdown", "pointerup", "mousedown", "mouseup"]) window.removeEventListener(type, blockPress, true);
      host.remove();
      if (calibration === cleanup) calibration = null;
    };
    const fail = (error) => {
      if (calibration !== cleanup) return;
      selecting = true;
      report(`校准未完成：${error.message} 可继续选择，或按 Esc 取消。`, true);
    };
    const save = async (element) => {
      if (!selecting) return;
      validateField(field, element);
      if (location.href !== startUrl) throw new Error("页面已切换，请在当前项目重新校准。");
      const selector = selectorFor(element);
      selecting = false;
      const profile = { ...await savedProfile(), [field]: selector };
      assertPage();
      if (location.href !== startUrl || calibration !== cleanup) throw new Error("校准已取消或页面已切换。");
      await chrome.storage.local.set({ flowAdapterProfile: profile, flowAdapterOrigin: location.origin });
      const label = { prompt: "提示框", generate: "生成按钮", results: "结果区域", upload: "上传入口" }[field];
      report(`已标记${label}。如弹出文件选择窗口，可取消；完成其他标记后点击“检查页面”。`);
      cleanup();
      try { await worker("CALIBRATED", { field, selector }); }
      catch (error) { report(`标记已保存，但插件后台未确认：${error.message} 请点击“检查页面”。`, true); }
    };
    const cancelSelection = () => { report("已取消标记。"); cleanup(); };
    const keydown = (event) => {
      if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); cancelSelection(); }
    };
    const consume = (event) => { event.preventDefault(); event.stopImmediatePropagation(); };
    const fileInputs = () => [...document.querySelectorAll('input[type="file"]')].filter((input) => !disabled(input));
    const uploadTarget = (element) => {
      if (!(element instanceof Element)) return {};
      const mediaAction = /^(?:(?:upload|file_upload|upload_file)\s*)?Upload media$|^上传媒体$|^上传文件$/i;
      let button = element.closest(`${BUTTON_SELECTOR},[role="menuitem"],[role="tab"],label,input[type="file"]`);
      // Allow the explicitly named media action when it is a custom text control.
      if (!button) {
        for (let node = element; node && node !== document.body; node = node.parentElement) {
          const name = normalizeText(node.getAttribute("aria-label") || node.getAttribute("title") || node.textContent);
          if (mediaAction.test(name) || /^Uploads$/i.test(name)) { button = node; break; }
        }
      }
      if (!button || disabled(button)) return {};
      const input = button.matches('input[type="file"]') ? button : button.control;
      if (input?.matches('input[type="file"]') && !disabled(input)) return { input };
      const label = normalizeText(button.getAttribute("aria-label") || button.getAttribute("title") || button.textContent);
      try {
        if (previousProfile.generate && button.matches(previousProfile.generate)) {
          return { problem: "这个位置与已保存的生成按钮标记重合。请先重新标记实际的生成按钮，再标记上传入口。" };
        }
      }
      catch { /* A stale saved selector must not allow a submit control through calibration. */ }
      if (/(\b(send|submit|generate)\b|发送|提交|生成)/i.test(label)) return {};
      const category = /^Uploads$/i.test(label);
      const uploadLabel = mediaAction.test(label) || /\b(uploads?|attach|attachment|add|file_upload|upload_file|attach_file|add_photo_alternate)\b|上传|添加|附件|本地文件|选择文件/i.test(label);
      const opensMenu = ["menu", "dialog", "true"].includes(button.getAttribute("aria-haspopup"));
      if (!uploadLabel && !opensMenu && !/^[+＋]$/.test(label)) return {};
      return { button, category };
    };
    const observeUpload = () => {
      if (observer) return;
      const existing = new Set(fileInputs());
      let ambiguous = false;
      observer = new MutationObserver(() => {
        if (!selecting || ambiguous) return;
        const added = fileInputs().filter((input) => !existing.has(input));
        if (added.length > 1) {
          ambiguous = true;
          fail(new Error("出现多个文件上传控件，无法确定目标。请点击菜单中具体的上传文件项，让插件识别实际使用的控件。"));
        } else if (added.length === 1) {
          void save(added[0]).catch(fail);
        }
      });
      observer.observe(document.documentElement, { childList: true, subtree: true });
      report("正在等待上传控件。请点击素材弹窗左下角的“Upload media”（上传媒体）；如弹出文件选择窗口，可取消。");
    };
    const blockPress = (event) => {
      if (event.composedPath().includes(host)) return;
      if (field === "upload" && selecting && uploadTarget(event.target).button) {
        // Let the user's real pointer sequence open menus, including portal menu items.
        observeUpload();
        return;
      }
      consume(event);
    };
    const click = (event) => {
      if (event.composedPath().includes(host)) return;
      if (!selecting) { consume(event); return; }
      let element = event.target;
      if (field === "upload") {
        const target = uploadTarget(element);
        if (target.input) { consume(event); void save(target.input).catch(fail); return; }
        if (target.button) {
          observeUpload();
          if (target.category) report("已选择 Uploads 素材分类。请继续点击弹窗左下角的“Upload media”（上传媒体），才能标记文件上传入口。");
          // Upload handlers may live in a form; do not allow its default submission.
          if (target.button.form && target.button.type === "submit") event.preventDefault();
          return;
        }
        consume(event);
        fail(new Error(target.problem || "请点击素材弹窗左下角的“Upload media”（上传媒体），或先点输入框旁的“＋”展开素材弹窗。其他控件在上传标记期间不会执行。"));
        return;
      }
      consume(event);
      if (!(element instanceof Element)) return;
      if (field === "prompt") element = element.closest(PROMPT_SELECTOR);
      if (field === "generate") element = element.closest(BUTTON_SELECTOR);
      if (!element) { fail(new Error("没有选中对应控件。")); return; }
      void save(element).catch(fail);
    };
    const expiresAt = Date.now() + 60000;
    const expiry = setInterval(() => {
      if (location.href !== startUrl || Date.now() >= expiresAt) {
        report(location.href !== startUrl ? "页面已切换，请重新标记。" : "标记已超时。请重新点击标记按钮，再展开 Flow 上传菜单。", true);
        cleanup();
      }
    }, 1000);
    cancel.addEventListener("click", cancelSelection);
    window.addEventListener("click", click, true);
    window.addEventListener("keydown", keydown, true);
    for (const type of ["pointerdown", "pointerup", "mousedown", "mouseup"]) window.addEventListener(type, blockPress, true);
    calibration = cleanup;
    report(`${instructions[field]} 按 Esc 取消。`);
    return { field, calibrationId, url: startUrl, calibrating: calibration === cleanup };
  }

  function elementsFor(profile) {
    const elements = Object.fromEntries(["prompt", "generate", "results"].map((field) =>
      [field, validateField(field, resolveSelector(profile?.[field]))]));
    if (elements.results.contains(elements.prompt) || elements.results.contains(elements.generate)) {
      throw new Error("结果区域包含编辑器或生成按钮，请校准更具体的结果列表。");
    }
    return elements;
  }

  function accepts(input, file) {
    const rules = input.accept.toLowerCase().split(",").map((rule) => rule.trim()).filter(Boolean);
    return !rules.length || rules.some((rule) =>
      rule.startsWith(".") ? file.name.toLowerCase().endsWith(rule) :
        rule.endsWith("/*") ? file.mimeType.toLowerCase().startsWith(rule.slice(0, -1)) :
          file.mimeType.toLowerCase() === rule);
  }

  function uploadInput(profile, files) {
    const suitable = (input) => !disabled(input) && (input.multiple || files.length === 1) && files.every((file) => accepts(input, file));
    if (profile.upload) {
      const input = validateField("upload", resolveSelector(profile.upload));
      if (!suitable(input)) throw new Error("上传控件不支持所选文件类型或数量，请调整 Flow 模式和素材选择。");
      return input;
    }
    const inputs = [...document.querySelectorAll('input[type="file"]')].filter(suitable);
    if (inputs.length !== 1) throw new Error("没有唯一且兼容的文件上传控件，请先打开上传入口并校准。");
    return inputs[0];
  }

  function composerFor(elements) {
    const isolated = (node) => node && node !== document.body && node !== document.documentElement && !node.contains(elements.results);
    let scope = elements.prompt.parentElement;
    while (scope && !scope.contains(elements.generate)) scope = scope.parentElement;
    if (!isolated(scope)) {
      // A native form owner can connect an external send button without including chat history.
      const form = elements.prompt.closest("form");
      if (isolated(form) && elements.generate.form === form) scope = form;
    }
    if (!isolated(scope)) {
      const label = Array.from(normalizeText(elements.generate.getAttribute("aria-label") ||
        elements.generate.getAttribute("title") || elements.generate.textContent || "无文字按钮")).slice(0, 60).join("");
      throw new Error(`无法确定独立输入区：生成按钮当前指向「${label}」，它与提示框的共同区域包含结果列表或整个页面。请重新标记底部提示框和旁边的发送箭头；结果区域须位于输入框之外。`);
    }
    return scope;
  }

  function attachmentNodes(scope, name, prompt) {
    return [...scope.querySelectorAll("*")].filter((element) => {
      if (element === prompt || prompt.contains(element)) return false;
      const values = [element.getAttribute("title"), element.getAttribute("aria-label"), element.getAttribute("alt")];
      if (element.children.length === 0 && !element.matches("input,textarea")) values.push(element.textContent);
      return values.some((value) => normalizeText(value) === name ||
        (element.matches(BUTTON_SELECTOR) && /^(remove|delete|clear|移除|删除|清除)[\s:：-]+/i.test(value || "") &&
          normalizeText(value).replace(/^(remove|delete|clear|移除|删除|清除)[\s:：-]+/i, "") === name));
    });
  }

  function attachmentEvidence(scope, prompt) {
    return [...scope.querySelectorAll('input[type="file"]')].some((input) => input.files?.length) ||
      [...scope.querySelectorAll('img[src],video,audio,[role="img"]')].some((element) => !prompt.contains(element) && visible(element)) ||
      [...scope.querySelectorAll(BUTTON_SELECTOR)].some((button) => !prompt.contains(button) && visible(button) &&
        /^(remove|delete)\b|^(移除|删除)/i.test(normalizeText(button.getAttribute("aria-label") || button.getAttribute("title") || button.textContent)));
  }

  async function clearPreviousAttachments(run) {
    const elements = elementsFor(run.profile);
    const scope = composerFor(elements);
    const owned = uploadedAttachments?.url === run.url ? uploadedAttachments.names : [];
    for (const name of owned) {
      check(run);
      const nodes = attachmentNodes(scope, name, elements.prompt);
      if (!nodes.length) continue;
      const controls = new Set();
      for (const node of nodes) {
        for (let chip = node; chip && chip !== scope; chip = chip.parentElement) {
          if (chip.contains(elements.prompt) || chip.contains(elements.generate)) break;
          if (owned.some((other) => other !== name && attachmentNodes(chip, other, elements.prompt).length)) break;
          const buttons = [...chip.querySelectorAll(BUTTON_SELECTOR)];
          if (chip.matches(BUTTON_SELECTOR)) buttons.push(chip);
          const removers = buttons.filter((button) => visible(button) && !disabled(button) &&
            /^(remove|delete|clear)\b|^(移除|删除|清除)/i.test(normalizeText(button.getAttribute("aria-label") || button.getAttribute("title") || button.textContent)));
          if (removers.length === 1) { controls.add(removers[0]); break; }
          if (removers.length > 1) break;
        }
      }
      if (controls.size !== 1) throw new Error("上个镜头的参考素材没有唯一的移除按钮，请手动清空编辑器后重试。");
      const button = [...controls][0];
      check(run);
      button.click();
      const removalDeadline = Date.now() + 5000;
      while (attachmentNodes(scope, name, elements.prompt).length) {
        if (Date.now() >= removalDeadline) throw new Error("未确认旧参考素材已移除，请手动清空编辑器。");
        await delay(run, 100);
      }
      if (!scope.isConnected) throw new Error("清理时编辑器已变化，请检查素材并重新校准。");
    }
    for (const input of scope.querySelectorAll('input[type="file"]')) {
      if (input.files?.length && [...input.files].every((file) => owned.includes(file.name))) input.value = "";
    }
    if (attachmentEvidence(scope, elements.prompt)) {
      throw new Error("编辑器中已有未知或无法确认清理的参考素材。请先手动清空，再开始队列。");
    }
    uploadedAttachments = null;
  }

  function assertAttachedFiles(run, elements) {
    const scope = composerFor(elements);
    const names = run.files.map((file) => file.name);
    if ([...scope.querySelectorAll('[role="alert"]')].some((element) => visible(element) && normalizeText(element.textContent))) {
      throw new Error("编辑器有可见错误提示，请处理后再提交。");
    }
    for (const name of names) {
      const confirmed = attachmentNodes(scope, name, elements.prompt)
        .some((element) => !element.matches("input,textarea") && visible(element));
      if (!confirmed) throw new Error(`未在编辑器中确认素材 ${name} 已附加。请检查上传结果；写入文件输入框不代表上传成功。`);
    }
    const previews = [...scope.querySelectorAll('img[src],video[src],audio[src]')]
      .filter((element) => !elements.prompt.contains(element) && visible(element));
    const unknownFiles = [...scope.querySelectorAll('input[type="file"]')]
      .some((input) => [...(input.files || [])].some((file) => !names.includes(file.name)));
    if (unknownFiles || previews.length > names.length || (!names.length && attachmentEvidence(scope, elements.prompt))) {
      throw new Error("编辑器参考素材与当前镜头不一致，请清空后重试。");
    }
  }

  function check(run) {
    assertPage();
    if (run.cancelled || run.controller.signal.aborted) throw new Error("已停止等待；已提交的 Flow 任务不会被取消。");
    if (location.href !== run.url) throw new Error("Flow 项目或账号页面已切换，已停止自动操作。");
    if (Date.now() >= run.deadline) throw new Error("等待 Flow 超时，请人工核对页面和队列，避免重复提交。");
  }

  function delay(run, milliseconds) {
    check(run);
    return new Promise((resolve, reject) => {
      const abort = () => { clearTimeout(timer); reject(new Error("已停止等待；已提交的 Flow 任务不会被取消。")); };
      const timer = setTimeout(() => { run.controller.signal.removeEventListener("abort", abort); resolve(); }, milliseconds);
      run.controller.signal.addEventListener("abort", abort, { once: true });
    });
  }

  function promptText(element) {
    return element.matches("textarea") ? element.value : (element.innerText || element.textContent);
  }

  function fillPrompt(element, prompt) {
    element.focus();
    if (element.matches("textarea")) {
      Object.getOwnPropertyDescriptor(HTMLTextAreaElement.prototype, "value").set.call(element, prompt);
      element.dispatchEvent(new Event("input", { bubbles: true }));
      element.dispatchEvent(new Event("change", { bubbles: true }));
    } else {
      const selection = window.getSelection();
      const range = document.createRange();
      range.selectNodeContents(element);
      selection.removeAllRanges();
      selection.addRange(range);
      if (!document.execCommand("insertText", false, prompt)) throw new Error("Flow 编辑器未接受文本，请重新校准提示词输入框。");
    }
    if (normalizeText(promptText(element)) !== normalizeText(prompt)) throw new Error("提示词填写不完整，已阻止提交。");
  }

  async function readFile(run, file) {
    const total = Math.ceil(file.byteLength / CHUNK_BYTES);
    const parts = [];
    for (let index = 0; index < total; index++) {
      check(run);
      const chunk = await worker("MEDIA_READ", { ...runIdentity(run), mediaId: file.id, index }, run.controller.signal);
      check(run);
      const expectedBytes = Math.min(CHUNK_BYTES, file.byteLength - index * CHUNK_BYTES);
      if (chunk?.total !== total || typeof chunk.base64 !== "string" ||
        chunk.base64.length !== 4 * Math.ceil(expectedBytes / 3) ||
        !/^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$/.test(chunk.base64)) {
        throw new Error("媒体分片元数据不一致，已阻止提交。");
      }
      const decoded = atob(chunk.base64);
      if (decoded.length !== expectedBytes) throw new Error("媒体分片长度不一致，已阻止提交。");
      parts.push(Uint8Array.from(decoded, (character) => character.charCodeAt(0)));
    }
    return new File(parts, file.name, { type: file.mimeType });
  }

  async function waitReady(run) {
    let readySince = null;
    // shortcut: readiness uses visible progress and Generate state; unsupported upload UIs need recalibration/manual review.
    while (true) {
      check(run);
      const elements = elementsFor(run.profile);
      const { generate, results } = elements;
      const scope = composerFor(elements);
      const busy = [...scope.querySelectorAll('progress,[role="progressbar"],[aria-busy="true"]')]
        .some((element) => !results.contains(element) && visible(element));
      const modal = [...document.querySelectorAll('dialog[open],[role="dialog"][aria-modal="true"]')].some(visible);
      if (!disabled(generate) && !busy && !modal) {
        readySince ??= Date.now();
        if (Date.now() - readySince >= 1000) return;
      } else readySince = null;
      await delay(run, 250);
    }
  }

  function videoUrl(video) {
    const value = video.currentSrc || video.getAttribute("src") || video.querySelector("source[src]")?.getAttribute("src");
    if (!value) return "";
    try { return new URL(value, location.href).href; } catch { return ""; }
  }

  function snapshot(results) {
    const videos = [...results.querySelectorAll("video")];
    return {
      videos: new Set(videos), urls: new Set(videos.map(videoUrl).filter(Boolean)),
      elements: new Set(results.querySelectorAll("*")), text: normalizeText(results.innerText || results.textContent),
    };
  }

  function safeResultUrl(value) {
    try {
      const url = new URL(value, location.href);
      if (url.username || url.password || url.port) return false;
      if (url.protocol === "blob:") return url.origin === location.origin && allowedUrl(location.href);
      return url.protocol === "https:" &&
        (url.hostname === "storage.googleapis.com" || url.hostname === "googleusercontent.com" ||
          url.hostname.endsWith(".googleusercontent.com") || url.hostname.endsWith(".googlevideo.com") ||
          url.origin === location.origin);
    } catch { return false; }
  }

  function findResult(results, baseline, key) {
    const matches = [];
    for (const video of results.querySelectorAll("video")) {
      const src = videoUrl(video);
      if (!src || baseline.videos.has(video) || baseline.urls.has(src) || !visible(video)) continue;
      for (let card = video.parentElement; card && card !== results; card = card.parentElement) {
        if (!normalizeText(card.innerText || card.textContent).includes(key)) continue;
        if (baseline.elements.has(card)) break;
        if (card.querySelectorAll("video").length !== 1) throw new Error("多个视频与提示词对应，结果不唯一；请人工核对。");
        const links = [...card.querySelectorAll("a[download][href]")].filter((link) => visible(link) && safeResultUrl(link.href));
        const url = links.length === 1 ? links[0].href : src;
        if (!safeResultUrl(url)) throw new Error("结果媒体地址不在允许范围内，请在 Flow 页面人工下载。");
        matches.push({ url });
        break;
      }
    }
    if (matches.length > 1) throw new Error("多个结果卡片与提示词对应，无法唯一确定本次结果；请人工核对。");
    return matches[0] || null;
  }

  async function execute(run) {
    try {
      check(run);
      const initial = elementsFor(run.profile);
      const input = run.files.length ? uploadInput(run.profile, run.files) : null;
      await clearPreviousAttachments(run);
      const files = [];
      for (const file of run.files) files.push(await readFile(run, file));
      check(run);
      fillPrompt(initial.prompt, run.prompt);
      if (input) {
        if (!input.isConnected || uploadInput(run.profile, run.files) !== input) throw new Error("上传控件已变化，请重新校准。");
        const transfer = new DataTransfer();
        for (const file of files) transfer.items.add(file);
        input.files = transfer.files;
        if (input.files.length !== files.length) throw new Error("文件未完整写入上传控件，已阻止提交。");
        uploadedAttachments = { url: run.url, names: files.map((file) => file.name) };
        input.dispatchEvent(new Event("input", { bubbles: true }));
        input.dispatchEvent(new Event("change", { bubbles: true }));
      }
      await waitReady(run);
      check(run);
      let elements = elementsFor(run.profile);
      if (normalizeText(promptText(elements.prompt)) !== normalizeText(run.prompt)) throw new Error("提示词被页面更改，已阻止提交。");
      assertAttachedFiles(run, elements);
      const generate = elements.generate;
      run.phase = "submitting";
      await worker("BEFORE_SUBMIT", runIdentity(run), run.controller.signal);
      check(run);
      elements = elementsFor(run.profile);
      if (elements.generate !== generate || disabled(elements.generate) || normalizeText(promptText(elements.prompt)) !== normalizeText(run.prompt)) {
        throw new Error("生成按钮或提示词状态已变化，已阻止提交。");
      }
      assertAttachedFiles(run, elements);
      const baseline = snapshot(elements.results);
      const results = elements.results;
      check(run);
      // Mark uncertain before clicking: a handler can submit successfully and then throw.
      run.afterSubmit = true;
      run.phase = "submitted";
      elements.generate.click();
      await worker("PROGRESS", { ...runIdentity(run), phase: "submitted" }, run.controller.signal);
      while (true) {
        check(run);
        if (!results.isConnected || resolveSelector(run.profile.results) !== results) {
          throw new Error("结果区域已重建，无法可靠对应提交记录；请人工核对。");
        }
        const result = findResult(results, baseline, run.key);
        if (result) {
          run.resultUrl = result.url;
          await worker("RESULT", { ...runIdentity(run), url: result.url }, run.controller.signal);
          run.outcome = "complete";
          break;
        }
        await delay(run, 500);
      }
    } catch (error) {
      run.outcome = "error";
      run.message = error instanceof Error ? error.message : String(error);
      try { await worker("ERROR", { ...runIdentity(run), message: run.message, afterSubmit: run.afterSubmit }); }
      catch { /* STATUS retains the failure when the worker is unavailable. */ }
    } finally {
      if (last?.attemptId === run.attemptId) last = statusOf(run);
      if (active === run) active = null;
    }
  }

  function runIdentity(run) {
    return { runId: run.runId, shotId: run.shotId, attemptId: run.attemptId };
  }

  function statusOf(run) {
    return run ? {
      ...runIdentity(run), phase: run.phase, afterSubmit: run.afterSubmit, cancelled: run.cancelled,
      outcome: run.outcome || null, message: run.message || null, resultUrl: run.resultUrl || null,
    } : { runId: null, shotId: null, attemptId: null, phase: null, afterSubmit: false, cancelled: false };
  }

  function start(data) {
    if (active) throw new Error("本页面已有任务运行，禁止重复提交。");
    if (!data || typeof data.runId !== "string" || !data.runId.trim() ||
      typeof data.attemptId !== "string" || !data.attemptId.trim() ||
      typeof data.shotId !== "string" || !data.shotId.trim() || typeof data.prompt !== "string") {
      throw new Error("缺少有效任务、镜头或提示词。");
    }
    if (seenAttempts.has(data.attemptId)) throw new Error("此尝试已在本页面执行过，请核对队列，禁止重复提交。");
    const key = promptKey(data.prompt);
    const files = data.files || [];
    if (!Array.isArray(files) || files.some((file) => !file || typeof file.id !== "string" || !file.id.trim() ||
      typeof file.name !== "string" || !file.name.trim() || /[\/\\\x00-\x1f]/.test(file.name) ||
      typeof file.mimeType !== "string" || !/^[\w.+-]+\/[\w.+-]+$/.test(file.mimeType) ||
      !Number.isSafeInteger(file.byteLength) || file.byteLength <= 0) ||
      new Set(files.map((file) => file.id)).size !== files.length || new Set(files.map((file) => file.name)).size !== files.length) {
      throw new Error("媒体描述无效或重复。");
    }
    const timeoutMs = data.timeoutMs ?? 15 * 60 * 1000;
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs <= 0) throw new Error("任务超时设置无效。");
    const profile = Object.fromEntries(FIELDS.map((field) => [field, data.profile?.[field] || ""]));
    for (const field of ["prompt", "generate", "results"]) {
      if (typeof profile[field] !== "string" || !profile[field]) throw new Error("请先校准提示词、生成按钮和结果列表。");
    }
    calibration?.();
    const run = {
      runId: data.runId, shotId: data.shotId, attemptId: data.attemptId, prompt: data.prompt, files, profile, key,
      url: location.href, deadline: Date.now() + timeoutMs, controller: new AbortController(),
      phase: "preparing", afterSubmit: false, cancelled: false,
    };
    seenAttempts.add(run.attemptId);
    active = run;
    last = statusOf(run);
    setTimeout(() => { void execute(run); }, 0);
    return { accepted: true, ...statusOf(run) };
  }

  async function command(message) {
    assertPage();
    switch (message.type) {
      case "INSPECT": return inspect();
      case "CALIBRATE": return calibrate(message.data?.field);
      case "RUN": return start(message.data);
      case "STATUS": return { url: location.href, active: !!active, ...(active ? statusOf(active) : last || statusOf(null)) };
      case "STOP": {
        calibration?.();
        if (active && ((message.data?.runId && message.data.runId !== active.runId) ||
          (message.data?.shotId && message.data.shotId !== active.shotId) ||
          (message.data?.attemptId && message.data.attemptId !== active.attemptId))) throw new Error("停止请求与当前任务不匹配。");
        const stopped = !!active;
        if (active) {
          active.cancelled = true;
          active.controller.abort();
          last = statusOf(active);
        }
        const status = active ? statusOf(active) : last || statusOf(null);
        active = null;
        return { stopped, ...status };
      }
      default: throw new Error("未知 Flow 页面命令。");
    }
  }

  // Exposed only in the extension's isolated world; tests exercise the same DOM helpers.
  globalThis.YingceFlowAdapter = Object.freeze({ allowedUrl, selectorFor, resolveSelector, accepts, uploadInput, fillPrompt, promptKey, snapshot, findResult });
  if (window.top !== window || !allowedUrl(location.href) || !globalThis.chrome?.runtime?.onMessage) return;
  chrome.runtime.onMessage.addListener((message, sender, sendResponse) => {
    if (message?.target !== "flow" || sender.id !== chrome.runtime.id) return false;
    void command(message).then(
      (data) => sendResponse({ ok: true, data, error: null }),
      (error) => sendResponse({ ok: false, data: null, error: error instanceof Error ? error.message : String(error) }),
    );
    return true;
  });
})();
