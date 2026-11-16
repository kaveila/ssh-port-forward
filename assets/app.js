import { Call } from "/wails/runtime.js";

const SERVICE = "main.ForwardService";
const state = {
  hosts: [],
  selectedHost: null,
  forwards: [],
  refreshing: false,
  detailRequest: 0,
  pendingDelete: "",
  pendingDeleteTimer: null,
};

const elements = {
  refreshButton: document.querySelector("#refreshButton"),
  configPath: document.querySelector("#configPath"),
  hostCount: document.querySelector("#hostCount"),
  notice: document.querySelector("#notice"),
  searchInput: document.querySelector("#searchInput"),
  searchCount: document.querySelector("#searchCount"),
  hostList: document.querySelector("#hostList"),
  detailLayer: document.querySelector("#detailLayer"),
  backButton: document.querySelector("#backButton"),
  detailTitle: document.querySelector("#detailTitle"),
  detailAddress: document.querySelector("#detailAddress"),
  detailStatus: document.querySelector("#detailStatus"),
  detailLight: document.querySelector("#detailLight"),
  forwardList: document.querySelector("#forwardList"),
  forwardCount: document.querySelector("#forwardCount"),
  form: document.querySelector("#forwardForm"),
  kind: document.querySelector("#kind"),
  bindAddress: document.querySelector("#bindAddress"),
  localPort: document.querySelector("#localPort"),
  remoteHost: document.querySelector("#remoteHost"),
  remotePort: document.querySelector("#remotePort"),
  bindLabel: document.querySelector("#bindLabel"),
  localPortLabel: document.querySelector("#localPortLabel"),
  autoStart: document.querySelector("#autoStart"),
  startNow: document.querySelector("#startNow"),
  submitButton: document.querySelector("#submitButton"),
  toast: document.querySelector("#toast"),
};

let toastTimer;

function rpc(method, ...args) {
  return Call.ByName(`${SERVICE}.${method}`, ...args);
}

function formatError(error) {
  if (!error) return "未知错误";
  if (typeof error === "string") return error;
  return error.message || error.error || JSON.stringify(error);
}

function showToast(message) {
  elements.toast.textContent = message;
  elements.toast.classList.add("show");
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => elements.toast.classList.remove("show"), 2600);
}

function setBusy(busy) {
  elements.refreshButton.classList.toggle("loading", busy);
  elements.refreshButton.disabled = busy;
}

function hostMatches(host, query) {
  if (!query) return true;
  const haystack = `${host.name} ${host.hostName} ${host.user}`.toLocaleLowerCase();
  return haystack.includes(query.toLocaleLowerCase());
}

function addressFor(host) {
  const identity = host.user ? `${host.user}@` : "";
  return `${identity}${host.hostName}:${host.port}`;
}

function makeLight(running, large = false) {
  const light = document.createElement("span");
  light.className = `light ${running ? "running" : "idle"}${large ? " large" : ""}`;
  light.setAttribute("aria-label", running ? "正在转发" : "未转发");
  return light;
}

function renderHosts() {
  const query = elements.searchInput.value.trim();
  const visibleHosts = state.hosts.filter(host => hostMatches(host, query));
  elements.searchCount.textContent = query ? `${visibleHosts.length}/${state.hosts.length}` : "";
  elements.hostCount.textContent = String(state.hosts.length);
  elements.hostList.replaceChildren();

  if (visibleHosts.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty-state";
    const title = document.createElement("p");
    title.textContent = state.hosts.length === 0 ? "SSH 配置中没有 Host" : "没有匹配的 Host";
    const hint = document.createElement("p");
    hint.textContent = state.hosts.length === 0 ? "可点击右上角重新读取配置" : "换一个关键词试试";
    empty.append(title, hint);
    elements.hostList.append(empty);
    return;
  }

  const fragment = document.createDocumentFragment();
  for (const host of visibleHosts) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "host-row";
    row.dataset.host = host.name;
    row.setAttribute("aria-label", `打开 ${host.name} 的端口转发`);

    const avatar = document.createElement("span");
    avatar.className = "host-avatar";
    avatar.textContent = host.name.slice(0, 1);

    const copy = document.createElement("span");
    copy.className = "host-copy";
    const titleLine = document.createElement("span");
    titleLine.className = "host-title-line";
    const name = document.createElement("span");
    name.className = "host-name";
    name.textContent = host.name;
    titleLine.append(name);
    const address = document.createElement("span");
    address.className = "host-address";
    address.textContent = addressFor(host);
    copy.append(titleLine, address);

    const forwardState = document.createElement("span");
    forwardState.className = "host-state";
    const badge = document.createElement("span");
    badge.className = `forward-badge${host.activeCount > 0 ? " has-active" : ""}`;
    badge.textContent = host.forwardCount ? `${host.activeCount}/${host.forwardCount}` : "0";
    const chevron = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    chevron.setAttribute("viewBox", "0 0 24 24");
    chevron.classList.add("chevron");
    chevron.innerHTML = '<path d="m9 18 6-6-6-6"/>';
    forwardState.append(makeLight(host.forwarding), badge, chevron);

    row.append(avatar, copy, forwardState);
    row.addEventListener("click", () => openDetail(host));
    fragment.append(row);
  }
  elements.hostList.append(fragment);
}

async function refreshHosts({ silent = false } = {}) {
  if (state.refreshing) return;
  state.refreshing = true;
  if (!silent) setBusy(true);
  try {
    const snapshot = await rpc("GetHosts");
    elements.configPath.textContent = snapshot.configPath || "~/.ssh/config";
    elements.configPath.title = snapshot.configPath || "";
    state.hosts = Array.isArray(snapshot.hosts) ? snapshot.hosts : [];
    elements.notice.hidden = !snapshot.error;
    elements.notice.textContent = snapshot.error || "";

    if (state.selectedHost) {
      const updated = state.hosts.find(host => host.name === state.selectedHost.name);
      if (updated) {
        state.selectedHost = updated;
        renderDetailHeading();
      } else {
        closeDetail();
      }
    }
    renderHosts();
  } catch (error) {
    elements.notice.hidden = false;
    elements.notice.textContent = formatError(error);
    if (!silent) showToast(formatError(error));
  } finally {
    state.refreshing = false;
    setBusy(false);
  }
}

function renderDetailHeading() {
  const host = state.selectedHost;
  if (!host) return;
  elements.detailTitle.textContent = host.name;
  elements.detailAddress.textContent = addressFor(host);
  elements.detailStatus.textContent = host.forwarding ? "TUNNEL ACTIVE" : "TUNNEL IDLE";
  elements.detailLight.className = `light large ${host.forwarding ? "running" : "idle"}`;
}

function openDetail(host) {
  state.selectedHost = host;
  state.forwards = [];
  elements.detailLayer.hidden = false;
  renderDetailHeading();
  renderForwards();
  refreshForwards();
}

function closeDetail() {
  state.selectedHost = null;
  state.forwards = [];
  state.detailRequest++;
  elements.detailLayer.hidden = true;
  clearPendingDelete();
}

function kindLabel(kind) {
  if (kind === "remote") return "远端";
  if (kind === "dynamic") return "SOCKS";
  return "本地";
}

function endpointText(forward) {
  if (forward.kind === "dynamic") {
    return `${forward.bindAddress}:${forward.localPort}`;
  }
  if (forward.kind === "remote") {
    return `远端 ${forward.localPort} → ${forward.remoteHost}:${forward.remotePort}`;
  }
  return `${forward.bindAddress}:${forward.localPort} → ${forward.remoteHost}:${forward.remotePort}`;
}

function renderForwards() {
  elements.forwardList.replaceChildren();
  elements.forwardCount.textContent = `${state.forwards.length} 条`;

  if (state.forwards.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty-state";
    empty.style.minHeight = "90px";
    empty.textContent = "还没有端口转发";
    elements.forwardList.append(empty);
    return;
  }

  const fragment = document.createDocumentFragment();
  for (const forward of state.forwards) {
    const card = document.createElement("article");
    card.className = `forward-card${forward.running ? " running" : ""}`;

    const top = document.createElement("div");
    top.className = "forward-top";
    top.append(makeLight(forward.running));

    const kind = document.createElement("span");
    kind.className = "kind-label";
    kind.textContent = kindLabel(forward.kind);

    const endpoint = document.createElement("code");
    endpoint.className = "endpoint";
    endpoint.title = endpointText(forward);
    endpoint.textContent = endpointText(forward);

    const actions = document.createElement("div");
    actions.className = "forward-actions";
    const toggle = document.createElement("button");
    toggle.type = "button";
    toggle.className = `mini-button ${forward.running ? "stop" : "primary"}`;
    toggle.textContent = forward.running ? "停止" : "启动";
    toggle.addEventListener("click", () => toggleForward(forward, toggle));
    actions.append(toggle);

    const remove = document.createElement("button");
    remove.type = "button";
    remove.className = `mini-button ${state.pendingDelete === forward.id ? "danger" : ""}`;
    remove.textContent = state.pendingDelete === forward.id ? "确认删除" : "删除";
    remove.addEventListener("click", () => requestDelete(forward, remove));
    actions.append(remove);

    top.append(kind, endpoint, actions);
    card.append(top);

    const meta = document.createElement("div");
    meta.className = "forward-meta";
    meta.append(makeLight(forward.running));
    const statusText = document.createElement("span");
    statusText.textContent = forward.running ? "运行中" : "已停止";
    meta.append(statusText);
    if (forward.running && forward.pid) {
      const pid = document.createElement("code");
      pid.textContent = `PID ${forward.pid}`;
      meta.append(pid);
    }
    if (forward.autoStart) {
      const auto = document.createElement("span");
      auto.textContent = "自动启动";
      meta.append(auto);
    }
    card.append(meta);

    if (forward.lastError) {
      const error = document.createElement("div");
      error.className = "forward-error";
      error.title = forward.lastError;
      error.textContent = forward.lastError;
      card.append(error);
    }
    fragment.append(card);
  }
  elements.forwardList.append(fragment);
}

async function refreshForwards({ silent = false } = {}) {
  const host = state.selectedHost;
  if (!host) return;
  const request = ++state.detailRequest;
  try {
    const forwards = await rpc("GetForwards", host.name);
    if (request !== state.detailRequest || state.selectedHost?.name !== host.name) return;
    state.forwards = Array.isArray(forwards) ? forwards : [];
    renderForwards();
  } catch (error) {
    if (!silent) showToast(formatError(error));
  }
}

async function toggleForward(forward, button) {
  button.disabled = true;
  button.textContent = forward.running ? "停止中…" : "启动中…";
  try {
    if (forward.running) {
      await rpc("StopForward", forward.id);
    } else {
      await rpc("StartForward", forward.id);
    }
    await refreshForwards();
    await refreshHosts({ silent: true });
  } catch (error) {
    await refreshForwards();
    await refreshHosts({ silent: true });
    showToast(formatError(error));
  } finally {
    button.disabled = false;
  }
}

function clearPendingDelete() {
  state.pendingDelete = "";
  clearTimeout(state.pendingDeleteTimer);
  state.pendingDeleteTimer = null;
}

function requestDelete(forward, button) {
  if (state.pendingDelete !== forward.id) {
    clearPendingDelete();
    state.pendingDelete = forward.id;
    renderForwards();
    state.pendingDeleteTimer = setTimeout(() => {
      clearPendingDelete();
      renderForwards();
    }, 3200);
    return;
  }

  clearPendingDelete();
  button.disabled = true;
  button.textContent = "删除中…";
  rpc("DeleteForward", forward.id)
    .then(async () => {
      await refreshForwards();
      await refreshHosts({ silent: true });
      showToast("端口转发已删除");
    })
    .catch(async error => {
      await refreshForwards();
      showToast(formatError(error));
    });
}

function updateFormForKind() {
  const kind = elements.kind.value;
  const remoteFields = document.querySelectorAll(".remote-field");
  remoteFields.forEach(field => field.classList.toggle("hidden", kind === "dynamic"));
  elements.localPortLabel.textContent = kind === "remote" ? "远端监听端口" : kind === "dynamic" ? "SOCKS 端口" : "本地端口";
  elements.bindLabel.textContent = kind === "remote" ? "远端监听地址" : "监听地址";
  elements.remoteHost.required = kind !== "dynamic";
  elements.remotePort.required = kind !== "dynamic";
}

async function submitForward(event) {
  event.preventDefault();
  if (!state.selectedHost) return;

  const kind = elements.kind.value;
  const input = {
    host: state.selectedHost.name,
    kind,
    bindAddress: elements.bindAddress.value.trim() || "127.0.0.1",
    localPort: Number(elements.localPort.value),
    remoteHost: kind === "dynamic" ? "" : elements.remoteHost.value.trim(),
    remotePort: kind === "dynamic" ? 0 : Number(elements.remotePort.value),
    autoStart: elements.autoStart.checked,
    startNow: elements.startNow.checked,
  };

  if (!Number.isInteger(input.localPort) || input.localPort < 1 || input.localPort > 65535) {
    showToast("监听端口必须在 1-65535 之间");
    elements.localPort.focus();
    return;
  }
  if (kind !== "dynamic") {
    if (!input.remoteHost) {
      showToast("请填写目标地址");
      elements.remoteHost.focus();
      return;
    }
    if (!Number.isInteger(input.remotePort) || input.remotePort < 1 || input.remotePort > 65535) {
      showToast("目标端口必须在 1-65535 之间");
      elements.remotePort.focus();
      return;
    }
  }

  elements.submitButton.disabled = true;
  const label = elements.submitButton.querySelector("span");
  const previousLabel = label.textContent;
  label.textContent = input.startNow ? "正在建立通道…" : "正在保存…";
  try {
    await rpc("AddForward", input);
    await refreshForwards();
    await refreshHosts({ silent: true });
    elements.localPort.value = "";
    elements.remoteHost.value = "";
    elements.remotePort.value = "";
    showToast(input.startNow ? "端口转发已启动" : "端口转发已保存");
  } catch (error) {
    await refreshForwards();
    await refreshHosts({ silent: true });
    showToast(formatError(error));
  } finally {
    elements.submitButton.disabled = false;
    label.textContent = previousLabel;
  }
}

elements.refreshButton.addEventListener("click", () => refreshHosts());
elements.searchInput.addEventListener("input", renderHosts);
elements.backButton.addEventListener("click", closeDetail);
elements.kind.addEventListener("change", updateFormForKind);
elements.form.addEventListener("submit", submitForward);
elements.detailLayer.addEventListener("click", event => {
  if (event.target === elements.detailLayer) closeDetail();
});
document.addEventListener("keydown", event => {
  if (event.key === "Escape" && !elements.detailLayer.hidden) closeDetail();
});

window.sshForwardRefresh = async () => {
  await refreshHosts();
  if (state.selectedHost) await refreshForwards();
};

updateFormForKind();
refreshHosts();

setInterval(() => {
  if (document.hidden) return;
  refreshHosts({ silent: true });
  if (state.selectedHost) refreshForwards({ silent: true });
}, 2500);
