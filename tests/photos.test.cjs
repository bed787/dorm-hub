const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const MiB = 1024 * 1024;
const source = fs.readFileSync(path.join(__dirname, "../web/assets/app.js"), "utf8");

function harness({ encode, width = 4000, height = 3000, respond } = {}) {
  const events = {}, dialogEvents = {}, encodings = [], revoked = [], fills = [];
  const toast = { classList: { toggle() {} }, hidden: true };
  const content = { innerHTML: "" }, view = { innerHTML: "" };
  const image = {
    src: "/api/photos/first", alt: 'Лампа "<белая>"',
    getAttribute(name) { return this[name]; },
  };
  const dialog = {
    open: false,
    showModal() { this.open = true; },
    close() { this.open = false; },
    addEventListener(name, fn) { dialogEvents[name] = fn; },
    getBoundingClientRect() { return { left: 20, right: 600, top: 20, bottom: 500 }; },
  };
  const canvas = {
    width: 0, height: 0,
    getContext() {
      return { fillStyle: "", fillRect() { fills.push(this.fillStyle); }, drawImage() {} };
    },
    toBlob(callback, type, quality) {
      const call = { type, quality, width: this.width, height: this.height };
      encodings.push(call);
      callback(encode ? encode(call, encodings.length) : new Blob(["compressed"], { type }));
    },
  };
  class PhotoURLs extends URL {
    static createObjectURL() { return "blob:photo-" + ++this.count; }
    static revokeObjectURL(url) { revoked.push(url); }
    static count = 0;
  }
  class Image {
    constructor() { this.width = this.naturalWidth = width; this.height = this.naturalHeight = height; }
    set src(_) { queueMicrotask(() => this.onload()); }
  }
  const context = vm.createContext({
    DormHubUtils: require("../web/assets/utils.js"),
    DormHubRequests: require("../web/assets/requests.js"),
    Blob, FormData, URL: PhotoURLs, URLSearchParams, Image,
    document: {
      getElementById(id) { return id === "brand-mark" ? { innerHTML: '<svg class="brand-mark"></svg>' } : null; },
      querySelector(selector) {
        return { "#app": {}, "#dialog": dialog, "#dialog-content": content,
          "#gallery img": image, "#toast": toast, "#view": view }[selector] || null;
      },
      querySelectorAll() { return []; },
      createElement(tag) { assert.equal(tag, "canvas"); return canvas; },
      addEventListener(name, fn) { events[name] = fn; },
    },
    window: { addEventListener() {} }, location: { hash: "#/new" },
    setInterval() {}, setTimeout() {}, clearTimeout() {},
    fetch(url, options) {
      if (url === "/api/me") return new Promise(() => {});
      assert.ok(respond, "Unexpected HTTP request: " + url);
      return Promise.resolve(respond(url, options)).then(data => ({ ok: true, json: async () => ({ data }) }));
    },
  });
  vm.runInContext(source + "\nglobalThis.photos = {state, queuePhotos, normalizePhoto, saveListing, detailView, modal};", context);
  return { ...context.photos, encodings, revoked, fills, canvas, toast, dialog, content, view, image,
    click(action, extra = {}) {
      const button = { dataset: { action, ...extra }, closest() { return null; } };
      return events.click({ target: { closest() { return button; } }, preventDefault() {} });
    },
    backdrop(x, y) { dialogEvents.click({ target: dialog, clientX: x, clientY: y }); },
  };
}

function fields() {
  return new Map(Object.entries({ kind: "offer", category: "things", title: "Лампа",
    description: "Лампа для комнаты", location: "PETRODVORTSOVY", dorm_id: "",
    price_mode: "unset", telegram: "https://t.me/student", vk: "" }));
}

test("a JPEG larger than 5 MiB is compressed before upload", async () => {
  const requests = [];
  const h = harness({ respond: async (url, options) => {
    requests.push(url);
    if (url === "/api/listings") {
      assert.equal(JSON.parse(options.body).has_photos, true);
      return { id: 12, photos: [], status: "archived" };
    }
    assert.equal(url, "/api/listings/12/photos");
    const file = options.body.get("file");
    assert.equal(file.type, "image/jpeg");
    assert.ok(file.size <= 5 * MiB);
    assert.equal(await file.text(), "compressed");
    return { id: "saved" };
  } });
  h.queuePhotos([new Blob([new Uint8Array(8 * MiB)], { type: "image/jpeg" })]);
  await h.state.photoWork;
  assert.equal(h.state.pending.length, 1);
  assert.equal(h.encodings[0].width, 1920);
  assert.equal(h.encodings[0].height, 1440);
  assert.ok(h.revoked.includes("blob:photo-1"));
  await h.saveListing({}, fields(), "draft");
  assert.deepEqual(requests, ["/api/listings", "/api/listings/12/photos"]);
});

test("large PNGs and 48-megapixel phone photos are resized without cropping", async () => {
  const h = harness({ width: 8000, height: 6000 });
  h.queuePhotos([new Blob([new Uint8Array(6 * MiB)], { type: "image/png" })]);
  await h.state.photoWork;
  assert.equal(h.state.pending.length, 1);
  assert.equal(h.state.pending[0].file.type, "image/jpeg");
  assert.equal(h.encodings[0].width, 1920);
  assert.equal(h.encodings[0].height, 1440);
  assert.deepEqual(h.fills, ["#fff"]);
  assert.equal(h.canvas.width, 1);
});

test("compression lowers quality when the first JPEG still exceeds the upload limit", async () => {
  const h = harness({ encode: (_, attempt) => ({ type: "image/jpeg", size: (attempt === 1 ? 6 : 4) * MiB }) });
  const result = await h.normalizePhoto({ width: 4000, height: 3000 });
  assert.equal(result.size, 4 * MiB);
  assert.deepEqual(h.encodings.map(x => x.quality), [0.9, 0.8]);
  assert.ok(h.encodings.every(x => x.width === 1920 && x.height === 1440));
});

test("compression reduces dimensions after exhausting the quality attempts", async () => {
  const h = harness({ encode: (_, attempt) => ({ type: "image/jpeg", size: (attempt <= 3 ? 6 : 4) * MiB }) });
  const result = await h.normalizePhoto({ width: 4000, height: 3000 });
  assert.ok(result.size <= 5 * MiB);
  assert.deepEqual(h.encodings.map(x => [x.width, x.height]), [[1920, 1440], [1920, 1440], [1920, 1440], [1440, 1080]]);
});

test("compression fails after a bounded number of attempts rather than sending an oversized file", async () => {
  const h = harness({ encode: () => ({ type: "image/jpeg", size: 6 * MiB }) });
  await assert.rejects(h.normalizePhoto({ width: 4000, height: 3000 }), /до 5 МБ/);
  assert.equal(h.encodings.length, 12);
  assert.equal(h.canvas.width, 1);
});

test("failed, empty and unsupported canvas results are rejected", async () => {
  for (const result of [null, { size: 0, type: "image/jpeg" }, { size: 10, type: "image/png" }]) {
    const h = harness({ encode: () => result });
    await assert.rejects(h.normalizePhoto({ width: 100, height: 60 }), /обработать фотографию/);
    assert.equal(h.canvas.width, 1);
  }
});

test("unsupported files and oversized originals are rejected before opening an image", async () => {
  const h = harness();
  h.queuePhotos([{ type: "image/svg+xml", size: 50 }, { type: "image/jpeg", size: 51 * MiB }]);
  await h.state.photoWork;
  assert.equal(h.state.pending.length, 0);
  assert.equal(h.encodings.length, 0);
  assert.equal(h.revoked.length, 0);
  assert.match(h.toast.textContent, /50 МБ/);
});

test("invalid or excessive decoded dimensions fail before creating a canvas", async () => {
  const h = harness();
  for (const image of [{ width: 0, height: 10 }, { width: NaN, height: 10 },
    { width: 10000, height: 10000 }, { width: 16001, height: 1 }])
    await assert.rejects(h.normalizePhoto(image), /Исходная фотография/);
  assert.equal(h.encodings.length, 0);
});

test("the photo viewer opens the selected thumbnail and escapes the caption", async () => {
  const h = harness();
  await h.click("gallery", { photo: "second" });
  await h.click("open-photo");
  assert.equal(h.dialog.open, true);
  assert.match(h.content.innerHTML, /class="photo-viewer-image" src="\/api\/photos\/second"/);
  assert.match(h.content.innerHTML, /alt="Лампа &quot;&lt;белая&gt;&quot;"/);
  assert.doesNotMatch(h.content.innerHTML, /<белая>/);
  await h.click("close-dialog");
  assert.equal(h.dialog.open, false);
  assert.equal(h.content.innerHTML, "");
});

test("a backdrop click closes the viewer and the next dialog has no photo content", async () => {
  const h = harness();
  await h.click("open-photo");
  h.backdrop(50, 50);
  assert.equal(h.dialog.open, true);
  h.backdrop(0, 0);
  assert.equal(h.dialog.open, false);
  h.modal("Обычное окно", "<p>Текст</p>");
  assert.equal(h.dialog.open, true);
  assert.doesNotMatch(h.content.innerHTML, /photo-viewer-image/);
});

test("listing photos have a keyboard-accessible opener and placeholders do not", async () => {
  const listing = { id: 1, author_id: 2, author_name: "Студент", title: "Лампа", description: "Описание",
    category: "things", kind: "offer", price_mode: "unset", location: "PETRODVORTSOVY",
    status: "active", created_at: "2026-10-21T00:00:00Z", photos: [{ id: "first" }] };
  const h = harness({ respond: () => listing });
  h.state.me = { id: 3, is_admin: false };
  await h.detailView(1, h.state.seq);
  assert.match(h.view.innerHTML, /<button type="button" class="gallery-open" data-action="open-photo"/);
  listing.photos = [];
  await h.detailView(1, h.state.seq);
  assert.doesNotMatch(h.view.innerHTML, /data-action="open-photo"/);
});
