"use strict";
const { escape: h, minor, price, linkify } = DormHubUtils;
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
  cropSession: null,
  resendAt: 0,
  editor: null,
  completedDeals: 0,
  deletionResendAt: 0,
  maintenance: false,
  siteMaintenance: false,
  maintenanceReason: "",
  legal: null,
  loginAcceptance: null,
  showcaseSeed: String(Math.floor(Date.now() / 14400000)),
  railOffsets: new Map(),
  showcaseKey: "",
  adminLogin: location.pathname === "/admin-login",
};
const app = document.querySelector("#app"),
  dialog = document.querySelector("#dialog");
const landingHTML = document.querySelector("#landing") ? app.innerHTML : document.querySelector("#public-landing")?.innerHTML || "";
const landingTitle = "Dorm Hub — объявления студентов СПбГУ";
const categoryIcons = {
  things: '<rect x="3" y="3" width="18" height="14" rx="2"/><path d="M8 21h8m-4-4v4"/>',
  clothes: '<path d="m8 3-5 3 2 5 3-1v11h8V10l3 1 2-5-5-3a4 4 0 0 1-8 0Z"/>',
  accessories: '<rect x="5" y="8" width="14" height="13" rx="2"/><path d="M9 8V6a3 3 0 0 1 6 0v2"/>',
  home: '<path d="m3 10 9-7 9 7M5 9v12h14V9M9 21v-8h6v8"/>',
  study: '<path d="M12 5c-3-2-6-2-10-1v15c4-1 7-1 10 1 3-2 6-2 10-1V4c-4-1-7-1-10 1Zm0 0v15"/>',
  help: '<path d="M14 3a5 5 0 0 0-5 7l-6 6a3 3 0 0 0 4 4l6-6a5 5 0 0 0 7-5l-3 3-5-5 3-3Z"/>',
  other: '<circle cx="5" cy="12" r="1"/><circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/>',
};
function categoryIcon(code) {
  const paths = Object.hasOwn(categoryIcons, code) ? categoryIcons[code] : categoryIcons.other;
  return `<svg class="category-icon" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">${paths}</svg>`;
}
const categoryNames = {things: "Техника", clothes: "Одежда и обувь", accessories: "Аксессуары", home: "Для дома", study: "Книги и пособия", help: "Услуги", other: "Другое"};
const categoryHints = {things: "Бытовая техника, электроника", clothes: "Одежда, обувь", accessories: "Украшения, сумки, обложки", home: "Мебель, посуда, предметы быта", study: "Книги, учебники, пособия", help: "Печать, ремонт, помощь", other: "Всё остальное"};
const locationNames = {PETRODVORTSOVY: "ПУНК", KHALTURINA: "Халтурина", GSOM: "ВШМ", VASILEOSTROVSKY: "Васильевский остров", NEVSKY: "Солидарности", EXTERNAL: "Другой адрес", UNSPECIFIED: "Место не указано"};
function dormCaption(dorm) { const number = dorm.name.match(/^№\s*(\d+)/); return number ? "Общежитие № " + number[1] : dorm.name; }
function shortPlace(listing) { const dorm = listing.dorm_name?.match(/^№\s*(\d+)/); return dorm ? "Общежитие № " + dorm[1] : listing.location === "EXTERNAL" ? listing.address || "Другой адрес" : locationNames[listing.location] || ""; }
function listingFields(data, strict = false) {
  let location = data.get("location") || "", dormID = data.get("dorm_id") ? Number(data.get("dorm_id")) : null;
  if (data.has("place")) {
    const place = data.get("place"), dorm = state.catalog?.dorms.find(d => String(d.id) === place);
    location = place === "EXTERNAL" ? "EXTERNAL" : dorm?.location || "";
    dormID = dorm ? dorm.id : null;
  }
  let amount = null;
  if (data.get("price_mode") === "price" && data.get("price")) {
    try { amount = minor(data.get("price")); } catch (error) { if (strict) throw error; }
  }
  if (strict && data.get("price_mode") === "price" && amount === null) throw new Error("Укажи цену или выбери другой вариант стоимости");
  return {draft: true, kind: data.get("kind") || "offer", category: data.get("category") || "", title: data.get("title") || "", description: data.get("description") || "", location, address: location === "EXTERNAL" ? data.get("address") || "" : "", dorm_id: location === "EXTERNAL" ? null : dormID, price_mode: data.get("price_mode") === "price" && amount === null ? "unset" : data.get("price_mode") || "unset", price_minor: amount, telegram: data.get("telegram") || "", vk: data.get("vk") || ""};
}
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
  fetch(...args), {onMaintenance(reason) {
    if (location.pathname !== "/admin-login") maintenanceView(reason);
  }},
);
function applySiteSettings(settings) {
  state.siteMaintenance = settings.maintenance;
  state.maintenanceReason = settings.reason || "";
}
function brand() {
  return `<a class="brand" href="${state.me ? '/app#/feed' : '/'}" aria-label="Dorm Hub — ${state.me ? 'лента' : 'главная'}">${document.getElementById("brand-mark").innerHTML}</a>`;
}
function authPath(action) {
  return state.adminLogin ? "/auth/admin/" + action : "/auth/" + action;
}
function enterApp() {
  const hash = location.hash?.startsWith("#/") ? location.hash : state.adminLogin && state.me?.is_admin ? "#/admin?tab=site" : "#/feed";
  history.replaceState(null, "", "/app" + hash);
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
  if (location === "EXTERNAL") return option("", "Не требуется", "");
  if (!location) return option("", "Сначала выберите район", "");
  return options(
    state.catalog.dorms.filter((d) => d.location === location),
    current,
    empty,
  );
}
function closeDialog() {
  state.cropSession?.finish();
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
  app.innerHTML = `<header class="topbar">
    <div class="row topbar-start"><button class="mobile-menu" data-action="menu" aria-label="Открыть меню" aria-controls="sidebar" aria-expanded="false">☰</button>${brand()}</div>
    <span class="deal-total small muted" title="По отметкам участников">Завершено сделок: ${state.completedDeals}</span>
    <form class="search" id="search-form"><span aria-hidden="true">⌕</span><input name="q" placeholder="Поиск объявлений" aria-label="Поиск объявлений" maxlength="200"><button aria-label="Искать">↵</button></form>
    <div class="row topbar-actions">${themeButton()}<a class="button new-listing" href="#/new" aria-label="Новое объявление"><span aria-hidden="true">＋</span><span class="new-label">Объявление</span></a><a href="#/settings" class="user-pill row" aria-label="Мой профиль"><div class="avatar">${h(state.me.nickname[0])}</div><span class="small">${h(state.me.nickname)}</span></a></div>
    </header><div class="layout"><aside class="sidebar" id="sidebar"><nav class="nav" aria-label="Главное меню">${nav.map(([href, icon, label]) => `<a href="${href}" class="${path === href ? "selected" : ""}"><span class="nav-icon" aria-hidden="true">${icon}</span>${label}</a>`).join("")}</nav><button class="button plain" data-action="logout">Выйти</button><nav class="app-documents" aria-label="О площадке"><a href="/about" target="_blank" rel="noopener">О Dorm Hub</a><a href="/rules" target="_blank" rel="noopener">Правила сайта</a><a href="/privacy" target="_blank" rel="noopener">Политика обработки ПД</a><a href="/consent" target="_blank" rel="noopener">Согласие на обработку ПД</a><a href="https://t.me/dorm_hub_spbu" target="_blank" rel="noopener noreferrer">Канал Dorm Hub в Telegram ↗</a></nav></aside><button class="sidebar-backdrop" data-action="menu" aria-label="Закрыть меню" tabindex="-1"></button><main class="main" id="view" tabindex="-1"><div class="boot" role="status">Загружаем…</div></main></div>`;
}
function mount(content, seq) {
  if (seq !== state.seq) return;
  const banner = state.me?.is_admin && state.siteMaintenance
    ? '<div class="notice" role="status">Технические работы включены. Посетители не видят площадку. <a href="#/admin?tab=site">Управление доступом</a></div>' : "";
  document.querySelector("#view").innerHTML = banner + content;
  tick();
}
function empty(title, text = "", button = "") {
  return `<div class="empty"><h2>${h(title)}</h2>${text ? `<p>${h(text)}</p>` : ""}${button}</div>`;
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
function photoMarkup(src, alt, lazy = false) {
  return `<img class="photo-foreground" src="${h(src)}" alt="${h(alt)}"${lazy ? ' loading="lazy"' : ''}><img class="photo-backdrop" src="${h(src)}" alt="" aria-hidden="true"${lazy ? ' loading="lazy"' : ''}>`;
}
function listingPlace(l) {
  return [locationNames[l.location] || l.location, l.location === "EXTERNAL" ? l.address : l.dorm_name ? dormCaption({name:l.dorm_name}) : ""].filter(Boolean).join(" · ");
}
function openPhoto() {
  const image = document.querySelector("#gallery img");
  if (!image) return;
  modal(
    "Фотография",
    `<img class="photo-viewer-image" src="${h(image.getAttribute("src"))}" alt="${h(image.alt)}">`,
  );
}
function card(l, scope = "feed") {
  const own = l.author_id === state.me.id;
  if (scope === "feed") return `<article class="listing-card compact-card"><div class="card-media"><a href="#/listing/${l.id}" class="${l.photos.length ? "photo-frame" : "card-placeholder"}" style="width:100%;height:100%" aria-label="${h(l.title || "Без названия")}">${l.photos.length ? photoMarkup(photoURL(l.photos[0].id), l.title || "Без названия", true) : categoryIcon(l.category)}</a>${own ? "" : `<button class="icon-button ${l.favorite ? "active" : ""}" data-action="favorite" data-id="${l.id}" data-value="${l.favorite}" aria-label="${l.favorite ? "Убрать из избранного" : "В избранное"}">${l.favorite ? "♥" : "♡"}</button>`}</div><div class="card-content"><h3><a class="listing-card-link" href="#/listing/${l.id}">${h(l.title || "Без названия")}</a></h3><span class="price">${h(price(l))}</span><div class="card-loc">⌖ ${h(shortPlace(l))}</div></div></article>`;
  return `<article class="listing-card" data-scope="${h(scope)}"><div class="card-media cat-${h(l.category)}"><a href="#/listing/${l.id}" class="${l.photos.length ? "photo-frame" : "card-placeholder"}" style="${l.photos.length ? "width:100%;height:100%" : ""}" aria-label="${h(l.title || "Без названия")}">${l.photos.length ? photoMarkup(photoURL(l.photos[0].id), l.title || "Без названия", true) : categoryIcon(l.category)}</a>${own || scope === "moderation" ? "" : `<button class="icon-button ${l.favorite ? "active" : ""}" data-action="favorite" data-id="${l.id}" data-value="${l.favorite}" aria-label="${l.favorite ? "Убрать из избранного" : "В избранное"}">${l.favorite ? "♥" : "♡"}</button>`}</div><div class="card-content"><div class="card-kind">${l.kind === "offer" ? "Предложение" : "Заявка"}</div><span class="eyebrow muted" style="font-size:9px;letter-spacing:1px">${h(categoryNames[l.category])}</span><h3><a class="listing-card-link" href="#/listing/${l.id}">${h(l.title || "Без названия")}</a></h3><p class="card-description">${h(l.description)}</p><div class="row between wrap"><span class="price">${h(price(l))}</span>${scope === "feed" && l.status === "active" ? "" : statusBadge(l)}</div><div class="card-loc">⌖ ${h(listingPlace(l))}</div>${scope === "mine" && l.moderation_comment ? `<p class="small rejection-note">${h(l.moderation_comment)}</p>` : ""}${scope === "moderation" ? `<button type="button" class="small muted category-correct" data-action="change-category" data-id="${l.id}" data-revision="${l.revision}" data-category="${h(l.category)}">Изменить категорию</button>` : ""}<div class="card-footer"><a href="#/profile/${l.author_id}">${h(l.author_name)}</a><span>${h(ago(l.created_at))}</span></div></div></article>`;
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
  if (page === "mine") { p.set("scope", "mine"); if (!p.has("status")) p.set("status", "active"); }
  else p.set("scope", page === "favorites" ? "favorites" : "feed");
  if (page === "feed") { if (!p.get("kind")) p.set("kind", "offer"); p.delete("dorm_id"); }
  const overview = page === "feed" && !p.get("q") && !p.get("category") && !p.get("all") && !p.get("author_id");
  const request = new URLSearchParams(p); request.delete("all");
  if (overview) { request.delete("offset"); request.set("seed", state.showcaseSeed); }
  const result = await api((overview ? "/showcase?" : "/listings?") + request);
  if (seq !== state.seq) return;
  const link = (changes, remove = []) => {
    const q = new URLSearchParams(p); q.delete("offset");
    for (const key of remove) q.delete(key);
    for (const [key, value] of Object.entries(changes)) value ? q.set(key, value) : q.delete(key);
    return "#/" + page + "?" + h(q.toString());
  };
  const title = page === "mine" ? "Мои объявления" : page === "favorites" ? "Избранное" : p.get("q") ? "Поиск" : categoryNames[p.get("category")] || "Объявления";
  const tabs = page === "mine" ? [["active","Опубликовано"],["pending","На проверке"],["archived","Архив"],["completed","Завершено"]] : page === "feed" ? [["offer","Предлагают"],["request","Ищут"]] : [["","Все"],["offer","Предлагают"],["request","Ищут"]];
  const key = page === "mine" ? "status" : "kind";
  let head = `<div class="browse-head"><div class="browse-heading"><h1>${h(title)}</h1><span class="feed-meta">${result.total} объявлений</span></div>${page === "feed" && !p.get("category") && !p.get("q") ? `<a class="small muted" href="${link({all: overview ? "1" : ""})}">${overview ? "Все новые ↗" : "По категориям"}</a>` : ""}${page === "feed" && p.get("category") ? `<a class="small muted" href="${link({},["category","all"])}">Все категории</a>` : ""}</div>`;
  head += `<div class="browse-toolbar"><nav class="tabs">${tabs.map(([value,label]) => `<a class="${(p.get(key)||"") === value ? "selected" : ""}" href="${link({[key]:value})}">${label}${overview && result.kind_totals ? ` <span class="small muted">${result.kind_totals[value] || 0}</span>` : ""}</a>`).join("")}</nav><form class="filters" id="filter-form" data-page="${page}"><select name="location" aria-label="Где искать">${options(state.catalog.locations,p.get("location"),"Все места")}</select>${["kind","status","category","q","all"].map(k=>`<input type="hidden" name="${k}" value="${h(p.get(k)||"")}">`).join("")}<button class="button plain" type="submit">Применить</button></form></div>`;
  if (page !== "mine") head += `<nav class="category-chips" aria-label="Категории"><a class="category-chip ${!p.get("category") ? "selected" : ""}" href="${link({category:""})}">Все</a>${state.catalog.categories.map(c=>`<a class="category-chip ${p.get("category") === c.code ? "selected" : ""}" href="${link({category:c.code},["all"])}">${h(c.name)}</a>`).join("")}</nav>`;
  let content = "";
  if (overview && result.sections.length) {
    state.showcaseKey = p.get("kind") + "|" + (p.get("location")||"") + "|" + state.showcaseSeed;
    content = result.sections.map(section => `<section class="showcase-section"><div class="showcase-heading"><h2>${h(section.name)}<span class="showcase-count">${section.total}</span></h2><div class="showcase-actions"><a href="${link({category:section.code})}">Смотреть все ↗</a><div class="showcase-arrows"><button type="button" data-action="rail-scroll" data-rail="${h(section.code)}" data-direction="-1" aria-label="${h(section.name)}: назад">‹</button><button type="button" data-action="rail-scroll" data-rail="${h(section.code)}" data-direction="1" aria-label="${h(section.name)}: вперёд">›</button></div></div></div><div class="showcase-rail" id="rail-${h(section.code)}" data-showcase-rail="${h(section.code)}">${section.items.map(l=>card(l)).join("")}</div></section>`).join("");
  } else if (!overview && result.items.length) content = `<div class="cards ${page === "mine" ? "" : "compact-cards"}">${result.items.map(l=>card(l,page === "mine" ? "mine" : "feed")).join("")}</div>${pager(result,p,page)}`;
  else content = empty(page === "favorites" ? "Пока ничего не сохранено" : "Нет объявлений", page === "favorites" ? "Нажми на сердечко у объявления." : "Попробуй другое место или категорию.", `<a class="button" href="#/new">Создать объявление</a>`);
  if (p.get("q")) head += `<p class="small muted">По запросу «${h(p.get("q"))}»</p>`;
  mount(head+content,seq);
  if (overview) document.querySelectorAll("[data-showcase-rail]").forEach(rail => rail.scrollLeft = state.railOffsets.get(state.showcaseKey+":"+rail.dataset.showcaseRail)||0);
  const search = document.querySelector("#search-form input"); if(search) search.value = p.get("q")||"";
}

async function detailView(id, seq) {
  const l = await api("/listings/" + id);
  if (seq !== state.seq) return;
  state.detail = l;
  const own = l.author_id === state.me.id;
  const contacts = [
    l.telegram &&
      `<a class="button" target="_blank" rel="noopener noreferrer" data-contact-event="telegram" data-listing-id="${l.id}" href="${h(l.telegram)}">Написать в Telegram ↗</a>`,
    l.vk &&
      `<a class="button secondary" target="_blank" rel="noopener noreferrer" data-contact-event="vk" data-listing-id="${l.id}" href="${h(l.vk)}">Написать в VK ↗</a>`,
  ]
    .filter(Boolean)
    .join("");
  mount(
    `<div class="breadcrumb"><a href="#/feed">← Лента</a> / ${h(categoryNames[l.category])}</div><div class="detail-grid"><section><div class="gallery" id="gallery">${l.photos.length ? `<button type="button" class="gallery-open photo-frame" data-action="open-photo" aria-label="Открыть фотографию целиком">${photoMarkup(photoURL(l.photos[0].id), l.title || "Без названия")}</button>` : categoryIcon(l.category)}</div>${l.photos.length > 1 ? `<div class="thumbs">${l.photos.map((p, i) => `<button data-action="gallery" data-photo="${h(p.id)}" aria-label="Фотография ${i + 1}"><img src="${photoURL(p.id)}" alt=""></button>`).join("")}</div>` : ""}<div class="panel" style="margin-top:24px"><h2>Описание</h2><div class="description">${linkify(l.description)}</div></div></section><section><div class="row wrap"><span class="pill">${l.kind === "offer" ? "Предложение" : "Заявка"}</span><span class="pill gray">${h(categoryNames[l.category])}</span>${statusBadge(l)}</div><h1 class="detail-title">${h(l.title || "Без названия")}</h1><div class="detail-price price">${h(price(l))}</div><div class="meta-list"><div><span class="muted">Местоположение</span><strong>${h(locationNames[l.location] || l.location)}</strong></div>${l.location === "EXTERNAL" && l.address ? `<div><span class="muted">Адрес</span><strong>${h(l.address)}</strong></div>` : ""}${l.dorm_name ? `<div><span class="muted">Общежитие</span><strong>${h(dormCaption({name:l.dorm_name}))}</strong></div>` : ""}<div><span class="muted">Опубликовано</span><span>${h(date(l.created_at))}</span></div></div>${own || state.me.is_admin ? moderationNotice(l) : ""}<div class="stack">${own ? `<a class="button" href="#/edit/${l.id}">Редактировать</a>${l.status === "active" ? `<button class="button secondary" data-action="complete" data-id="${l.id}">Завершить объявление</button>` : l.status === "archived" || l.status === "completed" ? `<button class="button secondary" data-action="transition" data-id="${l.id}" data-transition="submit" ${l.hidden ? "disabled" : ""}>Отправить на проверку</button>` : ""}${l.status !== "archived" ? `<button class="button plain" data-action="transition" data-id="${l.id}" data-transition="archive">Отложить в архив</button>` : ""}<button class="button plain danger-text" data-action="delete-listing" data-id="${l.id}">Удалить объявление</button>` : `${l.status === "active" || (state.me.is_admin && l.status === "pending") ? contacts : `<div class="notice">${l.status === "completed" ? "Автор завершил это объявление." : "Объявление доступно только автору и модератору."}</div>`}<button class="button plain" data-action="favorite" data-id="${l.id}" data-value="${l.favorite}">${l.favorite ? "♥ Убрать из избранного" : "♡ Сохранить в избранное"}</button>`}</div><div class="seller"><a class="row" href="#/profile/${l.author_id}"><div class="avatar">${h(l.author_name[0])}</div><div><strong>${h(l.author_name)}</strong><div class="small muted">Профиль №${l.author_id} ↗</div></div></a></div><div class="row wrap" style="margin-top:20px">${state.me.is_admin && l.status === "pending" ? `<button class="button" data-action="review-approve" data-id="${l.id}" data-revision="${l.revision}">Одобрить</button><button class="button secondary" data-action="review-reject" data-id="${l.id}" data-revision="${l.revision}">Отклонить</button>` : ""}${!own ? `<button class="small muted" data-action="report" data-kind="listing" data-id="${l.id}">Пожаловаться</button>` : ""}${state.me.is_admin ? `<button class="small muted" data-action="change-category" data-id="${l.id}" data-revision="${l.revision}" data-category="${h(l.category)}">Категория</button><button class="small muted" data-action="moderate" data-kind="listing" data-id="${l.id}" data-transition="${l.hidden ? "show" : "hide"}">${l.hidden ? "Показать" : "Скрыть"}</button><button class="small muted" data-action="moderate" data-kind="listing" data-id="${l.id}" data-transition="delete">Удалить</button>` : ""}</div></section></div>`,
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
    `<section class="profile-head"><div class="avatar large">${h(user.nickname[0])}</div><div><h1>${h(user.nickname)}</h1><div class="profile-stats"><span>№${user.id}</span><span>С ${h(date(user.created_at))}</span><span>★ ${user.stars}</span></div></div><div class="row wrap" style="margin-left:auto">${user.id !== state.me.id && user.active !== false && !user.blocked ? `<button class="button ${user.starred ? "secondary" : ""}" data-action="star" data-id="${user.id}" data-value="${user.starred}">${user.starred ? "★ Отозвать звезду" : "☆ Поставить звезду"}</button><button class="button plain" data-action="report" data-kind="user" data-id="${user.id}">Пожаловаться</button>` : ""}</div></section>${state.me.is_admin && user.blocked ? '<div class="notice">Пользователь заблокирован.</div>' : state.me.is_admin && user.active === false ? '<div class="notice">Аккаунт деактивирован.</div>' : ""}<h2>Объявления пользователя</h2>${list.items.length ? `<div class="cards ${state.me.is_admin ? "" : "compact-cards"}">${list.items.map((l) => card(l, state.me.is_admin ? "moderation" : "feed")).join("")}</div>${pager(list, listingParams, "profile/" + id)}` : empty("Нет доступных объявлений")}`,
    seq,
  );
}
function settingsView(seq) {
  const u = state.me;
  mount(
    `<div class="page-head"><h1>Мой профиль</h1></div><div class="panel" style="max-width:800px"><div class="profile-head"><div class="avatar large">${h(u.nickname[0])}</div><div><h2>${h(u.nickname)}</h2><div class="profile-stats"><span>№${u.id}</span><span>★ ${u.stars}</span><span>С ${h(date(u.created_at))}</span></div></div></div><form id="settings-form" class="form-grid"><label class="field full"><span>Никнейм</span><input name="nickname" minlength="3" maxlength="30" value="${h(u.nickname)}" required ${u.nickname_changed ? "readonly" : ""}><span class="hint">${u.nickname_changed ? "Однократная смена никнейма уже использована." : "Никнейм можно изменить только один раз."}</span></label><label class="field"><span>Telegram</span><input name="telegram" type="text" maxlength="300" placeholder="@username или https://t.me/username" value="${h(u.telegram)}"></label><label class="field"><span>VK</span><input name="vk" type="text" maxlength="300" placeholder="username или https://vk.com/username" value="${h(u.vk)}"></label><div class="full small muted">Корпоративная почта: ${h(state.email)}. Другим пользователям она не видна. Контакты уже созданных объявлений сохраняются отдельно.</div><div class="full"><button class="button" type="submit">Сохранить изменения</button></div></form><section class="danger-zone"><h3>Удаление аккаунта</h3><p class="small muted">Аккаунт, объявления, фотографии, контакты и другие связанные данные будут удалены без возможности восстановления. Для подтверждения отправим отдельный код на твою почту.</p><button class="button danger" data-action="delete-account">Удалить аккаунт</button></section></div>`,
    seq,
  );
}
async function listingPreview(form) {
  const editor = state.editor;
  clearTimeout(autosaveTimer);
  let work;
  do { work = state.photoWork; await work; await editor?.work.catch(() => {}); } while (work !== state.photoWork);
  clearTimeout(autosaveTimer);
  if (!form.isConnected || editor !== state.editor) throw new Error("Открой объявление заново");
  const input = listingFields(new FormData(form), true);
  if (!categoryNames[input.category]) throw new Error("Выбери категорию");
  if (!input.location) throw new Error("Выбери общежитие или другой адрес");
  if (!input.telegram.trim() && !input.vk.trim()) throw new Error("Укажи Telegram или VK");
  if (input.title.trim().length < 3 || input.description.trim().length < 10) throw new Error("Заполни название и описание");
  captureEditor(form);
  const saved = state.formListing?.photos || [];
  const sources = saved.map(p => state.pending.find(item => item.replaceID === p.id)?.url || photoURL(p.id))
    .concat(state.pending.filter(p => !p.replaceID || !saved.some(photo => photo.id === p.replaceID)).map(p => p.url));
  const dorm = state.catalog.dorms.find(d => d.id === input.dorm_id);
  return `<div class="listing-preview"><div class="preview-photos">${sources[0] ? `<img src="${h(sources[0])}" alt="${h(input.title)}">` : `<div class="card-placeholder">${categoryIcon(input.category)}</div>`}${sources.length > 1 ? `<div class="preview-thumbnails">${sources.slice(1).map(src=>`<img src="${h(src)}" alt="Фотография объявления">`).join("")}</div>` : ""}</div><div class="preview-copy"><span class="small muted">${input.kind === "offer" ? "Предлагаю" : "Ищу"} · ${h(categoryNames[input.category])}</span><h2>${h(input.title)}</h2><div class="price">${h(price(input))}</div><p class="small muted">${h(dorm ? dormCaption(dorm) : input.address || "Другой адрес")}</p><div class="description">${linkify(input.description)}</div><p class="small">${h([input.telegram,input.vk].filter(Boolean).join(" · "))}</p></div></div>`;
}

async function formView(id, seq, params = new URLSearchParams()) {
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
      kind: params.get("kind") === "request" ? "request" : "offer",
      category: state.catalog.categories.some(c => c.code === params.get("category")) ? params.get("category") : "",
      title: "",
      description: "",
      location: "",
      dorm_id: null,
      price_mode: "unset",
      telegram: state.me.telegram,
      vk: state.me.vk,
    };
  // A browser may still hold a draft from before the category migration.
  if (!state.catalog.categories.some(c => c.code === v.category)) v.category = "";
  mount(
    `<div class="page-head"><h1>${l ? "Редактировать объявление" : "Новое объявление"}</h1><p id="autosave-status" class="small muted" role="status">Черновик сохраняется автоматически.</p></div>${l ? moderationNotice(l) : ""}
    <form id="listing-form" class="panel editor-wizard">
      <nav class="editor-steps" aria-label="Создание объявления"><span data-step-label="1"><b>1</b>Категория</span><span data-step-label="2"><b>2</b>Объявление</span><span data-step-label="3"><b>3</b>Проверка</span></nav>
      <input type="hidden" name="kind" value="${h(v.kind)}"><input type="hidden" name="category" value="${h(v.category)}">
      <fieldset data-editor-step="1"><legend>Что хочешь разместить?</legend>
        <div class="kind-choices"><button type="button" data-wizard-kind="offer">Предлагаю</button><button type="button" data-wizard-kind="request">Ищу</button></div>
        <div class="category-choices">${state.catalog.categories.map(c=>`<button type="button" class="category-choice" data-wizard-category="${h(c.code)}"><span class="category-symbol" aria-hidden="true">${categoryIcon(c.code)}</span><span>${h(c.name)}<small>${h(categoryHints[c.code]||"")}</small></span></button>`).join("")}</div>
        <div class="wizard-actions"><a class="small muted" href="#/mine?status=archived">Закрыть</a><button type="button" class="button" data-wizard="next">Продолжить →</button></div>
      </fieldset>
      <fieldset data-editor-step="2" hidden><legend>Объявление</legend><div class="form-grid">
        <label class="field full"><span>Название</span><input name="title" minlength="3" maxlength="120" value="${h(v.title)}" required placeholder="Например, настольная лампа"></label>
        <label class="field full"><span>Описание</span><textarea name="description" minlength="10" maxlength="10000" required placeholder="Состояние, подробности, как забрать">${h(v.description)}</textarea></label>
        <label class="field full"><span>Местоположение</span><select name="place" required aria-label="Общежитие">${option("","Выбери общежитие",v.dorm_id || (v.location === "EXTERNAL" ? "EXTERNAL" : ""))}${state.catalog.dorms.map(d=>option(d.id,dormCaption(d)+(d.location === "GSOM" ? " · ВШМ" : ""),v.dorm_id)).join("")}${option("EXTERNAL","Другой адрес",v.location === "EXTERNAL" ? "EXTERNAL" : "")}</select></label>
        <label class="field full" id="address-field" ${v.location !== "EXTERNAL" ? "hidden" : ""}><span>Адрес · необязательно</span><input name="address" maxlength="300" value="${h(v.address||"")}" ${v.location !== "EXTERNAL" ? "disabled" : ""} placeholder="Улица, дом или место встречи"></label>
        <label class="field"><span>Стоимость</span><select name="price_mode">${option("unset","Не указано",v.price_mode)+option("price","Цена",v.price_mode)+option("exchange","Обмен",v.price_mode)}</select></label>
        <label class="field" id="price-field" ${v.price_mode !== "price" ? "hidden" : ""}><span>Цена, ₽</span><input name="price" type="text" inputmode="decimal" value="${recover && recovery.raw_price !== undefined ? h(recovery.raw_price) : v.price_minor != null ? h(v.price_minor/100) : ""}" placeholder="0 — бесплатно"></label>
      </div><div class="form-section"><h2>Фотографии</h2><p>До 10 фотографий.</p></div><div id="photo-list" class="photo-list"></div><label class="upload"><strong>＋ Добавить фотографии</strong><input id="photo-input" type="file" accept="image/jpeg,image/png" multiple></label>
      <div class="form-section"><h2>Контакты</h2><p>Укажи хотя бы один контакт.</p></div><div class="form-grid"><label class="field"><span>Telegram</span><input name="telegram" maxlength="300" value="${h(v.telegram)}" placeholder="@username"></label><label class="field"><span>VK</span><input name="vk" maxlength="300" value="${h(v.vk)}" placeholder="Имя или ссылка на профиль"></label></div>
      <p class="error-text small" data-preview-error role="alert" hidden></p><div class="wizard-actions"><button type="button" class="button plain" data-wizard="back">← Категория</button><button type="button" class="button" data-wizard="preview">Предпросмотр →</button></div></fieldset>
      <fieldset data-editor-step="3" hidden><legend>Проверь объявление</legend><div data-listing-preview></div><div class="wizard-actions"><button type="button" class="button plain" data-wizard="back">← Редактировать</button><button class="button" type="submit">${l?.status === "completed" ? "Сохранить изменения" : "Отправить на проверку"}</button></div></fieldset>
      <div class="editor-footer"><button class="button secondary" type="submit" name="save_mode" value="draft" formnovalidate>Сохранить в архив</button><a class="small muted" href="#/mine?status=archived">Закрыть</a></div>
    </form>`,
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
      replaceID: item.replaceID,
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
  if (globalThis.DormHubWizard) DormHubWizard.init(state.editor.form, {
    preview: () => listingPreview(state.editor.form),
    changed: () => { captureEditor(); scheduleAutosave(); },
    initial: v.category && (l || recover) ? 2 : 1,
  });
}

function renderPhotos() {
  const target = document.querySelector("#photo-list");
  if (!target) return;
  const existing = state.formListing?.photos || [];
  function thumbnail(src, alt, index, photo, pendingIndex) {
    const local = pendingIndex >= 0 ? state.pending[pendingIndex] : null;
    const cropTarget = local ? `data-index="${pendingIndex}"` : `data-photo="${h(photo.id)}"`;
    return `<div class="photo-item"><div class="photo-preview photo-frame">${photoMarkup(src, alt)}${index === 0 ? '<span class="pill">Обложка</span>' : ""}<button type="button" class="photo-crop-button" data-action="crop" ${cropTarget} ${local?.uploadKey ? "disabled" : ""} aria-label="Обрезать: ${h(alt)}">Обрезать</button></div><div class="row">${photo && index > 0 ? `<button type="button" data-action="cover" data-photo="${h(photo.id)}" ${local ? "disabled" : ""}>В обложку</button>` : ""}${local ? `<button type="button" data-action="remove-pending" data-index="${pendingIndex}" ${local.uploadKey ? "disabled" : ""}>${local.replaceID ? "Отменить обрезку" : "Удалить"}</button>` : `<button type="button" data-action="delete-photo" data-photo="${h(photo.id)}">Удалить</button>`}</div></div>`;
  }
  target.innerHTML = existing.map((p, i) => {
    const pendingIndex = state.pending.findIndex(item => item.replaceID === p.id);
    return thumbnail(pendingIndex >= 0 ? state.pending[pendingIndex].url : photoURL(p.id), `Фотография ${i + 1}`, i, p, pendingIndex);
  }).join("") + state.pending.map((p, i) => p.replaceID && existing.some(photo => photo.id === p.replaceID) ? "" : thumbnail(p.url, `Новая фотография ${i + 1}`, existing.length + state.pending.slice(0, i).filter(p => !p.replaceID).length, null, i)).join("");
  const form = target.closest("form");
  if (form?.dataset.busy) lockControls(form);
}
const photoLimits = {
  uploadBytes: 5 * 1024 * 1024,
  sourceBytes: 50 * 1024 * 1024,
  sourcePixels: 64000000,
  sourceSide: 16000,
  longSide: 1920,
};
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
    if ((state.formListing?.photos.length || 0) + pending.filter(p => !p.replaceID).length >= 10) {
      toast("Можно добавить не больше 10 фотографий", true);
      break;
    }
    if (!["image/jpeg", "image/png"].includes(file.type)) {
      toast("Допустимы фотографии JPEG и PNG", true);
      continue;
    }
    if (file.size > photoLimits.sourceBytes) {
      toast("Исходная фотография должна быть не больше 50 МБ", true);
      continue;
    }
    const url = URL.createObjectURL(file);
    try {
      const image = await imageFrom(url);
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
function canvasJPEG(canvas, quality) {
  return new Promise((resolve, reject) =>
    canvas.toBlob(
      (blob) =>
        blob && blob.size && blob.type === "image/jpeg"
          ? resolve(blob)
          : reject(new Error("Не удалось обработать фотографию")),
      "image/jpeg",
      quality,
    ),
  );
}
async function normalizePhoto(image) {
  const width = image.naturalWidth || image.width,
    height = image.naturalHeight || image.height;
  if (
    !Number.isFinite(width) || !Number.isFinite(height) ||
    width < 1 || height < 1 ||
    width > photoLimits.sourceSide || height > photoLimits.sourceSide ||
    width * height > photoLimits.sourcePixels
  )
    throw new Error("Исходная фотография должна быть не больше 64 мегапикселей и 16000 пикселей по стороне");
  const canvas = document.createElement("canvas");
  let scale = Math.min(1, photoLimits.longSide / Math.max(width, height));
  try {
    for (let attempt = 0; attempt < 4; attempt++) {
      canvas.width = Math.max(1, Math.round(width * scale));
      canvas.height = Math.max(1, Math.round(height * scale));
      const context = canvas.getContext("2d");
      if (!context) throw new Error("Не удалось обработать фотографию");
      context.imageSmoothingEnabled = true;
      context.imageSmoothingQuality = "high";
      context.fillStyle = "#fff";
      context.fillRect(0, 0, canvas.width, canvas.height);
      context.drawImage(image, 0, 0, canvas.width, canvas.height);
      for (const quality of [0.9, 0.8, 0.7]) {
        const blob = await canvasJPEG(canvas, quality);
        if (blob.size <= photoLimits.uploadBytes) return blob;
      }
      scale *= 0.75;
    }
    throw new Error("Не удалось сжать фотографию до 5 МБ");
  } finally {
    canvas.width = canvas.height = 1;
  }
}
function imageFrom(url) {
  return new Promise((resolve, reject) => {
    const image = new Image();
    image.onload = () => resolve(image);
    image.onerror = () => reject(new Error("Не удалось прочитать изображение"));
    image.src = url;
  });
}
function cropRect(width, height, ratio = 0) {
  const w = ratio > 0 ? Math.min(width, height * ratio) : width;
  const h = ratio > 0 ? w / ratio : height;
  return { x: (width - w) / 2, y: (height - h) / 2, width: w, height: h };
}
function moveCrop(box, dx, dy, width, height) {
  return { ...box, x: Math.max(0, Math.min(width - box.width, box.x + dx)), y: Math.max(0, Math.min(height - box.height, box.y + dy)) };
}
function resizeCrop(box, corner, dx, dy, width, height, ratio = 0) {
  const east = corner.includes("e"), south = corner.includes("s");
  const ax = east ? box.x : box.x + box.width, ay = south ? box.y : box.y + box.height;
  const px = Math.max(0, Math.min(width, (east ? box.x + box.width : box.x) + dx));
  const py = Math.max(0, Math.min(height, (south ? box.y + box.height : box.y) + dy));
  const sx = Math.sign(px - ax) || (east ? 1 : -1), sy = Math.sign(py - ay) || (south ? 1 : -1);
  let w = Math.max(1, Math.abs(px - ax)), h = Math.max(1, Math.abs(py - ay));
  if (ratio > 0) { w = Math.max(w, h * ratio); h = w / ratio; }
  const fit = Math.min(1, (sx > 0 ? width - ax : ax) / w, (sy > 0 ? height - ay : ay) / h);
  if (fit <= 0) return { ...box };
  w *= fit; h *= fit;
  return { x: sx > 0 ? ax : ax - w, y: sy > 0 ? ay : ay - h, width: w, height: h };
}
async function crop(index, photoID) {
  if (state.cropSession) return;
  const seq = state.seq, pending = state.pending, editor = state.editor;
  let item = index == null ? null : pending[index];
  if (index != null && !item) return;
  clearTimeout(autosaveTimer);
  let resolveClosed;
  const closed = new Promise(resolve => { resolveClosed = resolve; });
  const session = {
    cancelled: false,
    finish() {
      if (session.cancelled) return;
      session.cancelled = true;
      dialog.removeEventListener("close", session.finish);
      if (state.cropSession === session) state.cropSession = null;
      resolveClosed();
      if (seq === state.seq) scheduleAutosave();
    },
  };
  state.cropSession = session;
  try {
    // Drain the save already in flight before installing the crop gate.
    await state.photoWork;
    await editor?.work.catch(() => {});
    if (session.cancelled || seq !== state.seq || pending !== state.pending) { session.finish(); return; }
    if (item && !pending.includes(item)) {
      photoID = item.savedID;
      item = null;
    }
    if (!item && photoID) item = pending.find(p => p.replaceID === photoID) || null;
    const saved = state.formListing?.photos.find(p => p.id === photoID);
    if ((!item && !saved) || item?.uploadKey) {
      if (item?.uploadKey) toast("Повтори сохранение, чтобы подтвердить загрузку фотографии", true);
      session.finish(); return;
    }
    state.photoWork = closed;
    const image = await imageFrom(item ? item.url : photoURL(saved.id));
    if (session.cancelled || seq !== state.seq || pending !== state.pending) { session.finish(); return; }
    modal("Обрезать фотографию", `<p class="small muted">Перетаскивай углы рамки или саму рамку.</p><canvas id="crop-canvas" class="crop-canvas" width="1200" height="900" aria-label="Область обрезки фотографии"></canvas><div class="crop-tools"><label class="field"><span>Пропорции</span><select id="crop-ratio"><option value="0">Свободно</option><option value="original">Исходные</option><option value="${4/3}">4:3</option><option value="1">1:1</option><option value="${16/9}">16:9</option><option value="${3/4}">3:4</option></select></label><label class="field"><span>Ширина, px</span><input type="number" id="crop-width" min="1" step="1" inputmode="numeric"></label><label class="field"><span>Высота, px</span><input type="number" id="crop-height" min="1" step="1" inputmode="numeric"></label></div><div class="form-actions"><button type="button" class="button" id="crop-save">Применить</button><button type="button" class="button secondary" id="crop-reset">Сбросить</button><button type="button" class="button plain" data-action="close-dialog">Отмена</button></div>`);
    dialog.addEventListener("close", session.finish, { once: true });
    const canvas = document.querySelector("#crop-canvas"), context = canvas.getContext("2d");
    if (!context) throw new Error("Не удалось открыть редактор фотографии");
    const widthInput = document.querySelector("#crop-width"), heightInput = document.querySelector("#crop-height");
    const ratioInput = document.querySelector("#crop-ratio"), save = document.querySelector("#crop-save");
    const width = image.naturalWidth || image.width, height = image.naturalHeight || image.height;
    widthInput.max = width; heightInput.max = height;
    const scale = Math.min(canvas.width / width, canvas.height / height);
    const ox = (canvas.width - width * scale) / 2, oy = (canvas.height - height * scale) / 2;
    let ratio = 0, box = cropRect(width, height), drag = null;
    function draw() {
      context.fillStyle = "#181818"; context.fillRect(0, 0, canvas.width, canvas.height);
      context.drawImage(image, ox, oy, width * scale, height * scale);
      const x = ox + box.x * scale, y = oy + box.y * scale, w = box.width * scale, h = box.height * scale;
      context.fillStyle = "rgba(0,0,0,.55)";
      context.fillRect(ox, oy, width * scale, y - oy);
      context.fillRect(ox, y + h, width * scale, oy + height * scale - y - h);
      context.fillRect(ox, y, x - ox, h);
      context.fillRect(x + w, y, ox + width * scale - x - w, h);
      const handle = 12 * canvas.width / canvas.getBoundingClientRect().width;
      context.strokeStyle = "#fff"; context.lineWidth = 2 * canvas.width / canvas.getBoundingClientRect().width;
      context.strokeRect(x, y, w, h); context.fillStyle = "#fff";
      for (const [px, py] of [[x,y], [x+w,y], [x,y+h], [x+w,y+h]]) context.fillRect(px-handle/2, py-handle/2, handle, handle);
      widthInput.value = Math.max(1, Math.round(box.width)); heightInput.value = Math.max(1, Math.round(box.height));
    }
    function point(e) {
      const rect = canvas.getBoundingClientRect();
      return { x: Math.max(0, Math.min(width, ((e.clientX - rect.left) * canvas.width / rect.width - ox) / scale)), y: Math.max(0, Math.min(height, ((e.clientY - rect.top) * canvas.height / rect.height - oy) / scale)) };
    }
    canvas.addEventListener("pointerdown", e => {
      e.preventDefault(); canvas.setPointerCapture(e.pointerId);
      const p = point(e), radius = 22 * canvas.width / canvas.getBoundingClientRect().width / scale;
      const corners = [["nw", box.x, box.y], ["ne", box.x+box.width, box.y], ["sw", box.x, box.y+box.height], ["se", box.x+box.width, box.y+box.height]];
      const corner = corners.find(([,x,y]) => Math.hypot(p.x-x,p.y-y) <= radius)?.[0];
      if (corner) drag = { ...p, box: { ...box }, corner };
      else if (p.x >= box.x && p.x <= box.x+box.width && p.y >= box.y && p.y <= box.y+box.height) drag = { ...p, box: { ...box }, move: true };
      else { box = { x:p.x, y:p.y, width:0, height:0 }; drag = { ...p, box: { ...box }, corner:"se" }; }
      draw();
    });
    canvas.addEventListener("pointermove", e => {
      if (!drag) return;
      const p = point(e);
      box = drag.move ? moveCrop(drag.box,p.x-drag.x,p.y-drag.y,width,height) : resizeCrop(drag.box,drag.corner,p.x-drag.x,p.y-drag.y,width,height,ratio);
      draw();
    });
    for (const event of ["pointerup", "pointercancel", "lostpointercapture"]) canvas.addEventListener(event, () => { drag = null; });
    ratioInput.addEventListener("change", () => { ratio = ratioInput.value === "original" ? width/height : Number(ratioInput.value); box = cropRect(width,height,ratio); draw(); });
    function dimension(which) {
      let w = which === "width" ? Number(widthInput.value) : box.width;
      let h = which === "height" ? Number(heightInput.value) : box.height;
      if (!Number.isFinite(w) || w < 1 || !Number.isFinite(h) || h < 1) { draw(); return; }
      w = Math.min(width,w); h = Math.min(height,h);
      if (ratio > 0) { if (which === "width") h = w/ratio; else w = h*ratio; }
      const fit = Math.min(1,width/w,height/h); w *= fit; h *= fit;
      box = moveCrop({ x:box.x+(box.width-w)/2, y:box.y+(box.height-h)/2, width:w, height:h },0,0,width,height);
      draw();
    }
    widthInput.addEventListener("change", () => dimension("width"));
    heightInput.addEventListener("change", () => dimension("height"));
    document.querySelector("#crop-reset").addEventListener("click", () => { box = cropRect(width,height,ratio); draw(); });
    save.addEventListener("click", async () => {
      if (save.disabled) return;
      save.disabled = true;
      const output = document.createElement("canvas");
      output.width = Math.max(1,Math.round(box.width)); output.height = Math.max(1,Math.round(box.height));
      try {
        output.getContext("2d").drawImage(image,box.x,box.y,box.width,box.height,0,0,output.width,output.height);
        const blob = await normalizePhoto(output);
        if (session.cancelled || seq !== state.seq || pending !== state.pending || !canvas.isConnected) return;
        if (item) {
          if (!pending.includes(item) || item.uploadKey) return;
          URL.revokeObjectURL(item.url); item.file = blob; item.url = URL.createObjectURL(blob);
        } else {
          if (!state.formListing?.photos.some(p => p.id === saved.id)) return;
          pending.push({ file:blob, url:URL.createObjectURL(blob), replaceID:saved.id });
        }
        captureEditor(); renderPhotos(); closeDialog();
      } catch (e) { if (!session.cancelled) toast(e.message,true); }
      finally { output.width = output.height = 1; if (save.isConnected) save.disabled = false; }
    });
    draw();
    return { closed };
  } catch (e) {
    session.finish();
    if (dialog.open) closeDialog();
    throw e;
  }
}
let logViewer = null;
function stopLogViewer() {
  logViewer?.stop();
  logViewer = null;
}
function logPanel() {
  return `<section id="admin-log-panel" class="panel"><h2>Журнал приложения</h2><p class="small muted">Последние 500 сообщений. Журнал очищается при перезапуске.</p><form class="log-controls" data-log-filters><label class="field"><span>Уровень</span><select name="level"><option value="">Все</option><option value="INFO">Информация</option><option value="WARN">Предупреждения и ошибки</option><option value="ERROR">Ошибки</option></select></label><label class="field log-search"><span>Поиск по сообщению или ID запроса</span><input type="search" name="q" maxlength="200" placeholder="Например: 500 или request_id" autocomplete="off"></label><label class="log-auto"><input type="checkbox" name="auto" checked><span>Обновлять каждые 5 секунд</span></label><button class="button secondary" type="button" data-log-refresh>Обновить</button></form><p class="small muted" data-log-state role="status">Загружаем…</p><div class="log-list" data-log-list></div></section>`;
}
async function adminView(params, seq) {
  const tab = params.get("tab") || "listings",
    offset = Number(params.get("offset") || 0);
  let content = "";
  const tabs = `<nav class="tabs">${[
    ["listings", "Объявления"],
    ["categories", "Категории"],
    ["reports", "Жалобы"],
    ["users", "Пользователи"],
    ["dorms", "Общежития"],
    ["site", "Доступ к сайту"],
    ["logs", "Логи"],
    ["analytics", "Статистика"],
  ]
    .map(
      ([v, n]) =>
        `<a href="#/admin?tab=${v}" class="${tab === v ? "selected" : ""}">${n}</a>`,
    )
    .join("")}</nav>`;
  if (tab === "listings") {
    const page = await api("/admin/listings?offset=" + offset);
    content = page.items.length ? `<div class="cards">${page.items.map((l) => card(l, "moderation")).join("")}</div>${pager(page, params, "admin")}` : empty("Очередь пуста");
  } else if (tab === "categories") {
    const page = await api("/admin/listings?category_review=true&offset=" + offset);
    content = page.items.length ? `<div class="cards">${page.items.map(l=>card(l,"moderation")).join("")}</div>${pager(page,params,"admin")}` : empty("Нет объявлений для распределения");
  } else if (tab === "reports") {
    const status = params.get("status") || "new",
      items = await api(
        "/admin/reports?status=" +
          encodeURIComponent(status) +
          "&offset=" +
          offset,
      );
    content = `<form id="admin-filter" class="filters"><select name="status">${option("new", "Новые", status) + option("resolved", "Решённые", status) + option("dismissed", "Отклонённые", status)}</select><button class="button plain">Показать</button></form><div class="stack">${items.map((r) => `<article class="report-card"><div class="row between wrap"><strong>Жалоба №${r.id}</strong><span class="small muted">${h(date(r.created_at))}</span></div><div class="row wrap small" style="margin-top:10px"><a href="#/${r.target_type === "listing" ? "listing" : "profile"}/${r.target_id}">${r.target_type === "listing" ? "Объявление" : "Пользователь"} №${r.target_id} ↗</a><span class="pill gray">${h({ new: "Новая", resolved: "Решена", dismissed: "Отклонена" }[r.status] || r.status)}</span></div><div class="description">${h(r.reason)}</div>${r.resolution ? `<p class="small muted">Решение: ${h(r.resolution)}</p>` : ""}${r.status === "new" ? `<div class="row"><button class="button secondary" data-action="resolve" data-id="${r.id}" data-transition="resolved">Отметить решённой</button><button class="button plain" data-action="resolve" data-id="${r.id}" data-transition="dismissed">Отклонить</button></div>` : ""}</article>`).join("") || empty("Нет жалоб")}</div>${adminPager(items.length, offset, params)}`;
  } else if (tab === "users") {
    const items = await api("/admin/users?offset=" + offset);
    content = `<div class="panel table-wrap"><table><thead><tr><th>Пользователь</th><th>Состояние</th><th>Регистрация</th><th>Действия</th></tr></thead><tbody>${items.map((u) => `<tr><td><strong>${h(u.nickname)}</strong><div class="small muted" style="white-space:normal;overflow-wrap:anywhere">${h(u.email)}</div><div class="small muted">№${u.id}${u.is_admin ? " · администратор" : ""}</div></td><td><span class="pill ${u.blocked ? "red" : u.active ? "" : "gray"}">${u.blocked ? "Заблокирован" : u.active ? "Активен" : "Деактивирован"}</span></td><td>${h(date(u.created_at))}</td><td><div class="account-actions">${!u.is_admin && u.id !== state.me.id ? `<button data-action="moderate" data-kind="user" data-id="${u.id}" data-transition="${u.blocked ? "unblock" : "block"}">${u.blocked ? "Разблокировать" : "Заблокировать"}</button><button data-action="moderate" data-kind="user" data-id="${u.id}" data-transition="delete">Удалить</button>` : "—"}</div></td></tr>`).join("")}</tbody></table></div>${adminPager(items.length, offset, params)}`;
  } else if (tab === "dorms") {
    content = `<div class="panel"><h2>Справочник общежитий</h2><form id="dorm-form" class="form-grid"><label class="field"><span>Местоположение</span><select name="location" required>${options(state.catalog.locations.filter(l => l.code !== "EXTERNAL"), "")}</select></label><label class="field"><span>Название</span><input name="name" maxlength="100" required placeholder="Название или номер общежития"></label><div class="full"><button class="button" type="submit">Добавить общежитие</button></div></form><div class="table-wrap" style="margin-top:25px"><table><tbody>${state.catalog.dorms.map((d) => `<tr><td>${h(locationNames[d.location] || d.location)}</td><td>${h(dormCaption(d))}</td><td><button class="small muted" data-action="delete-dorm" data-id="${d.id}">Удалить</button></td></tr>`).join("") || '<tr><td class="muted">Нет общежитий</td></tr>'}</tbody></table></div></div>`;
  } else if (tab === "analytics") {
    const data = await api("/admin/analytics?days=" + encodeURIComponent(params.get("days") || "7") + "&offset=" + offset);
    content = DormHubAnalytics.render(data,state.catalog.categories);
  } else if (tab === "logs") {
    content = logPanel();
  } else if (tab === "site") {
    const settings = await api("/admin/site-settings");
    applySiteSettings(settings);
    content = `<section class="panel" style="max-width:760px"><h2>Технические работы</h2><p><strong>${settings.maintenance ? "Сайт закрыт для посетителей" : "Сайт открыт для посетителей"}</strong></p>${settings.maintenance ? `<div class="notice"><strong>Причина для посетителей</strong><p style="white-space:pre-wrap;overflow-wrap:anywhere">${h(settings.reason)}</p></div>` : ""}<button class="button ${settings.maintenance ? "" : "secondary"}" data-action="maintenance-toggle" data-value="${!settings.maintenance}" style="margin-top:16px">${settings.maintenance ? "Открыть сайт" : "Закрыть на технические работы"}</button><p class="small muted" style="margin-top:20px"><a href="/admin-login">Вход администратора</a></p></section>`;
  } else throw new Error("Раздел не найден");
  mount(
    `<div class="page-head"><h1>Модерация</h1></div>${tabs}${content}`,
    seq,
  );
  if (tab === "logs" && seq === state.seq) {
    logViewer = DormHubLogs.create({
      container: document.querySelector("#admin-log-panel"), api,
      active: () => seq === state.seq && !!state.me?.is_admin,
      onUnauthorized: () => { void guest(true); toast("Сессия завершена. Войди снова.", true); },
    });
  }
}
function adminPager(count, offset, params) {
  const p = new URLSearchParams(params),
    n = new URLSearchParams(params);
  p.set("offset", Math.max(0, offset - 50));
  n.set("offset", offset + 50);
  return `<div class="pagination">${offset ? `<a class="button plain" href="#/admin?${h(p.toString())}">← Назад</a>` : ""}${count === 50 ? `<a class="button plain" href="#/admin?${h(n.toString())}">Далее →</a>` : ""}</div>`;
}
async function route() {
  document.querySelectorAll("[data-showcase-rail]").forEach(rail => state.railOffsets.set(state.showcaseKey+":"+rail.dataset.showcaseRail,rail.scrollLeft));
  stopLogViewer();
  if (state.maintenance) return maintenanceView();
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
    else if (parts[0] === "new") await formView(null, seq, params);
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
      await guest(true);
      toast("Сессия завершена. Войди снова.", true);
    } else
      mount(
        `<div class="panel error-panel"><h2>Не удалось открыть страницу</h2><p class="muted">${h(e.message)}</p><a class="button" href="#/feed">В ленту</a><button class="button plain" data-action="retry">Повторить</button></div>`,
        seq,
      );
  }
}
function loginView(code = false) {
  document.title = state.adminLogin ? "Вход администратора — Dorm Hub" : "Вход — Dorm Hub";
  app.innerHTML = `<main class="login"><header class="login-header row between">${brand()}${themeButton()}</header><section class="login-side"><div class="login-box panel">
    <a class="login-home" href="/about">← О площадке и частые вопросы</a>
    <h1>${code ? "Код из письма" : state.adminLogin ? "Вход администратора" : "Вход"}</h1><p class="muted">${code ? "Код отправлен на " + h(state.email) + "." : state.adminLogin ? "Код придёт на почту администратора." : "Введи студенческую почту или разрешённый адрес. При первом входе аккаунт создастся автоматически."}</p>
    <form id="${code ? "code-form" : "login-form"}">
      ${code ? `<label class="field"><span>Код из письма</span><input class="code-input" name="code" maxlength="6" minlength="6" inputmode="numeric" pattern="[0-9]{6}" autocomplete="one-time-code" required autofocus></label>` : `
      <label class="field"><span>Почта</span><input name="email" type="email" maxlength="254" autocomplete="email" value="${h(state.email)}" placeholder="${state.adminLogin ? "Почта администратора" : "stXXXXXX@student.spbu.ru"}" required autofocus></label>
      ${state.adminLogin ? "" : `<label class="legal-check"><input type="checkbox" name="consent" required><span>Даю <a href="/consent" target="_blank" rel="noopener">согласие на обработку персональных данных</a>.</span></label>
      <label class="legal-check"><input type="checkbox" name="rules" required><span>Принимаю <a href="/rules" target="_blank" rel="noopener">правила сайта</a>.</span></label>`}`}
      <button class="button" type="submit">${code ? "Войти" : "Получить код"}</button>
    </form>
    ${code ? `<div class="row between" style="margin-top:18px"><button class="small muted" data-action="login-back">Изменить почту</button><button class="small muted" data-action="resend" id="resend-button">Отправить ещё раз</button></div>` : ""}
    ${state.adminLogin ? "" : `<div class="login-note">Доступ к объявлениям и профилям открыт только авторизованным участникам. Твою почту другие студенты не увидят.</div>
    <p class="login-legal">Как используются данные и как их удалить — в <a href="/privacy" target="_blank" rel="noopener">политике обработки ПД</a>.</p>`}
    </div></section></main>`;
  tick();
}

function maintenanceView(reason = state.maintenanceReason) {
  stopLogViewer();
  if (typeof reason === "string") state.maintenanceReason = reason;
  clearTimeout(autosaveTimer);
  if (!state.maintenance) {
    state.maintenance = true;
    ++state.seq;
    ++routeGeneration;
  }
  dialog.close();
  document.title = "Технические работы — Dorm Hub";
  app.innerHTML = `<main class="login"><header class="login-header row between">${brand()}${themeButton()}</header><section class="login-side"><div class="login-box panel"><h1>Технические работы</h1><p class="muted" style="white-space:pre-wrap;overflow-wrap:anywhere">${h(state.maintenanceReason || "Сайт временно недоступен.")}</p><div style="margin-top:24px"><a class="button" href="/">Проверить доступность</a></div><p class="login-legal"><a href="/rules">Правила</a> · <a href="/privacy">Политика обработки ПД</a> · <a href="/consent">Согласие</a></p><p style="margin-top:28px;text-align:right"><a class="small muted" href="/admin-login">Вход администратора</a></p></div></section></main>`;
}
async function guest(showLogin = false, goHome = false) {
  stopLogViewer();
  clearTimeout(autosaveTimer);
  state.editor = null;
  releasePending();
  state.me = null;
  state.csrf = "";
  state.catalog = null;
  ++state.seq;
  if (state.siteMaintenance && location.pathname !== "/admin-login") return maintenanceView();
  const data = await api("/auth/bootstrap");
  state.guestCSRF = data.csrf;
  state.legal = data.legal;
  state.loginAcceptance = null;
  if (goHome) history.replaceState(null, "", "/");
  state.adminLogin = location.pathname === "/admin-login";
  if (!showLogin && ["/", "/about"].includes(location.pathname) && !location.hash.startsWith("#/") && landingHTML) {
    app.innerHTML = landingHTML;
    document.title = landingTitle;
  } else loginView();
}
async function initialize() {
  if (location.pathname === "/about") return;
  state.maintenance = false;
  try {
    const data = await api("/me");
    state.me = data.user;
    state.email = data.email;
    state.csrf = data.csrf;
    if (state.me.is_admin) {
      applySiteSettings(await api("/admin/site-settings"));
    }
    state.catalog = await api("/catalog");
    await refreshStatistics();
    enterApp();
    await route();
  } catch (e) {
    if (e.status === 401 || (e.code === "maintenance" && location.pathname === "/admin-login")) {
      try {
        await guest();
      } catch (error) {
        bootError(error);
      }
    } else bootError(e);
  }
}
function bootError(error) {
  if (error.code === "maintenance") return maintenanceView(error.maintenanceReason);
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
  const input = {...listingFields(data, mode === "submit"), revision: previous?.revision || 0, has_photos: pending.length > 0};
  // Older browser backups may contain a rejected empty creation. Such a
  // request cannot have created a listing, so it must not replace new edits.
  if (
    !previous &&
    state.createAttempt &&
    !draftHasContent(state.createAttempt.input, [], null)
  )
    state.createAttempt = null;
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
  if (freshAttempt && !draftHasContent(input, pending, null)) {
    if (seq === state.seq && state.createAttempt === attempt) {
      state.createAttempt = null;
      if (state.editor) await storeEditor(state.editor);
    }
    throw new Error("Добавь название, описание или фотографию перед сохранением.");
  }
  if (freshAttempt && seq === state.seq && state.editor)
    await storeEditor(state.editor);
  let saved;
  if (previous) {
    saved = await api("/listings/" + previous.id, {
      method: "PUT",
      body: input,
    });
  } else {
    try {
      saved = await api("/listings", {
        method: "POST",
        body: attempt.input,
        idempotencyKey: attempt.key,
      });
    } catch (e) {
      // Release a definitively rejected first request. Recoveries after a lost
      // response keep their key: an earlier request may already have committed.
      if (
        freshAttempt &&
        [400, 401, 403, 413, 422, 429].includes(e.status) &&
        seq === state.seq &&
        state.createAttempt === attempt
      ) {
        state.createAttempt = null;
        if (state.editor) await storeEditor(state.editor);
      }
      throw e;
    }
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
    if (seq === state.seq) renderPhotos();
    if (seq === state.seq && state.editor) await storeEditor(state.editor);
    body.append("file", item.file, "photo.jpg");
    try {
      const photo = await api("/listings/" + saved.id + "/photos" + (item.replaceID ? "/" + encodeURIComponent(item.replaceID) : ""), {
        method: item.replaceID ? "PUT" : "POST",
        body,
        idempotencyKey: item.uploadKey,
      });
      if (item.replaceID) {
        const position = saved.photos.findIndex(p => p.id === item.replaceID);
        if (position >= 0) saved.photos.splice(position, 1);
      }
      if (!saved.photos.some((existing) => existing.id === photo.id)) saved.photos.push(photo);
      saved.photos.sort((a, b) => a.position - b.position);
      item.savedID = photo.id;
      pending.shift();
      URL.revokeObjectURL(item.url);
      if (saved.revision) saved.revision++;
      if (seq === state.seq && state.editor) await storeEditor(state.editor);
    } catch (e) {
      // These explicit errors occur before creation; the user can edit the file.
      if ([400, 403, 404, 409, 429].includes(e.status)) delete item.uploadKey;
      if (seq === state.seq) {
        try {
          const latest = await api("/listings/" + saved.id);
          const fields = [
            "kind",
            "category",
            "title",
            "description",
            "location",
            "address",
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
  if (seq === state.seq) { state.formListing = saved; renderPhotos(); }
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
      if (!state.adminLogin && (data.get("consent") !== "on" || data.get("rules") !== "on")) throw new Error("Подтверди согласие и правила");
      state.email = data.get("email").trim();
      state.loginAcceptance = state.adminLogin ? null : {consent: true, rules: true, ...state.legal};
      await api(authPath("request"), {
        method: "POST",
        body: { email: state.email, ...state.loginAcceptance },
        guest: true,
      });
      state.resendAt = Date.now() + 60000;
      loginView(true);
    } else if (form.id === "code-form") {
      const result = await api(authPath("verify"), {
        method: "POST",
        body: { email: state.email, code: data.get("code"), reactivate: false },
        guest: true,
      });
      state.me = result.user;
      state.email = result.email;
      state.csrf = result.csrf;
      if (state.me.is_admin) {
        applySiteSettings(await api("/admin/site-settings"));
      }
      state.catalog = await api("/catalog");
      await refreshStatistics();
      enterApp();
      await route();
    } else if (form.id === "maintenance-form") {
      const settings = await api("/admin/site-settings", {
        method: "PUT", body: {maintenance: form.dataset.enabled === "true", reason: data.get("reason")?.trim() || ""},
      });
      applySiteSettings(settings);
      closeDialog();
      toast(settings.maintenance ? "Сайт закрыт на технические работы" : "Сайт открыт для посетителей");
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
      await guest(false, true);
      toast(
        result.files_pending
          ? "Аккаунт удалён. Очистка файлов завершится автоматически."
          : "Аккаунт и связанные данные удалены",
      );
    } else if (form.id === "category-form") {
      await api("/admin/listings/" + form.dataset.id + "/category", {method:"PUT", body:{category:data.get("category"),revision:Number(form.dataset.revision)}});
      closeDialog(); toast("Категория изменена"); await route();
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
  if (el.name === "place") {
    const form = el.closest("form"), external = el.value === "EXTERNAL", address = form?.querySelector('[name="address"]');
    if (address) { form.querySelector("#address-field").hidden = !external; address.disabled = !external; if (!external) address.value = ""; }
  } else if (el.id === "photo-input") {
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
      dorm.disabled = !el.value || el.value === "EXTERNAL";
      if (form.id === "listing-form") {
        const external = el.value === "EXTERNAL", address = form.querySelector('[name="address"]');
        form.querySelector("#dorm-field").hidden = external;
        form.querySelector("#address-field").hidden = !external;
        address.disabled = !external;
        if (!external) address.value = "";
      }
    }
  } else if (el.name === "price_mode") {
    const field = document.querySelector("#price-field");
    if (field) field.hidden = el.value !== "price";
  }
});
document.addEventListener("click", async (event) => {
  const contact = event.target.closest("a[data-contact-event]");
  if (contact?.dataset.contactEvent && state.me && !state.me.is_admin) {
    void api("/analytics/events", {method:"POST",body:{listing_id:Number(contact.dataset.listingId),event:contact.dataset.contactEvent}}).catch(() => {});
  }
  const button = event.target.closest("[data-action]");
  if (!button) return;
  event.preventDefault();
  if (button.disabled || button.closest("form")?.dataset.busy) return;
  const { action, id, kind, transition, value, photo, index } = button.dataset;
  try {
    if (action === "rail-scroll") {
      const rail = document.getElementById("rail-" + button.dataset.rail);
      if (rail) rail.scrollBy({left: Number(button.dataset.direction) * Math.max(200, rail.clientWidth * .8), behavior: matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth"});
    } else if (action === "menu") {
      const side = document.querySelector(".sidebar");
      side.classList.toggle("open");
      document.querySelector(".layout")?.classList.toggle("menu-open", side.classList.contains("open"));
      document.querySelector(".mobile-menu")?.setAttribute("aria-expanded", String(side.classList.contains("open")));
    } else if (action === "close-dialog") closeDialog();
    else if (action === "initialize") await initialize();
    else if (action === "retry") await route();
    else if (action === "logout") {
      await leaveEditor();
      await api("/auth/logout", { method: "POST" });
      state.email = "";
      await guest(false, true);
    } else if (action === "maintenance-toggle") {
      if (!state.me?.is_admin) return;
      const enabled = value === "true";
      modal(enabled ? "Закрыть сайт для посетителей?" : "Открыть сайт для посетителей?",
        `<p class="muted">${enabled ? "Сайт станет недоступен участникам. Доступ администратора сохранится." : "Участники снова смогут входить и пользоваться площадкой."}</p><form id="maintenance-form" data-enabled="${enabled}">${enabled ? '<label class="field"><span>Сообщение для посетителей</span><textarea name="reason" required maxlength="1000" rows="4"></textarea></label>' : ""}<button class="button" type="submit" style="margin-top:16px">${enabled ? "Включить технические работы" : "Открыть сайт"}</button></form>`);
    } else if (action === "login-back") { state.loginAcceptance = null; loginView(false); }
    else if (action === "resend") {
      if (Date.now() < state.resendAt) return;
      button.disabled = true;
      try {
        await api(authPath("request"), {
          method: "POST",
          body: { email: state.email, ...state.loginAcceptance },
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
    } else if (action === "open-photo") openPhoto();
    else if (action === "gallery") {
      const image = document.querySelector("#gallery img");
      if (image) image.src = photoURL(photo);
      const background = document.querySelector("#gallery .photo-backdrop");
      if (background) background.src = photoURL(photo);
    } else if (action === "report")
      modal(
        "Пожаловаться",
        `<form id="report-form" data-kind="${h(kind)}" data-id="${h(id)}"><label class="field"><span>Причина</span><textarea name="reason" minlength="10" maxlength="2000" required></textarea></label><button class="button" type="submit" style="margin-top:18px">Отправить жалобу</button></form>`,
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
    } else if (action === "change-category" && state.me.is_admin) {
      modal("Категория", `<form id="category-form" data-id="${h(id)}" data-revision="${h(button.dataset.revision)}"><label class="field"><span>Категория</span><select name="category" required>${options(state.catalog.categories,button.dataset.category)}</select></label><button class="button" type="submit">Сохранить</button></form>`);
    } else if (action === "review-approve" || action === "review-reject") {
      const decision = action === "review-approve" ? "approve" : "reject";
      modal(
        decision === "approve" ? "Одобрить объявление?" : "Вернуть в архив",
        `<form id="review-form" data-id="${h(id)}" data-revision="${h(button.dataset.revision)}" data-decision="${decision}">${decision === "reject" ? '<label class="field"><span>Комментарий автору</span><textarea name="comment" minlength="3" maxlength="2000" required placeholder="Что нужно исправить перед публикацией"></textarea></label>' : ""}<button class="button" type="submit">${decision === "approve" ? "Опубликовать" : "Вернуть с комментарием"}</button></form>`,
      );
    } else if (action === "complete")
      modal(
        "Завершить объявление",
        `<form id="complete-form" data-id="${h(id)}"><label class="field"><span>Причина завершения</span><select name="outcome" required><option value="success">Сделка состоялась / нужное найдено</option><option value="no_longer_needed">Передумал / больше не актуально</option></select></label><button class="button" type="submit">Завершить</button></form>`,
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
      if (state.pending[Number(index)]?.uploadKey) {
        toast("Дождись загрузки или повтори сохранение, чтобы подтвердить её результат", true);
        return;
      }
      const [p] = state.pending.splice(Number(index), 1);
      if (p) URL.revokeObjectURL(p.url);
      renderPhotos();
      captureEditor();
      scheduleAutosave();
    } else if (action === "crop") await crop(index === undefined ? null : Number(index), photo);
    else if (action === "delete-photo" || action === "cover") {
      const seq = state.seq, form = button.closest("form");
      clearTimeout(autosaveTimer);
      await withBusy(form, () => changeSavedPhoto(action, photo));
      if (seq === state.seq) {
        renderPhotos();
        captureEditor();
        scheduleAutosave();
      }
    }
  } catch (e) {
    toast(e.message, true);
  } finally {
    if (button.isConnected) button.disabled = false;
  }
});
// Photo mutations share the editor queue with autosave and advance its local
// revision just as touchListing does on the server.
async function changeSavedPhoto(action, photo) {
  const seq = state.seq, id = state.formListing?.id, editor = state.editor;
  const change = async () => {
    if (seq !== state.seq || state.formListing?.id !== id) return;
    const listing = state.formListing;
    if (action === "delete-photo") {
      await api("/listings/" + id + "/photos/" + encodeURIComponent(photo), {method: "DELETE"});
      listing.photos = listing.photos.filter(p => p.id !== photo);
    } else {
      const ids = listing.photos.map(p => p.id), index = ids.indexOf(photo);
      if (index < 0) return;
      ids.splice(index, 1); ids.unshift(photo);
      await api("/listings/" + id + "/photos", {method: "PUT", body: {ids}});
      listing.photos.sort((a, b) => ids.indexOf(a.id) - ids.indexOf(b.id));
    }
    listing.photos.forEach((p, index) => {p.position = index;});
    if (listing.revision) listing.revision++;
    if (listing.status === "active") listing.status = "pending";
  };
  if (!editor) return change();
  editor.work = editor.work.catch(() => {}).then(change);
  return editor.work;
}
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
document.addEventListener("keydown", event => {
  if (event.key !== "Escape") return;
  document.querySelector(".sidebar")?.classList.remove("open");
  document.querySelector(".layout")?.classList.remove("menu-open");
  document.querySelector(".mobile-menu")?.setAttribute("aria-expanded", "false");
});
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
  if (!editor || editor.closing || !form || form.dataset.busy) return;
  const data = new FormData(form),
    raw = Object.fromEntries(data);
  const input = listingFields(data);
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
      replaceID: p.replaceID,
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
  if (state.maintenance || state.cropSession || !state.editor || state.editor.closing || state.editor.form?.dataset.step === "3") return;
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
function draftHasContent(input, pending = state.pending, listing = state.formListing) {
  return !!(
    input?.title?.trim() ||
    input?.description?.trim() ||
    input?.category ||
    input?.location ||
    input?.price_minor != null ||
    input?.has_photos ||
    pending.length ||
    listing
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
  if (state.cropSession) closeDialog();
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
