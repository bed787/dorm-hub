"use strict";
const { escape: h, minor, price } = DormHubUtils;
const state = {
  me: null,
  email: "",
  csrf: "",
  guestCSRF: "",
  catalog: null,
  seq: 0,
  formListing: null,
  createAttempt: null,
  pending: [],
  photoWork: Promise.resolve(),
  resendAt: 0,
  editor: null,
  completedDeals: 0,
  deletionResendAt: 0,
};
const app = document.querySelector("#app"),
  dialog = document.querySelector("#dialog");
const categoryIcons = {
  things: "◇",
  study: "▤",
  help: "✦",
  housing: "⌂",
  other: "◌",
};
const locationNames = {
  VASILEOSTROVSKY: "Василеостровский район",
  PETRODVORTSOVY: "Петродворцовый район",
  NEVSKY: "Невский район",
  UNSPECIFIED: "Район нужно уточнить",
};
const categoryNames = {
  things: "Вещи и техника",
  study: "Учёба",
  help: "Услуги и помощь",
  housing: "Жильё и общежития",
  other: "Другое",
};
let toastTimer,
  autosaveTimer,
  routeGeneration = 0;
const editorStore = globalThis.DormHubEditorStore || {
  get: async () => null,
  put: async () => {},
  remove: async () => {},
};
function toast(message, error = false) {
  const el = document.querySelector("#toast");
  el.textContent = message;
  el.classList.toggle("error", error);
  el.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    el.hidden = true;
  }, 6000);
}
const { api, requestKey } = DormHubRequests.createClient(state, (...args) =>
  fetch(...args),
);
function brand() {
  return '<a class="brand row" href="#/feed" aria-label="Dorm Hub — лента"><img src="/assets/icon.svg" alt=""><span>dorm-hub</span></a>';
}
function themeButton() {
  return '<button type="button" class="theme-toggle" data-theme-toggle aria-label="Переключить тему">◐</button>';
}
function date(value) {
  return new Intl.DateTimeFormat("ru-RU", {
    day: "numeric",
    month: "short",
    year: "numeric",
  }).format(new Date(value));
}
function ago(value) {
  const delta = Date.now() - new Date(value).getTime();
  if (delta < 60000) return "Только что";
  if (delta < 3600000) return Math.floor(delta / 60000) + " мин назад";
  if (delta < 86400000) return Math.floor(delta / 3600000) + " ч назад";
  return date(value);
}
function option(value, label, current) {
  return `<option value="${h(value)}" ${String(value) === String(current) ? "selected" : ""}>${h(label)}</option>`;
}
function options(items, current, empty = "Выберите") {
  return (
    option("", empty, current) +
    items.map((x) => option(x.code ?? x.id, x.name, current)).join("")
  );
}
function dormOptions(location, current, empty = "Без общежития") {
  if (!location) return option("", "Сначала выберите район", "");
  return options(
    state.catalog.dorms.filter((d) => d.location === location),
    current,
    empty,
  );
}
function closeDialog() {
  dialog.close();
  document.querySelector("#dialog-content").innerHTML = "";
}
function modal(title, content) {
  document.querySelector("#dialog-content").innerHTML =
    `<button class="dialog-close" data-action="close-dialog" aria-label="Закрыть">×</button><h2 id="dialog-title">${h(title)}</h2>${content}`;
  dialog.showModal();
}
function releasePending() {
  state.pending.forEach((p) => URL.revokeObjectURL(p.url));
  state.pending = [];
}
function shell() {
  const path = (location.hash || "#/feed").split("?")[0];
  const nav = [
    ["#/feed", "▦", "Лента"],
    ["#/mine", "▤", "Мои объявления"],
    ["#/favorites", "♡", "Избранное"],
    ["#/settings", "⚙", "Мой профиль"],
  ];
  if (state.me.is_admin) nav.push(["#/admin", "◈", "Модерация"]);
  app.innerHTML = `<header class="topbar"><div class="row"><button class="mobile-menu" data-action="menu" aria-label="Открыть меню">☰</button>${brand()}</div><span class="deal-total small muted" title="По отметкам участников">Завершено сделок: ${state.completedDeals}</span><form class="search" id="search-form"><span aria-hidden="true">⌕</span><input name="q" placeholder="Поиск объявлений" aria-label="Поиск объявлений" maxlength="200"><button aria-label="Искать">↵</button></form><div class="row">${themeButton()}<a class="button" href="#/new" aria-label="Новое объявление">＋ <span class="new-label">Объявление</span></a><a href="#/settings" class="user-pill row"><div class="avatar">${h(state.me.nickname[0])}</div><span class="small">${h(state.me.nickname)}</span></a></div></header><div class="layout"><aside class="sidebar"><nav class="nav" aria-label="Главное меню">${nav.map(([href, icon, label]) => `<a href="${href}" class="${path === href ? "selected" : ""}"><span class="nav-icon" aria-hidden="true">${icon}</span>${label}</a>`).join("")}</nav><button class="button plain" data-action="logout">Выйти</button></aside><main class="main" id="view" tabindex="-1"><div class="boot" role="status">Загружаем…</div></main></div>`;
}
function mount(content, seq) {
  if (seq !== state.seq) return;
  document.querySelector("#view").innerHTML = content;
  tick();
}
function empty(title, text, button = "") {
  return `<div class="empty"><h2>${h(title)}</h2><p>${h(text)}</p>${button}</div>`;
}
function statusBadge(l) {
  const names = {
    active: "Опубликовано",
    pending: "На проверке",
    completed: "Завершено",
    archived: "Архив",
  };
  return `<span class="pill ${l.hidden ? "red" : l.status === "pending" ? "yellow" : l.status === "archived" ? "gray" : ""}">${l.hidden ? "Скрыто модератором" : names[l.status] || ""}</span>`;
}
function moderationNotice(l) {
  return l.moderation_comment
    ? `<div class="notice moderation-comment"><strong>Комментарий модератора</strong><p>${h(l.moderation_comment)}</p></div>`
    : "";
}

function photoURL(id) {
  return "/api/photos/" + encodeURIComponent(id);
}
function card(l, scope = "feed") {
  const own = l.author_id === state.me.id;
  return `<article class="listing-card" data-scope="${h(scope)}"><div class="card-media cat-${h(l.category)}"><a href="#/listing/${l.id}" class="${l.photos.length ? "" : "card-placeholder"}" style="${l.photos.length ? "width:100%;height:100%" : ""}" aria-label="${h(l.title || "Без названия")}">${l.photos.length ? `<img src="${photoURL(l.photos[0].id)}" loading="lazy" alt="${h(l.title || "Без названия")}">` : categoryIcons[l.category] || "◇"}</a>${own || scope === "moderation" ? "" : `<button class="icon-button ${l.favorite ? "active" : ""}" data-action="favorite" data-id="${l.id}" data-value="${l.favorite}" aria-label="${l.favorite ? "Убрать из избранного" : "В избранное"}">${l.favorite ? "♥" : "♡"}</button>`}<span class="pill">${l.kind === "offer" ? "Предложение" : "Заявка"}</span></div><div class="card-content"><span class="eyebrow muted" style="font-size:9px;letter-spacing:1px">${h(categoryNames[l.category])}</span><h3><a class="listing-card-link" href="#/listing/${l.id}">${h(l.title || "Без названия")}</a></h3><p class="card-description">${h(l.description)}</p><div class="row between wrap"><span class="price">${h(price(l))}</span>${statusBadge(l)}</div><div class="card-loc">⌖ ${h(locationNames[l.location] || l.location)}${l.dorm_name ? " · " + h(l.dorm_name) : ""}</div>${scope === "mine" && l.moderation_comment ? `<p class="small rejection-note">${h(l.moderation_comment)}</p>` : ""}<div class="card-footer"><a href="#/profile/${l.author_id}">${h(l.author_name)}</a><span>${h(ago(l.created_at))}</span></div></div></article>`;
}
function pager(page, params, base) {
  if (page.total <= page.limit) return "";
  const p = new URLSearchParams(params),
    prev = new URLSearchParams(p),
    next = new URLSearchParams(p);
  prev.set("offset", Math.max(0, page.offset - page.limit));
  next.set("offset", page.offset + page.limit);
  return `<nav class="pagination" aria-label="Страницы">${page.offset > 0 ? `<a class="button plain" href="#/${base}?${h(prev.toString())}">← Назад</a>` : ""}<span class="small muted">${page.offset + 1}–${Math.min(page.offset + page.limit, page.total)} из ${page.total}</span>${page.offset + page.limit < page.total ? `<a class="button plain" href="#/${base}?${h(next.toString())}">Далее →</a>` : ""}</nav>`;
}
async function feedView(page, params, seq) {
  const p = new URLSearchParams(params);
  if (page === "mine") {
    p.set("scope", "mine");
    if (!p.has("status")) p.set("status", "active");
  } else if (page === "favorites") p.set("scope", "favorites");
  else p.set("scope", "feed");
  const result = await api("/listings?" + p.toString());
  if (seq !== state.seq) return;
  let head = `<div class="page-head"><h1>${page === "feed" ? "Объявления" : page === "mine" ? "Мои объявления" : "Избранное"}</h1></div>`;
  const tabs =
    page === "mine"
      ? [
          ["active", "Опубликовано"],
          ["pending", "На проверке"],
          ["archived", "Архив"],
          ["completed", "Завершено"],
        ]
      : [
          ["", "Все объявления"],
          ["offer", "Предложения"],
          ["request", "Заявки"],
        ];
  const key = page === "mine" ? "status" : "kind";
  head += `<nav class="tabs">${tabs
    .map(([value, label]) => {
      const tab = new URLSearchParams(p);
      value ? tab.set(key, value) : tab.delete(key);
      tab.delete("offset");
      return `<a class="${(p.get(key) || "") === value ? "selected" : ""}" href="#/${page}?${h(tab.toString())}">${label}</a>`;
    })
    .join("")}</nav>`;
  head += `<form class="filters" id="filter-form" data-page="${page}"><select name="category" aria-label="Категория">${options(state.catalog.categories, p.get("category"), "Все категории")}</select><select name="location" aria-label="Район">${options(state.catalog.locations, p.get("location"), "Все районы")}</select><select name="dorm_id" aria-label="Общежитие" ${!p.get("location") ? "disabled" : ""}>${dormOptions(p.get("location"), p.get("dorm_id"), "Все общежития")}</select><input type="hidden" name="kind" value="${h(p.get("kind") || "")}"><input type="hidden" name="status" value="${h(p.get("status") || "")}"><input type="hidden" name="q" value="${h(p.get("q") || "")}"><button class="button plain" type="submit">Применить</button>${p.get("q") ? `<a class="button plain" href="#/${page}">Сбросить поиск</a>` : ""}</form>`;
  mount(
    head +
      `<div class="feed-meta">${p.get("q") ? "По запросу «" + h(p.get("q")) + "» · " : ""}${result.total} объявлений</div>` +
      (result.items.length
        ? `<div class="cards">${result.items.map((l) => card(l, page === "mine" ? "mine" : "feed")).join("")}</div>${pager(result, p, page)}`
        : empty(
            page === "favorites"
              ? "Пока ничего не сохранено"
              : "Нет объявлений",
            page === "favorites"
              ? "Нажми на сердечко у объявления, чтобы добавить его сюда."
              : "Попробуй другие фильтры или опубликуй первое объявление.",
            `<a class="button" href="#/${page === "favorites" ? "feed" : "new"}">${page === "favorites" ? "В общую ленту" : "Создать объявление"}</a>`,
          )),
    seq,
  );
  const search = document.querySelector("#search-form input");
  if (search) search.value = p.get("q") || "";
}
async function detailView(id, seq) {
  const l = await api("/listings/" + id);
  if (seq !== state.seq) return;
  state.detail = l;
  const own = l.author_id === state.me.id;
  const contacts = [
    l.telegram &&
      `<a class="button" target="_blank" rel="noopener noreferrer" href="${h(l.telegram)}">Написать в Telegram ↗</a>`,
    l.vk &&
      `<a class="button secondary" target="_blank" rel="noopener noreferrer" href="${h(l.vk)}">Написать в VK ↗</a>`,
  ]
    .filter(Boolean)
    .join("");
  mount(
    `<div class="breadcrumb"><a href="#/feed">← Лента</a> / ${h(categoryNames[l.category])}</div><div class="detail-grid"><section><div class="gallery" id="gallery">${l.photos.length ? `<img src="${photoURL(l.photos[0].id)}" alt="${h(l.title || "Без названия")}">` : categoryIcons[l.category] || "◇"}</div>${l.photos.length > 1 ? `<div class="thumbs">${l.photos.map((p, i) => `<button data-action="gallery" data-photo="${h(p.id)}" aria-label="Фотография ${i + 1}"><img src="${photoURL(p.id)}" alt=""></button>`).join("")}</div>` : ""}<div class="panel" style="margin-top:24px"><h2>Описание</h2><div class="description">${h(l.description)}</div></div></section><section><div class="row wrap"><span class="pill">${l.kind === "offer" ? "Предложение" : "Заявка"}</span><span class="pill gray">${h(categoryNames[l.category])}</span>${statusBadge(l)}</div><h1 class="detail-title">${h(l.title || "Без названия")}</h1><div class="detail-price price">${h(price(l))}</div><div class="meta-list"><div><span class="muted">Район</span><strong>${h(locationNames[l.location] || l.location)}</strong></div>${l.dorm_name ? `<div><span class="muted">Общежитие</span><strong>${h(l.dorm_name)}</strong></div>` : ""}<div><span class="muted">Опубликовано</span><span>${h(date(l.created_at))}</span></div></div>${own || state.me.is_admin ? moderationNotice(l) : ""}<div class="stack">${own ? `<a class="button" href="#/edit/${l.id}">Редактировать</a>${l.status === "active" ? `<button class="button secondary" data-action="complete" data-id="${l.id}">Завершить объявление</button>` : l.status === "archived" || l.status === "completed" ? `<button class="button secondary" data-action="transition" data-id="${l.id}" data-transition="submit" ${l.hidden ? "disabled" : ""}>Отправить на проверку</button>` : ""}${l.status !== "archived" ? `<button class="button plain" data-action="transition" data-id="${l.id}" data-transition="archive">Отложить в архив</button>` : ""}<button class="button plain danger-text" data-action="delete-listing" data-id="${l.id}">Удалить объявление</button>` : `${l.status === "active" || (state.me.is_admin && l.status === "pending") ? contacts : `<div class="notice">${l.status === "completed" ? "Автор завершил это объявление." : "Объявление доступно только автору и модератору."}</div>`}<button class="button plain" data-action="favorite" data-id="${l.id}" data-value="${l.favorite}">${l.favorite ? "♥ Убрать из избранного" : "♡ Сохранить в избранное"}</button>`}</div><div class="seller"><a class="row" href="#/profile/${l.author_id}"><div class="avatar">${h(l.author_name[0])}</div><div><strong>${h(l.author_name)}</strong><div class="small muted">Профиль №${l.author_id} ↗</div></div></a></div><div class="row wrap" style="margin-top:20px">${state.me.is_admin && l.status === "pending" ? `<button class="button" data-action="review-approve" data-id="${l.id}" data-revision="${l.revision}">Одобрить</button><button class="button secondary" data-action="review-reject" data-id="${l.id}" data-revision="${l.revision}">Отклонить</button>` : ""}${!own ? `<button class="small muted" data-action="report" data-kind="listing" data-id="${l.id}">Пожаловаться</button>` : ""}${state.me.is_admin ? `<button class="small muted" data-action="moderate" data-kind="listing" data-id="${l.id}" data-transition="${l.hidden ? "show" : "hide"}">${l.hidden ? "Показать" : "Скрыть"}</button><button class="small muted" data-action="moderate" data-kind="listing" data-id="${l.id}" data-transition="delete">Удалить</button>` : ""}</div></section></div>`,
    seq,
  );
}
async function profileView(id, params, seq) {
  const listingParams = new URLSearchParams(params);
  listingParams.set("author_id", id);
  listingParams.set("scope", state.me.is_admin ? "admin_author" : "feed");
  const [user, list] = await Promise.all([
    api("/users/" + id),
    api("/listings?" + listingParams.toString()),
  ]);
  if (seq !== state.seq) return;
  mount(
    `<section class="profile-head"><div class="avatar large">${h(user.nickname[0])}</div><div><h1>${h(user.nickname)}</h1><div class="profile-stats"><span>№${user.id}</span><span>С ${h(date(user.created_at))}</span><span>★ ${user.stars}</span></div></div><div class="row wrap" style="margin-left:auto">${user.id !== state.me.id && user.active !== false && !user.blocked ? `<button class="button ${user.starred ? "secondary" : ""}" data-action="star" data-id="${user.id}" data-value="${user.starred}">${user.starred ? "★ Отозвать звезду" : "☆ Поставить звезду"}</button><button class="button plain" data-action="report" data-kind="user" data-id="${user.id}">Пожаловаться</button>` : ""}</div></section>${state.me.is_admin && user.blocked ? '<div class="notice">Пользователь заблокирован. Публикации доступны для проверки модератором.</div>' : state.me.is_admin && user.active === false ? '<div class="notice">Аккаунт деактивирован. Публикации доступны для проверки модератором.</div>' : ""}<h2>Объявления пользователя</h2>${list.items.length ? `<div class="cards">${list.items.map((l) => card(l, state.me.is_admin ? "moderation" : "feed")).join("")}</div>${pager(list, listingParams, "profile/" + id)}` : empty("Нет доступных объявлений", "Активные публикации появятся здесь.")}`,
    seq,
  );
}
function settingsView(seq) {
  const u = state.me;
  mount(
    `<div class="page-head"><h1>Мой профиль</h1><p class="muted">Контакты, никнейм и настройки аккаунта.</p></div><div class="panel" style="max-width:800px"><div class="profile-head"><div class="avatar large">${h(u.nickname[0])}</div><div><h2>${h(u.nickname)}</h2><div class="profile-stats"><span>№${u.id}</span><span>★ ${u.stars}</span><span>С ${h(date(u.created_at))}</span></div></div></div><form id="settings-form" class="form-grid"><label class="field full"><span>Никнейм</span><input name="nickname" minlength="3" maxlength="30" value="${h(u.nickname)}" required ${u.nickname_changed ? "readonly" : ""}><span class="hint">${u.nickname_changed ? "Однократная смена никнейма уже использована." : "Никнейм можно изменить только один раз."}</span></label><label class="field"><span>Telegram</span><input name="telegram" type="text" maxlength="300" placeholder="@username или https://t.me/username" value="${h(u.telegram)}"></label><label class="field"><span>VK</span><input name="vk" type="text" maxlength="300" placeholder="username или https://vk.com/username" value="${h(u.vk)}"></label><div class="full small muted">Корпоративная почта: ${h(state.email)}. Другим пользователям она не видна. Контакты уже созданных объявлений сохраняются отдельно.</div><div class="full"><button class="button" type="submit">Сохранить изменения</button></div></form><section class="danger-zone"><h3>Удаление аккаунта</h3><p class="small muted">Аккаунт, объявления, фотографии, контакты и другие связанные данные будут удалены без возможности восстановления. Для подтверждения отправим отдельный код на твою почту.</p><button class="button danger" data-action="delete-account">Удалить аккаунт</button></section></div>`,
    seq,
  );
}
async function formView(id, seq) {
  releasePending();
  state.formListing = null;
  state.createAttempt = null;
  state.photoWork = Promise.resolve();
  let l = id ? await api("/listings/" + id) : null;
  if (seq !== state.seq) return;
  if (l && l.author_id !== state.me.id)
    throw new Error("Редактировать можно только свои объявления");
  const recovery = await editorStore.get(String(state.me.id)).catch(() => null);
  if (seq !== state.seq) return;
  let recover = recovery && (!id || String(recovery.listing_id) === String(id));
  if (recover && !l && recovery.listing_id) {
    try {
      l = await api("/listings/" + recovery.listing_id);
    } catch (e) {
      if (e.status !== 404) throw e;
      recover = false;
      await editorStore.remove(String(state.me.id));
    }
  }
  if (seq !== state.seq) return;
  if (recover && l && recovery.revision && l.revision !== recovery.revision) {
    recover = false;
    toast("Объявление изменилось. Загружена актуальная версия.", true);
  }
  state.formListing = l;
  const v = (recover
    ? {
        ...recovery.input,
        price_mode: recovery.raw_price_mode || recovery.input.price_mode,
      }
    : null) ||
    l || {
      kind: "offer",
      category: "",
      title: "",
      description: "",
      location: "",
      dorm_id: null,
      price_mode: "unset",
      telegram: state.me.telegram,
      vk: state.me.vk,
    };
  mount(
    `<div class="page-head"><h1>${l ? "Редактировать объявление" : "Новое объявление"}</h1><p id="autosave-status" class="small muted" role="status">Черновик сохраняется автоматически в архив.</p></div>${l ? moderationNotice(l) : ""}<form id="listing-form" class="panel" style="max-width:950px"><div class="form-section"><h2>Основное</h2></div><div class="form-grid"><label class="field"><span>Тип</span><select name="kind">${option("offer", "Предложение", v.kind) + option("request", "Заявка", v.kind)}</select></label><label class="field"><span>Категория</span><select name="category" required>${options(state.catalog.categories, v.category)}</select></label><label class="field full"><span>Название</span><input name="title" minlength="3" maxlength="120" value="${h(v.title)}" required placeholder="Например, настольная лампа для комнаты"></label><label class="field full"><span>Описание</span><textarea name="description" minlength="10" maxlength="10000" required placeholder="Состояние, подробности, условия передачи или помощи…">${h(v.description)}</textarea></label><label class="field"><span>Район</span><select name="location" required>${options(state.catalog.locations, v.location)}</select></label><label class="field"><span>Общежитие · необязательно</span><select name="dorm_id" ${!locationNames[v.location] || v.location === "UNSPECIFIED" ? "disabled" : ""}>${dormOptions(v.location, v.dorm_id)}</select></label><label class="field"><span>Стоимость</span><select name="price_mode">${option("unset", "Не указано", v.price_mode) + option("price", "Цена", v.price_mode) + option("exchange", "Обмен", v.price_mode)}</select></label><label class="field" id="price-field" ${v.price_mode !== "price" ? "hidden" : ""}><span>Цена, ₽</span><input name="price" type="text" inputmode="decimal" value="${recover && recovery.raw_price !== undefined ? h(recovery.raw_price) : v.price_minor != null ? h(v.price_minor / 100) : ""}" placeholder="0,00"><span class="hint">0 — бесплатно.</span></label></div><div class="form-section" style="margin-top:28px"><h2>Фотографии</h2><p>До 10 JPEG или PNG, до 5 МБ и 16 мегапикселей каждая. Первая — обложка.</p></div><div id="photo-list" class="photo-list"></div><label class="upload"><strong>＋ Добавить фотографии</strong><span class="small muted">Обрезку можно настроить для каждой фотографии.</span><input id="photo-input" type="file" accept="image/jpeg,image/png" multiple></label><div class="form-section" style="margin-top:28px"><h2>Контакты</h2><p>Укажи хотя бы один контакт для отправки на проверку. Контакт будет виден авторизованным участникам в объявлении. Профиль заполнять необязательно; изменения здесь относятся только к этому объявлению.</p></div><div class="form-grid"><label class="field"><span>Telegram</span><input type="text" name="telegram" maxlength="300" value="${h(v.telegram)}" placeholder="@username или https://t.me/username"></label><label class="field"><span>VK</span><input type="text" name="vk" maxlength="300" value="${h(v.vk)}" placeholder="username или https://vk.com/username"></label></div><div class="form-actions"><button class="button" type="submit">${l?.status === "completed" ? "Сохранить изменения" : "Отправить на проверку"}</button><button class="button secondary" type="submit" name="save_mode" value="draft" formnovalidate>Сохранить в архив</button><a class="button plain" href="#/mine?status=archived">Закрыть</a><span class="small muted">Для черновика контакт не требуется.</span></div></form>`,
    seq,
  );
  state.editor = {
    seq,
    form: document.querySelector("#listing-form"),
    dirty: 0,
    saved: 0,
    work: Promise.resolve(),
    storeWork: Promise.resolve(),
    user: state.me.id,
    closing: false,
  };
  if (recover) {
    state.createAttempt = recovery.attempt || null;
    state.pending = (recovery.pending || []).map((item) => ({
      file: item.file,
      url: URL.createObjectURL(item.file),
      uploadKey: item.uploadKey,
    }));
    if (recovery.input) {
      captureEditor();
      scheduleAutosave();
    }
  }
  if (!recover) {
    captureEditor(undefined, true);
    editorStatus("Черновик сохраняется автоматически в архив.");
  }
  renderPhotos();
}

function renderPhotos() {
  const target = document.querySelector("#photo-list");
  if (!target) return;
  const existing = state.formListing?.photos || [];
  target.innerHTML =
    existing
      .map(
        (p, i) =>
          `<div class="photo-item"><img src="${photoURL(p.id)}" alt="Фотография ${i + 1}">${i === 0 ? '<span class="pill">Обложка</span>' : ""}<div class="row">${i > 0 ? `<button type="button" data-action="cover" data-photo="${h(p.id)}">В обложку</button>` : ""}<button type="button" data-action="delete-photo" data-photo="${h(p.id)}">Удалить</button></div></div>`,
      )
      .join("") +
    state.pending
      .map(
        (p, i) =>
          `<div class="photo-item"><img src="${h(p.url)}" alt="Новая фотография ${i + 1}">${existing.length === 0 && i === 0 ? '<span class="pill">Обложка</span>' : ""}<div class="row"><button type="button" data-action="crop" data-index="${i}" ${p.uploadKey ? "disabled" : ""}>Обрезать</button><button type="button" data-action="remove-pending" data-index="${i}">Удалить</button></div></div>`,
      )
      .join("");
  const form = target.closest("form");
  if (form?.dataset.busy) lockControls(form);
}
function queuePhotos(files) {
  const selected = Array.from(files),
    seq = state.seq,
    pending = state.pending;
  state.photoWork = state.photoWork
    .then(() => addPhotos(selected, seq, pending))
    .catch((e) => toast(e.message, true));
}
async function addPhotos(files, seq, pending) {
  for (const file of files) {
    if (seq !== state.seq || pending !== state.pending) return;
    if ((state.formListing?.photos.length || 0) + pending.length >= 10) {
      toast("Можно добавить не больше 10 фотографий", true);
      break;
    }
    if (
      !["image/jpeg", "image/png"].includes(file.type) ||
      file.size > 5 * 1024 * 1024
    ) {
      toast("Допустимы JPEG и PNG до 5 МБ", true);
      continue;
    }
    const url = URL.createObjectURL(file);
    try {
      const image = await imageFrom(url);
      if (
        image.width * image.height > 16000000 ||
        image.width > 12000 ||
        image.height > 12000
      )
        throw new Error("Лимит изображения — 16 мегапикселей");
      const normalized = await normalizePhoto(image);
      if (seq !== state.seq || pending !== state.pending) return;
      pending.push({ file: normalized, url: URL.createObjectURL(normalized) });
    } catch (e) {
      if (seq === state.seq) toast(e.message, true);
    } finally {
      URL.revokeObjectURL(url);
    }
  }
  if (seq === state.seq) {
    renderPhotos();
    captureEditor();
    scheduleAutosave();
  }
}
function normalizePhoto(image) {
  const scale = Math.min(1, 1920 / Math.max(image.width, image.height)),
    canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.round(image.width * scale));
  canvas.height = Math.max(1, Math.round(image.height * scale));
  const context = canvas.getContext("2d");
  context.fillStyle = "#fff";
  context.fillRect(0, 0, canvas.width, canvas.height);
  context.drawImage(image, 0, 0, canvas.width, canvas.height);
  return new Promise((resolve, reject) =>
    canvas.toBlob(
      (blob) =>
        blob
          ? resolve(blob)
          : reject(new Error("Не удалось обработать фотографию")),
      "image/jpeg",
      0.95,
    ),
  );
}
function imageFrom(url) {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("Не удалось прочитать изображение"));
    image.src = url;
  });
}
async function crop(index) {
  const seq = state.seq,
    pending = state.pending,
    item = pending[index];
  if (!item) return;
  if (item.uploadKey) {
    toast(
      "Сначала повтори сохранение, чтобы подтвердить загрузку фотографии",
      true,
    );
    return;
  }
  const image = await imageFrom(item.url);
  if (seq !== state.seq || pending !== state.pending || !pending.includes(item))
    return;
  modal(
    "Обрезать фотографию",
    `<p class="small muted">Перетаскивай изображение и настрой масштаб.</p><canvas id="crop-canvas" class="crop-canvas" width="1200" height="900" aria-label="Перетаскиваемая область обрезки"></canvas><div class="crop-tools"><select id="crop-ratio" aria-label="Пропорции"><option value="1.333333">4:3</option><option value="1">1:1</option></select><input type="range" id="crop-zoom" min="1" max="3" step="0.01" value="1" aria-label="Масштаб"></div><button class="button" id="crop-save">Применить обрезку</button>`,
  );
  const canvas = document.querySelector("#crop-canvas"),
    context = canvas.getContext("2d");
  let zoom = 1,
    dx = 0,
    dy = 0,
    drag = null;
  function draw() {
    const scale =
      Math.max(canvas.width / image.width, canvas.height / image.height) * zoom;
    const width = image.width * scale,
      height = image.height * scale;
    dx = Math.max(
      -(width - canvas.width) / 2,
      Math.min((width - canvas.width) / 2, dx),
    );
    dy = Math.max(
      -(height - canvas.height) / 2,
      Math.min((height - canvas.height) / 2, dy),
    );
    context.clearRect(0, 0, canvas.width, canvas.height);
    context.drawImage(
      image,
      (canvas.width - width) / 2 + dx,
      (canvas.height - height) / 2 + dy,
      width,
      height,
    );
  }
  canvas.addEventListener("pointerdown", (e) => {
    canvas.setPointerCapture(e.pointerId);
    drag = { x: e.clientX, y: e.clientY };
  });
  canvas.addEventListener("pointerup", () => {
    drag = null;
  });
  canvas.addEventListener("pointercancel", () => {
    drag = null;
  });
  canvas.addEventListener("pointermove", (e) => {
    if (!drag) return;
    const rect = canvas.getBoundingClientRect();
    dx += ((e.clientX - drag.x) * canvas.width) / rect.width;
    dy += ((e.clientY - drag.y) * canvas.height) / rect.height;
    drag = { x: e.clientX, y: e.clientY };
    draw();
  });
  document.querySelector("#crop-zoom").addEventListener("input", (e) => {
    zoom = Number(e.target.value);
    draw();
  });
  document.querySelector("#crop-ratio").addEventListener("change", (e) => {
    canvas.height = Math.round(canvas.width / Number(e.target.value));
    dx = dy = 0;
    draw();
  });
  document.querySelector("#crop-save").addEventListener("click", () => {
    canvas.toBlob(
      (blob) => {
        if (
          seq !== state.seq ||
          pending !== state.pending ||
          !pending.includes(item) ||
          !canvas.isConnected
        )
          return;
        if (!blob) {
          toast("Не удалось обрезать фотографию", true);
          return;
        }
        URL.revokeObjectURL(item.url);
        item.file = blob;
        item.url = URL.createObjectURL(blob);
        closeDialog();
        renderPhotos();
        captureEditor();
        scheduleAutosave();
      },
      "image/jpeg",
      0.9,
    );
  });
  draw();
}
async function adminView(params, seq) {
  const tab = params.get("tab") || "listings",
    offset = Number(params.get("offset") || 0);
  let content = "";
  const tabs = `<nav class="tabs">${[
    ["listings", "Объявления"],
    ["reports", "Жалобы"],
    ["users", "Пользователи"],
    ["dorms", "Общежития"],
  ]
    .map(
      ([v, n]) =>
        `<a href="#/admin?tab=${v}" class="${tab === v ? "selected" : ""}">${n}</a>`,
    )
    .join("")}</nav>`;
  if (tab === "listings") {
    const page = await api("/admin/listings?offset=" + offset);
    content = `<p class="muted">Открой объявление, проверь текст, фотографии и контакты. Одобрение публикует текущую версию; отказ возвращает её в архив автора.</p>${page.items.length ? `<div class="cards">${page.items.map((l) => card(l, "moderation")).join("")}</div>${pager(page, params, "admin")}` : empty("Очередь пуста", "Новые объявления появятся здесь после отправки на проверку.")}`;
  } else if (tab === "reports") {
    const status = params.get("status") || "new",
      items = await api(
        "/admin/reports?status=" +
          encodeURIComponent(status) +
          "&offset=" +
          offset,
      );
    content = `<form id="admin-filter" class="filters"><select name="status">${option("new", "Новые", status) + option("resolved", "Решённые", status) + option("dismissed", "Отклонённые", status)}</select><button class="button plain">Показать</button></form><div class="stack">${items.map((r) => `<article class="report-card"><div class="row between wrap"><strong>Жалоба №${r.id}</strong><span class="small muted">${h(date(r.created_at))}</span></div><div class="row wrap small" style="margin-top:10px"><a href="#/${r.target_type === "listing" ? "listing" : "profile"}/${r.target_id}">${r.target_type === "listing" ? "Объявление" : "Пользователь"} №${r.target_id} ↗</a><span class="pill gray">${h({ new: "Новая", resolved: "Решена", dismissed: "Отклонена" }[r.status] || r.status)}</span></div><div class="description">${h(r.reason)}</div>${r.resolution ? `<p class="small muted">Решение: ${h(r.resolution)}</p>` : ""}${r.status === "new" ? `<div class="row"><button class="button secondary" data-action="resolve" data-id="${r.id}" data-transition="resolved">Отметить решённой</button><button class="button plain" data-action="resolve" data-id="${r.id}" data-transition="dismissed">Отклонить</button></div>` : ""}</article>`).join("") || empty("Нет жалоб", "Новые обращения появятся здесь.")}</div>${adminPager(items.length, offset, params)}`;
  } else if (tab === "users") {
    const items = await api("/admin/users?offset=" + offset);
    content = `<div class="panel table-wrap"><table><thead><tr><th>Пользователь</th><th>Состояние</th><th>Регистрация</th><th>Действия</th></tr></thead><tbody>${items.map((u) => `<tr><td><strong>${h(u.nickname)}</strong><div class="small muted">№${u.id}${u.is_admin ? " · администратор" : ""}</div></td><td><span class="pill ${u.blocked ? "red" : u.active ? "" : "gray"}">${u.blocked ? "Заблокирован" : u.active ? "Активен" : "Деактивирован"}</span></td><td>${h(date(u.created_at))}</td><td><div class="account-actions">${!u.is_admin && u.id !== state.me.id ? `<button data-action="moderate" data-kind="user" data-id="${u.id}" data-transition="${u.blocked ? "unblock" : "block"}">${u.blocked ? "Разблокировать" : "Заблокировать"}</button><button data-action="moderate" data-kind="user" data-id="${u.id}" data-transition="delete">Удалить</button>` : "—"}</div></td></tr>`).join("")}</tbody></table></div>${adminPager(items.length, offset, params)}`;
  } else if (tab === "dorms") {
    content = `<div class="panel"><h2>Справочник общежитий</h2><form id="dorm-form" class="form-grid"><label class="field"><span>Район</span><select name="location" required>${options(state.catalog.locations, "")}</select></label><label class="field"><span>Название</span><input name="name" maxlength="100" required placeholder="Название или номер общежития"></label><div class="full"><button class="button" type="submit">Добавить общежитие</button></div></form><div class="table-wrap" style="margin-top:25px"><table><tbody>${state.catalog.dorms.map((d) => `<tr><td>${h(locationNames[d.location] || d.location)}</td><td>${h(d.name)}</td><td><button class="small muted" data-action="delete-dorm" data-id="${d.id}">Удалить</button></td></tr>`).join("") || '<tr><td class="muted">Справочник пока пуст. Добавьте утверждённые общежития.</td></tr>'}</tbody></table></div></div>`;
  } else throw new Error("Раздел не найден");
  mount(
    `<div class="page-head"><h1>Модерация</h1></div>${tabs}${content}`,
    seq,
  );
}
function adminPager(count, offset, params) {
  const p = new URLSearchParams(params),
    n = new URLSearchParams(params);
  p.set("offset", Math.max(0, offset - 50));
  n.set("offset", offset + 50);
  return `<div class="pagination">${offset ? `<a class="button plain" href="#/admin?${h(p.toString())}">← Назад</a>` : ""}${count === 50 ? `<a class="button plain" href="#/admin?${h(n.toString())}">Далее →</a>` : ""}</div>`;
}
async function route() {
  const generation = ++routeGeneration;
  try {
    await leaveEditor();
  } catch (e) {
    state.editor = null;
    toast(
      "Не удалось сохранить на сервере: " +
        e.message +
        ". Открой создание объявления, чтобы восстановить копию из браузера.",
      true,
    );
  }
  if (generation !== routeGeneration) return;
  if (!state.me) return;
  const seq = ++state.seq;
  releasePending();
  state.formListing = null;
  state.createAttempt = null;
  closeDialog();
  const [path, query = ""] = (location.hash || "#/feed")
      .replace(/^#\//, "")
      .split("?"),
    parts = path.split("/"),
    params = new URLSearchParams(query);
  shell();
  document.title =
    "Dorm Hub — " +
    ({
      feed: "Лента",
      mine: "Мои объявления",
      favorites: "Избранное",
      settings: "Мой профиль",
      new: "Новое объявление",
      admin: "Модерация",
    }[parts[0]] || "Объявления");
  try {
    if (["feed", "mine", "favorites"].includes(parts[0]))
      await feedView(parts[0], params, seq);
    else if (parts[0] === "listing") await detailView(parts[1], seq);
    else if (parts[0] === "profile") await profileView(parts[1], params, seq);
    else if (parts[0] === "settings") settingsView(seq);
    else if (parts[0] === "new") await formView(null, seq);
    else if (parts[0] === "edit") await formView(parts[1], seq);
    else if (parts[0] === "admin") {
      if (!state.me.is_admin) throw new Error("Недостаточно прав");
      await adminView(params, seq);
    } else
      mount(
        empty(
          "Страница не найдена",
          "Перейди в общую ленту.",
          '<a class="button" href="#/feed">В ленту</a>',
        ),
        seq,
      );
  } catch (e) {
    if (seq !== state.seq) return;
    if (e.status === 401) {
      await guest();
      toast("Сессия завершена. Войди снова.", true);
    } else
      mount(
        `<div class="panel error-panel"><h2>Не удалось открыть страницу</h2><p class="muted">${h(e.message)}</p><a class="button" href="#/feed">В ленту</a><button class="button plain" data-action="retry">Повторить</button></div>`,
        seq,
      );
  }
}
function loginView(code = false) {
  app.innerHTML = `<main class="login"><header class="login-header row between">${brand()}${themeButton()}</header><section class="login-side"><div class="login-box panel"><h1>${code ? "Код из письма" : "Вход"}</h1><p class="muted">${code ? "Отправили код на " + h(state.email) + "." : "Используй студенческую почту или разрешённый адрес. Пароль не нужен."}</p><form id="${code ? "code-form" : "login-form"}">${code ? `<label class="field"><span>Код из письма</span><input class="code-input" name="code" maxlength="6" minlength="6" inputmode="numeric" pattern="[0-9]{6}" autocomplete="one-time-code" required autofocus></label>` : `<label class="field"><span>Почта</span><input name="email" type="email" maxlength="254" autocomplete="email" value="${h(state.email)}" placeholder="stXXXXXX@student.spbu.ru" required autofocus></label>`}<button class="button" type="submit">${code ? "Войти" : "Получить код"}</button></form>${code ? `<div class="row between" style="margin-top:18px"><button class="small muted" data-action="login-back">Изменить почту</button><button class="small muted" data-action="resend" id="resend-button">Отправить ещё раз</button></div>` : ""}<div class="login-note">Доступ к объявлениям и профилям открыт только авторизованным участникам. Твою почту другие студенты не увидят.</div></div></section></main>`;
  tick();
}
async function guest() {
  clearTimeout(autosaveTimer);
  state.editor = null;
  releasePending();
  state.me = null;
  state.csrf = "";
  state.catalog = null;
  ++state.seq;
  const data = await api("/auth/bootstrap");
  state.guestCSRF = data.csrf;
  loginView();
}
async function initialize() {
  try {
    const data = await api("/me");
    state.me = data.user;
    state.email = data.email;
    state.csrf = data.csrf;
    state.catalog = await api("/catalog");
    await refreshStatistics();
    await route();
  } catch (e) {
    if (e.status === 401) {
      try {
        await guest();
      } catch (error) {
        bootError(error);
      }
    } else bootError(e);
  }
}
function bootError(error) {
  app.innerHTML = `<div class="panel error-panel"><h1>Dorm Hub</h1><p>Не удалось подключиться к площадке.</p><p class="muted">${h(error.message)}</p><button class="button" data-action="initialize">Повторить</button></div>`;
}
function lockControls(form) {
  for (const control of form.querySelectorAll("input,select,textarea,button")) {
    if (!control.disabled) {
      control.dataset.busyDisabled = "1";
      control.disabled = true;
    }
  }
}
async function withBusy(form, fn) {
  if (form.dataset.busy) return;
  form.dataset.busy = "1";
  lockControls(form);
  try {
    await fn();
  } catch (e) {
    toast(e.message, true);
  } finally {
    delete form.dataset.busy;
    for (const control of form.querySelectorAll("[data-busy-disabled]")) {
      delete control.dataset.busyDisabled;
      control.disabled = false;
    }
  }
}

async function saveListing(form, data, mode = "submit") {
  const seq = state.seq,
    previous = state.formListing,
    pending = state.pending;
  let amount = null;
  if (data.get("price_mode") === "price" && data.get("price")) {
    try {
      amount = minor(data.get("price"));
    } catch (e) {
      if (mode === "submit") throw e;
    }
  }
  if (
    mode === "submit" &&
    data.get("price_mode") === "price" &&
    amount === null
  )
    throw new Error("Укажи цену или выбери другой вариант стоимости");
  const input = {
    draft: true,
    revision: previous?.revision || 0,
    has_photos: pending.length > 0,
    kind: data.get("kind"),
    category: data.get("category"),
    title: data.get("title"),
    description: data.get("description"),
    location: data.get("location"),
    dorm_id: data.get("dorm_id") ? Number(data.get("dorm_id")) : null,
    price_mode: data.get("price_mode"),
    price_minor: amount,
    telegram: data.get("telegram"),
    vk: data.get("vk"),
  };
  if (input.price_mode === "price" && input.price_minor === null)
    input.price_mode = "unset";
  const freshAttempt = !previous && !state.createAttempt;
  const attempt = previous
    ? null
    : state.createAttempt || { key: requestKey(), input };
  if (!previous && seq === state.seq) {
    state.createAttempt = attempt;
    if (state.editor) await storeEditor(state.editor);
  }
  await state.photoWork;
  if (freshAttempt || previous) input.has_photos = pending.length > 0;
  if (freshAttempt && seq === state.seq && state.editor)
    await storeEditor(state.editor);
  let saved;
  if (previous) {
    saved = await api("/listings/" + previous.id, {
      method: "PUT",
      body: input,
    });
  } else {
    saved = await api("/listings", {
      method: "POST",
      body: attempt.input,
      idempotencyKey: attempt.key,
    });
    if (seq === state.seq) {
      state.formListing = saved;
      state.createAttempt = null;
    }
    // Recover a possibly committed creation before applying edits made after a
    // failed response. Changing the form must not create another listing.
    if (JSON.stringify(input) !== JSON.stringify(attempt.input)) {
      input.revision = saved.revision || 0;
      saved = await api("/listings/" + saved.id, {
        method: "PUT",
        body: input,
      });
    }
  }
  if (seq === state.seq) {
    state.formListing = saved;
    if (state.editor) await storeEditor(state.editor);
  }
  while (pending.length) {
    const item = pending[0],
      body = new FormData();
    item.uploadKey ||= requestKey();
    if (seq === state.seq && state.editor) await storeEditor(state.editor);
    body.append("file", item.file, "photo.jpg");
    try {
      const photo = await api("/listings/" + saved.id + "/photos", {
        method: "POST",
        body,
        idempotencyKey: item.uploadKey,
      });
      if (!saved.photos.some((existing) => existing.id === photo.id))
        saved.photos.push(photo);
      pending.shift();
      URL.revokeObjectURL(item.url);
      if (saved.revision) saved.revision++;
      if (seq === state.seq && state.editor) await storeEditor(state.editor);
    } catch (e) {
      // These explicit errors occur before creation; the user can edit the file.
      if ([400, 403, 409, 429].includes(e.status)) delete item.uploadKey;
      if (seq === state.seq) {
        try {
          const latest = await api("/listings/" + saved.id);
          const fields = [
            "kind",
            "category",
            "title",
            "description",
            "location",
            "dorm_id",
            "price_mode",
            "price_minor",
            "telegram",
            "vk",
          ];
          if (
            fields.every(
              (key) =>
                JSON.stringify(latest[key]) === JSON.stringify(saved[key]),
            )
          )
            state.formListing = latest;
        } catch {}
        if (state.editor) await storeEditor(state.editor);
        renderPhotos();
      }
      const error = new Error(
        "Объявление №" +
          saved.id +
          " сохранено. " +
          e.message +
          ". " +
          (seq === state.seq
            ? "Повтори сохранение для оставшихся фотографий."
            : "Открой его в «Моих объявлениях», чтобы добавить оставшиеся фотографии."),
      );
      error.status = e.status;
      throw error;
    }
  }
  if (mode === "submit" && previous?.status !== "completed") {
    saved = await api("/listings/" + saved.id + "/status", {
      method: "POST",
      body: { action: "submit" },
    });
  } else if (saved.revision) {
    saved = await api("/listings/" + saved.id);
  }
  if (seq === state.seq) state.formListing = saved;
  if (mode !== "autosave" && seq === state.seq) {
    if (state.editor) {
      state.editor.saved = state.editor.dirty;
      state.editor.closing = true;
      await state.editor.storeWork;
      await editorStore.remove(String(state.editor.user));
    }
    toast(
      mode === "submit" && previous?.status !== "completed"
        ? "Отправлено на проверку"
        : saved.status === "completed"
          ? "Изменения сохранены"
          : "Сохранено в архив",
    );
    location.hash = "/listing/" + saved.id;
  }
  return saved;
}
function collectFilter(form) {
  const p = new URLSearchParams();
  for (const [k, v] of new FormData(form)) {
    if (v) p.set(k, v);
  }
  return p;
}
document.addEventListener("submit", async (event) => {
  const form = event.target;
  if (!(form instanceof HTMLFormElement)) return;
  event.preventDefault();
  const data = new FormData(form);
  if (form.id === "search-form") {
    location.hash = "/feed?" + new URLSearchParams({ q: data.get("q").trim() });
    return;
  }
  if (form.id === "filter-form") {
    location.hash = "/" + form.dataset.page + "?" + collectFilter(form);
    return;
  }
  if (form.id === "admin-filter") {
    location.hash = "/admin?tab=reports&" + collectFilter(form);
    return;
  }
  await withBusy(form, async () => {
    if (form.id === "login-form") {
      state.email = data.get("email").trim();
      await api("/auth/request", {
        method: "POST",
        body: { email: state.email },
        guest: true,
      });
      state.resendAt = Date.now() + 60000;
      loginView(true);
    } else if (form.id === "code-form") {
      const result = await api("/auth/verify", {
        method: "POST",
        body: { email: state.email, code: data.get("code"), reactivate: false },
        guest: true,
      });
      state.me = result.user;
      state.email = result.email;
      state.csrf = result.csrf;
      state.catalog = await api("/catalog");
      await refreshStatistics();
      if (!location.hash || location.hash === "#/") location.hash = "/feed";
      await route();
    } else if (form.id === "settings-form") {
      state.me = await api("/me", {
        method: "PUT",
        body: {
          nickname: data.get("nickname"),
          telegram: data.get("telegram"),
          vk: data.get("vk"),
        },
      });
      toast("Профиль сохранён");
      await route();
    } else if (form.id === "listing-form") {
      clearTimeout(autosaveTimer);
      const editor = state.editor;
      if (editor) await editor.work.catch(() => {});
      const mode = event.submitter?.value === "draft" ? "draft" : "submit";
      if (editor) {
        editor.work = saveListing(form, data, mode);
        await editor.work;
      } else await saveListing(form, data, mode);
    } else if (form.id === "report-form") {
      await api("/reports", {
        method: "POST",
        body: {
          target_type: form.dataset.kind,
          target_id: Number(form.dataset.id),
          reason: data.get("reason"),
        },
      });
      closeDialog();
      toast("Жалоба отправлена администратору");
    } else if (form.id === "resolve-form") {
      await api("/admin/reports/" + form.dataset.id, {
        method: "PUT",
        body: { status: form.dataset.status, resolution: data.get("reason") },
      });
      closeDialog();
      toast("Решение сохранено");
      await route();
    } else if (form.id === "moderate-form") {
      if (
        form.dataset.operation === "delete" &&
        data.get("confirm") !== form.dataset.id
      )
        throw new Error("Введи ID для подтверждения удаления");
      await api("/admin/actions", {
        method: "POST",
        body: {
          target_type: form.dataset.kind,
          target_id: Number(form.dataset.id),
          action: form.dataset.operation,
          reason: data.get("reason"),
        },
      });
      closeDialog();
      toast("Действие выполнено");
      if (form.dataset.kind === "listing") location.hash = "/feed";
      else await route();
    } else if (form.id === "delete-account-form") {
      const user = state.me.id;
      const result = await api("/me", {
        method: "DELETE",
        body: { code: data.get("code") },
      });
      await editorStore.remove(String(user));
      closeDialog();
      state.email = "";
      await guest();
      toast(
        result.files_pending
          ? "Аккаунт удалён. Очистка файлов завершится автоматически."
          : "Аккаунт и связанные данные удалены",
      );
    } else if (form.id === "review-form") {
      await api("/admin/listings/" + form.dataset.id + "/review", {
        method: "POST",
        body: {
          action: form.dataset.decision,
          revision: Number(form.dataset.revision),
          comment: data.get("comment") || "",
        },
      });
      closeDialog();
      toast(
        form.dataset.decision === "approve"
          ? "Объявление опубликовано"
          : "Возвращено в архив с комментарием",
      );
      location.hash = "/admin?tab=listings";
      await route();
    } else if (form.id === "complete-form") {
      await api("/listings/" + form.dataset.id + "/status", {
        method: "POST",
        body: { action: "complete", outcome: data.get("outcome") },
      });
      closeDialog();
      await refreshStatistics();
      await route();
    } else if (form.id === "delete-listing-form") {
      await api("/listings/" + form.dataset.id, { method: "DELETE" });
      closeDialog();
      location.hash = "/mine?status=archived";
    } else if (form.id === "dorm-form") {
      await api("/admin/dorms", {
        method: "POST",
        body: { location: data.get("location"), name: data.get("name") },
      });
      state.catalog = await api("/catalog");
      toast("Общежитие добавлено");
      await route();
    } else if (form.id === "delete-dorm-form") {
      await api("/admin/dorms/" + form.dataset.id, { method: "DELETE" });
      state.catalog = await api("/catalog");
      closeDialog();
      await route();
    }
  });
});
document.addEventListener("change", (event) => {
  const el = event.target;
  if (el.closest("form")?.dataset.busy) return;
  if (el.id === "photo-input") {
    queuePhotos(el.files);
    el.value = "";
  } else if (el.name === "location") {
    const form = el.closest("form"),
      dorm = form?.querySelector('[name="dorm_id"]');
    if (dorm) {
      dorm.innerHTML = dormOptions(
        el.value,
        "",
        form.id === "filter-form" ? "Все общежития" : "Без общежития",
      );
      dorm.disabled = !el.value;
    }
  } else if (el.name === "price_mode") {
    const field = document.querySelector("#price-field");
    if (field) field.hidden = el.value !== "price";
  }
});
document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-action]");
  if (!button) return;
  event.preventDefault();
  if (button.disabled || button.closest("form")?.dataset.busy) return;
  const { action, id, kind, transition, value, photo, index } = button.dataset;
  try {
    if (action === "menu") {
      const side = document.querySelector(".sidebar");
      side.classList.toggle("open");
      button.setAttribute(
        "aria-expanded",
        String(side.classList.contains("open")),
      );
    } else if (action === "close-dialog") closeDialog();
    else if (action === "initialize") await initialize();
    else if (action === "retry") await route();
    else if (action === "logout") {
      await leaveEditor();
      await api("/auth/logout", { method: "POST" });
      await guest();
    } else if (action === "login-back") loginView(false);
    else if (action === "resend") {
      if (Date.now() < state.resendAt) return;
      button.disabled = true;
      try {
        await api("/auth/request", {
          method: "POST",
          body: { email: state.email },
          guest: true,
        });
        state.resendAt = Date.now() + 60000;
        toast("Новый код отправлен");
      } finally {
        tick();
      }
    } else if (action === "favorite") {
      button.disabled = true;
      await api("/listings/" + id + "/favorite", {
        method: value === "true" ? "DELETE" : "PUT",
      });
      await route();
    } else if (action === "star") {
      button.disabled = true;
      await api("/users/" + id + "/star", {
        method: value === "true" ? "DELETE" : "PUT",
      });
      await route();
    } else if (action === "transition") {
      button.disabled = true;
      await api("/listings/" + id + "/status", {
        method: "POST",
        body: { action: transition },
      });
      toast("Статус обновлён");
      await route();
    } else if (action === "gallery") {
      const image = document.querySelector("#gallery img");
      if (image) image.src = photoURL(photo);
    } else if (action === "report")
      modal(
        "Пожаловаться",
        `<p class="small muted">Опиши нарушение. Администратор рассмотрит обращение вручную.</p><form id="report-form" data-kind="${h(kind)}" data-id="${h(id)}"><label class="field"><span>Причина</span><textarea name="reason" minlength="10" maxlength="2000" required></textarea></label><button class="button" type="submit" style="margin-top:18px">Отправить жалобу</button></form>`,
      );
    else if (action === "resolve")
      modal(
        "Решение по жалобе",
        `<form id="resolve-form" data-id="${h(id)}" data-status="${h(transition)}"><label class="field"><span>Описание решения</span><textarea name="reason" minlength="10" maxlength="2000" required></textarea></label><button class="button" type="submit" style="margin-top:18px">Сохранить решение</button></form>`,
      );
    else if (action === "moderate") {
      const names = {
        hide: "Скрыть объявление",
        show: "Показать объявление",
        block: "Заблокировать пользователя",
        unblock: "Разблокировать пользователя",
        delete: "Удалить",
      };
      modal(
        names[transition],
        `<p class="small muted">${kind === "listing" ? "Объявление" : "Пользователь"} №${h(id)}.${transition === "delete" && kind === "user" ? " Аккаунт и связанные публикации будут удалены безвозвратно." : ""}</p><form id="moderate-form" data-id="${h(id)}" data-kind="${h(kind)}" data-operation="${h(transition)}"><label class="field"><span>Причина действия</span><textarea name="reason" minlength="10" maxlength="2000" required></textarea></label>${transition === "delete" ? `<label class="field" style="margin-top:15px"><span>Введи ID ${h(id)} для подтверждения</span><input name="confirm" required inputmode="numeric"></label>` : ""}<button class="button ${transition === "delete" ? "danger" : ""}" type="submit" style="margin-top:18px">Подтвердить</button></form>`,
      );
    } else if (action === "delete-account") {
      await api("/me/deletion-code", { method: "POST" });
      state.deletionResendAt = Date.now() + 60000;
      modal(
        "Удалить аккаунт навсегда?",
        `<p class="muted">Код отправлен на ${h(state.email)}. Будут удалены профиль, объявления, фото, контакты и связанные записи. Восстановление невозможно.</p><form id="delete-account-form"><label class="field"><span>Код удаления из письма</span><input name="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" minlength="6" maxlength="6" required autofocus></label><div class="form-actions"><button class="button danger" type="submit">Удалить навсегда</button><button class="button plain" type="button" data-action="close-dialog">Отмена</button><button class="button plain" id="deletion-resend" type="button" data-action="deletion-resend">Отправить код повторно</button></div></form>`,
      );
      tick();
    } else if (action === "deletion-resend") {
      await api("/me/deletion-code", { method: "POST" });
      state.deletionResendAt = Date.now() + 60000;
      tick();
      toast("Новый код удаления отправлен");
    } else if (action === "review-approve" || action === "review-reject") {
      const decision = action === "review-approve" ? "approve" : "reject";
      modal(
        decision === "approve" ? "Одобрить объявление?" : "Вернуть в архив",
        `<form id="review-form" data-id="${h(id)}" data-revision="${h(button.dataset.revision)}" data-decision="${decision}">${decision === "reject" ? '<label class="field"><span>Комментарий автору</span><textarea name="comment" minlength="3" maxlength="2000" required placeholder="Что нужно исправить перед публикацией"></textarea></label>' : '<p class="muted">Проверенная версия станет доступна участникам.</p>'}<button class="button" type="submit">${decision === "approve" ? "Опубликовать" : "Вернуть с комментарием"}</button></form>`,
      );
    } else if (action === "complete")
      modal(
        "Завершить объявление",
        `<form id="complete-form" data-id="${h(id)}"><label class="field"><span>Причина завершения</span><select name="outcome" required><option value="success">Сделка состоялась / нужное найдено</option><option value="no_longer_needed">Передумал / больше не актуально</option></select></label><p class="small muted">Первый вариант учитывается в общем счётчике один раз на объявление.</p><button class="button" type="submit">Завершить</button></form>`,
      );
    else if (action === "delete-listing")
      modal(
        "Удалить объявление?",
        `<p class="muted">Текст и фотографии будут удалены без возможности восстановления.</p><form id="delete-listing-form" data-id="${h(id)}"><button class="button danger" type="submit">Удалить</button><button class="button plain" type="button" data-action="close-dialog">Отмена</button></form>`,
      );
    else if (action === "delete-dorm")
      modal(
        "Удалить общежитие?",
        `<p class="muted">В объявлениях сохранится местоположение, а поле общежития очистится.</p><form id="delete-dorm-form" data-id="${h(id)}"><button class="button danger" type="submit">Удалить из справочника</button></form>`,
      );
    else if (action === "remove-pending") {
      const [p] = state.pending.splice(Number(index), 1);
      if (p) URL.revokeObjectURL(p.url);
      renderPhotos();
      captureEditor();
      scheduleAutosave();
    } else if (action === "crop") await crop(Number(index));
    else if (action === "delete-photo" || action === "cover") {
      const listing = state.formListing,
        seq = state.seq,
        form = button.closest("form");
      await withBusy(form, async () => {
        if (action === "delete-photo") {
          await api(
            "/listings/" + listing.id + "/photos/" + encodeURIComponent(photo),
            { method: "DELETE" },
          );
          listing.photos = listing.photos.filter((p) => p.id !== photo);
        } else {
          const ids = listing.photos.map((p) => p.id),
            i = ids.indexOf(photo);
          if (i < 0) return;
          ids.splice(i, 1);
          ids.unshift(photo);
          await api("/listings/" + listing.id + "/photos", {
            method: "PUT",
            body: { ids },
          });
          listing.photos.sort((a, b) => ids.indexOf(a.id) - ids.indexOf(b.id));
        }
        if (seq === state.seq) renderPhotos();
      });
    }
  } catch (e) {
    toast(e.message, true);
  } finally {
    if (button.isConnected) button.disabled = false;
  }
});
function tick() {
  const deletion = document.querySelector("#deletion-resend");
  if (deletion) {
    const seconds = Math.ceil((state.deletionResendAt - Date.now()) / 1000);
    deletion.disabled = seconds > 0;
    deletion.textContent =
      seconds > 0
        ? "Повторить через " + seconds + " с"
        : "Отправить код повторно";
  }
  const resend = document.querySelector("#resend-button");
  if (resend) {
    const seconds = Math.ceil((state.resendAt - Date.now()) / 1000);
    resend.disabled = seconds > 0;
    resend.textContent =
      seconds > 0 ? "Повторить через " + seconds + " с" : "Отправить ещё раз";
  }
}
document.addEventListener("input", (event) => {
  if (event.target.closest("#listing-form")) {
    captureEditor();
    scheduleAutosave();
  }
});
document.addEventListener("change", (event) => {
  if (event.target.closest("#listing-form")) {
    captureEditor();
    scheduleAutosave();
  }
});
window.addEventListener("beforeunload", (event) => {
  if (state.editor && state.editor.dirty > state.editor.saved) {
    event.preventDefault();
    event.returnValue = "";
  }
});
setInterval(tick, 1000);
window.addEventListener("hashchange", route);
dialog.addEventListener("click", (event) => {
  if (event.target === dialog) {
    const r = dialog.getBoundingClientRect();
    if (
      event.clientX < r.left ||
      event.clientX > r.right ||
      event.clientY < r.top ||
      event.clientY > r.bottom
    )
      closeDialog();
  }
});
initialize();

async function refreshStatistics() {
  try {
    state.completedDeals = (await api("/statistics")).completed_deals;
  } catch {
    state.completedDeals = 0;
  }
}
function editorStatus(message, error = false) {
  const el = document.querySelector("#autosave-status");
  if (el) {
    el.textContent = message;
    el.classList.toggle("error-text", error);
  }
}
function captureEditor(form = state.editor?.form, baseline = false) {
  const editor = state.editor;
  if (!editor || editor.closing || !form) return;
  const data = new FormData(form),
    raw = Object.fromEntries(data);
  let value = null;
  try {
    value = raw.price_mode === "price" && raw.price ? minor(raw.price) : null;
  } catch {}
  const input = {
    draft: true,
    kind: raw.kind || "offer",
    category: raw.category || "",
    title: raw.title || "",
    description: raw.description || "",
    location: raw.location || "",
    dorm_id: raw.dorm_id ? Number(raw.dorm_id) : null,
    price_mode:
      raw.price_mode === "price" && value === null
        ? "unset"
        : raw.price_mode || "unset",
    price_minor: value,
    telegram: raw.telegram || "",
    vk: raw.vk || "",
  };
  const fingerprint = JSON.stringify({
    input,
    raw_price: raw.price || "",
    photos: state.formListing?.photos || [],
    pending: state.pending.map((p) => ({ url: p.url, size: p.file.size })),
  });
  if (fingerprint === editor.fingerprint) return;
  editor.fingerprint = fingerprint;
  editor.dirty++;
  editor.input = input;
  editor.rawPrice = raw.price || "";
  editor.rawPriceMode = raw.price_mode;
  if (baseline) {
    editor.saved = editor.dirty;
    return;
  }
  storeEditor(editor);
  editorStatus("Есть изменения — сохраняем черновик…");
}
function storeEditor(editor) {
  if (editor.closing) return editor.storeWork;
  const record = {
    listing_id: state.formListing?.id || null,
    revision: state.formListing?.revision || 0,
    input: editor.input,
    raw_price: editor.rawPrice,
    raw_price_mode: editor.rawPriceMode,
    attempt: state.createAttempt,
    pending: state.pending.map((p) => ({
      file: p.file,
      uploadKey: p.uploadKey,
    })),
  };
  editor.storeWork = editor.storeWork
    .catch(() => {})
    .then(() => editorStore.put(String(editor.user), record))
    .then(() => {
      editor.storeError = false;
    })
    .catch(() => {
      editor.storeError = true;
      editorStatus(
        "Браузер не сохраняет резервную копию. Не закрывай вкладку до сохранения на сервере.",
        true,
      );
    });
  return editor.storeWork;
}
function scheduleAutosave() {
  clearTimeout(autosaveTimer);
  if (!state.editor || state.editor.closing) return;
  autosaveTimer = setTimeout(
    () =>
      autosave().catch((e) =>
        editorStatus(
          "Не сохранено на сервере: " +
            e.message +
            ". Копия останется в этом браузере.",
          true,
        ),
      ),
    700,
  );
}
function draftHasContent(input) {
  return !!(
    input?.title.trim() ||
    input?.description.trim() ||
    input?.category ||
    input?.location ||
    input?.price_minor != null ||
    state.pending.length ||
    state.formListing
  );
}
async function autosave() {
  const editor = state.editor;
  if (!editor || editor.closing || editor.dirty <= editor.saved) return;
  editor.work = editor.work
    .catch(() => {})
    .then(async () => {
      if (
        editor.closing ||
        editor !== state.editor ||
        editor.dirty <= editor.saved
      )
        return;
      await state.photoWork;
      const version = editor.dirty;
      if (!draftHasContent(editor.input)) {
        editor.saved = version;
        await editorStore.remove(String(editor.user));
        editorStatus("Пустой черновик не создаётся");
        return;
      }
      const input = editor.input || {
        draft: true,
        kind: "offer",
        category: "",
        title: "",
        description: "",
        location: "",
        dorm_id: null,
        price_mode: "unset",
        price_minor: null,
        telegram: "",
        vk: "",
      };
      const data = new Map(
        Object.entries({
          ...input,
          price:
            input.price_minor == null ? "" : String(input.price_minor / 100),
        }),
      );
      await saveListing(editor.form, data, "autosave");
      editor.saved = version;
      await storeEditor(editor);
      if (
        editor.dirty === editor.saved &&
        state.pending.length === 0 &&
        !(
          editor.rawPriceMode === "price" && editor.input.price_mode !== "price"
        )
      ) {
        await editor.storeWork;
        await editorStore.remove(String(editor.user));
        editorStatus(
          state.formListing?.status === "completed"
            ? "Изменения сохранены"
            : "Сохранено в архив",
        );
      } else scheduleAutosave();
    });
  return editor.work;
}
async function leaveEditor() {
  clearTimeout(autosaveTimer);
  const editor = state.editor;
  if (!editor) return;
  if (!editor.closing) {
    captureEditor();
    await state.photoWork;
    await autosave();
    await editor.storeWork;
  } else {
    await editor.work.catch(() => {});
    await editor.storeWork;
    await editorStore.remove(String(editor.user));
  }
  state.editor = null;
}
