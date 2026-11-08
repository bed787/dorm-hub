"use strict";
(function (root) {
  const escape = root.DormHubUtils?.escape || (typeof require === "function" ? require("./utils.js").escape : null);

  function rows(items) {
    if (!items.length) return '<p class="muted">Сообщений по этому фильтру нет.</p>';
    return items.map(entry => {
      const date = new Date(entry.time);
      const stamp = Number.isNaN(date.getTime()) ? entry.time : date.toLocaleString("ru-RU");
      const kind = entry.level === "ERROR" ? "error" : entry.level === "WARN" ? "warn" : "info";
      return `<article class="log-entry log-entry-${kind}"><div class="log-meta"><time datetime="${escape(entry.time)}">${escape(stamp)}</time><strong>${escape(entry.level)}</strong><span>#${escape(entry.id)}</span></div><p>${escape(entry.message)}</p>${entry.details && entry.details !== "{}" ? `<pre>${escape(entry.details)}</pre>` : ""}</article>`;
    }).join("");
  }

  // One request at a time; never write into a page after navigation or logout.
  function create({container, api, active, onUnauthorized = () => {}, later = setTimeout, cancel = clearTimeout}) {
    const form = container.querySelector("[data-log-filters]");
    const output = container.querySelector("[data-log-list]");
    const status = container.querySelector("[data-log-state]");
    const refresh = container.querySelector("[data-log-refresh]");
    const auto = form.elements.auto;
    let stopped = false, pending = false, queued = false, timer = null, version = 0;
    const alive = () => !stopped && active();
    const clear = () => { if (timer !== null) cancel(timer); timer = null; };
    function schedule(delay = 5000) {
      clear();
      if (alive() && auto.checked) timer = later(load, delay);
    }
    async function load() {
      clear();
      if (!alive()) return;
      if (pending) { queued = true; return; }
      pending = true;
      refresh.disabled = true;
      const requestVersion = version;
      const query = new URLSearchParams({level: form.elements.level.value, q: form.elements.q.value.trim()});
      status.textContent = "Загружаем…";
      let mayRefresh = true;
      try {
        const page = await api("/admin/logs?" + query);
        if (!alive() || requestVersion !== version) return;
        output.innerHTML = rows(page.items);
        status.textContent = `Показано ${page.items.length} из ${page.matched} совпадений. Обновлено ${new Date().toLocaleTimeString("ru-RU")}.`;
      } catch (error) {
        if (!alive() || requestVersion !== version) return;
        status.textContent = "Не удалось загрузить логи: " + error.message;
        if (error.status === 401 || error.status === 403) {
          mayRefresh = false;
          auto.checked = false;
          output.innerHTML = "";
          if (error.status === 401) onUnauthorized();
        }
      } finally {
        pending = false;
        if (alive()) {
          refresh.disabled = false;
          if (queued) { queued = false; void load(); }
          else if (mayRefresh) schedule();
        }
      }
    }
    function filters(event) {
      if (event.target === auto) {
        if (auto.checked) void load(); else clear();
        return;
      }
      version++;
      clear();
      if (pending) queued = true;
      else timer = later(load, 250);
    }
    function submit(event) { event.preventDefault(); void load(); }
    function click() { void load(); }
    form.addEventListener("submit", submit);
    form.addEventListener("input", filters);
    refresh.addEventListener("click", click);
    void load();
    return {
      stop() {
        stopped = true;
        clear();
        form.removeEventListener("submit", submit);
        form.removeEventListener("input", filters);
        refresh.removeEventListener("click", click);
      },
    };
  }
  const logs = {rows, create};
  if (typeof module !== "undefined" && module.exports) module.exports = logs;
  else root.DormHubLogs = logs;
})(globalThis);
