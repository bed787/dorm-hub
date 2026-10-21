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
  const backdrop = { src: "/api/photos/first" };
  function control() {
    const listeners = {};
    return { value: "", disabled: false, addEventListener(name, fn) { listeners[name] = fn; },
      fire(name, event = {}) { return listeners[name]?.(event); } };
  }
  const controls = { ratio: control(), width: control(), height: control(), save: control(), reset: control() };
  const preview = { ...control(), width: 1200, height: 900, isConnected: true,
    getBoundingClientRect() { return { left: 0, top: 0, width: 600, height: 450 }; },
    setPointerCapture() {},
    getContext() { return { fillRect() {}, drawImage() {}, save() {}, restore() {}, beginPath() {}, rect() {}, clip() {}, strokeRect() {} }; },
  };
  const image = {
    src: "/api/photos/first", alt: 'Лампа "<белая>"',
    getAttribute(name) { return this[name]; },
  };
  const dialog = {
    open: false,
    showModal() { this.open = true; },
    close() { this.open = false; dialogEvents.close?.(); },
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
      const result = encode ? encode(call, encodings.length) : new Blob(["compressed"], { type });
      if (result?.then) result.then(callback); else callback(result);
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
          "#gallery img": image, "#gallery .photo-backdrop": backdrop, "#toast": toast, "#view": view,
          "#crop-canvas": preview, "#crop-ratio": controls.ratio, "#crop-width": controls.width,
          "#crop-height": controls.height, "#crop-save": controls.save, "#crop-reset": controls.reset }[selector] || null;
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
  vm.runInContext(source + "\nglobalThis.photos = {state, queuePhotos, normalizePhoto, saveListing, detailView, modal, card, photoMarkup, crop, cropRect, moveCrop, resizeCrop};", context);
  return { ...context.photos, encodings, revoked, fills, canvas, toast, dialog, content, view, image, background: backdrop, controls, preview,
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
  assert.equal(h.background.src, "/api/photos/second");
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
  assert.match(h.view.innerHTML, /<button type="button" class="gallery-open photo-frame" data-action="open-photo"/);
  listing.photos = [];
  await h.detailView(1, h.state.seq);
  assert.doesNotMatch(h.view.innerHTML, /data-action="open-photo"/);
});

test("a card retains the complete photo with an inaccessible decorative backdrop", () => {
  const h = harness();
  h.state.me = { id: 1 };
  const html = h.card({ id: 2, author_id: 3, author_name: "Студент", title: '<Лампа>',
    category: 'things', kind: 'offer', status: 'active', price_mode: 'unset',
    photos: [{ id: 'first' }], created_at: '2026-10-21T00:00:00Z' });
  assert.match(html, /class="photo-frame"/);
  assert.match(html, /class="photo-foreground" src="\/api\/photos\/first" alt="&lt;Лампа&gt;" loading="lazy"/);
  assert.match(html, /class="photo-backdrop" src="\/api\/photos\/first" alt="" aria-hidden="true" loading="lazy"/);
  assert.ok(html.indexOf('photo-foreground') < html.indexOf('photo-backdrop'));
});

test("crop frames preserve requested ratios and remain inside portrait and landscape photos", () => {
  const h = harness();
  for (const [width, height] of [[1600, 800], [800, 1600], [1200, 1200]]) {
    for (const ratio of [0, 1, 4 / 3, 16 / 9, 3 / 4]) {
      const box = h.cropRect(width, height, ratio);
      assert.ok(box.x >= 0 && box.y >= 0);
      assert.ok(box.x + box.width <= width + 0.00001);
      assert.ok(box.y + box.height <= height + 0.00001);
      if (ratio) assert.ok(Math.abs(box.width / box.height - ratio) < 0.00001);
      else assert.deepEqual({ ...box }, { x: 0, y: 0, width, height });
      for (const corner of ['nw', 'ne', 'sw', 'se']) {
        for (const [dx, dy] of [[10000, 10000], [-10000, -10000], [10000, -10000], [17, -23]]) {
          const resized = h.resizeCrop(box, corner, dx, dy, width, height, ratio);
          assert.ok(resized.width > 0 && resized.height > 0);
          assert.ok(resized.x >= -0.00001 && resized.y >= -0.00001);
          assert.ok(resized.x + resized.width <= width + 0.00001);
          assert.ok(resized.y + resized.height <= height + 0.00001);
          if (ratio) assert.ok(Math.abs(resized.width / resized.height - ratio) < 0.00001);
        }
      }
    }
  }
});

test("moving a crop clamps it to the image edges without changing its dimensions", () => {
  const h = harness(), box = { x: 100, y: 200, width: 400, height: 300 };
  assert.deepEqual({ ...h.moveCrop(box, 5000, 5000, 1200, 900) }, { x: 800, y: 600, width: 400, height: 300 });
  assert.deepEqual({ ...h.moveCrop(box, -5000, -5000, 1200, 900) }, { x: 0, y: 0, width: 400, height: 300 });
});

test("free crop fields export the requested dimensions and release autosave on apply", async () => {
  const h = harness(), previous = new Blob(['original'], { type: 'image/jpeg' });
  h.state.pending.push({ file: previous, url: 'blob:original' });
  await h.crop(0);
  let released = false;
  h.state.photoWork.then(() => { released = true; });
  await new Promise(setImmediate);
  assert.equal(released, false);
  h.controls.width.value = '800'; h.controls.width.fire('change');
  h.controls.height.value = '600'; h.controls.height.fire('change');
  await h.controls.save.fire('click');
  await h.state.photoWork;
  assert.equal(released, true);
  assert.equal(h.encodings[0].width, 800);
  assert.equal(h.encodings[0].height, 600);
  assert.notEqual(h.state.pending[0].file, previous);
  assert.ok(h.revoked.includes('blob:original'));
  assert.equal(h.dialog.open, false);
});

test("crop proportions and reset update both pixel fields", async () => {
  const h = harness(); h.state.pending.push({ file: new Blob(['x']), url: 'blob:original' });
  await h.crop(0);
  h.controls.ratio.value = '1'; h.controls.ratio.fire('change');
  assert.equal(h.controls.width.value, h.controls.height.value);
  h.controls.width.value = '900'; h.controls.width.fire('change');
  assert.equal(h.controls.height.value, 900);
  h.controls.reset.fire('click');
  assert.equal(h.controls.width.value, 4000);
  assert.equal(h.controls.height.value, 3000);
  assert.equal(h.controls.ratio.value, 'free');
  h.dialog.close(); await h.state.photoWork;
});

test("closing the crop leaves the original file unchanged and resumes pending work", async () => {
  const h = harness(), previous = new Blob(['original'], { type: 'image/jpeg' });
  h.state.pending.push({ file: previous, url: 'blob:original' });
  await h.crop(0);
  h.controls.width.value = '300'; h.controls.width.fire('change');
  h.dialog.close(); await h.state.photoWork;
  assert.equal(h.state.pending[0].file, previous);
  assert.equal(h.state.pending[0].url, 'blob:original');
  assert.equal(h.revoked.length, 0);
});

test("a crop finishing after cancellation cannot overwrite the selected file", async () => {
  let resolve;
  const encoded = new Promise(done => { resolve = done; });
  const h = harness({ encode: () => encoded }), previous = new Blob(['original'], { type: 'image/jpeg' });
  h.state.pending.push({ file: previous, url: 'blob:original' });
  await h.crop(0);
  const applying = h.controls.save.fire('click');
  h.dialog.close();
  resolve(new Blob(['cropped'], { type: 'image/jpeg' }));
  await applying; await h.state.photoWork;
  assert.equal(h.state.pending[0].file, previous);
  assert.equal(h.revoked.length, 0);
});

test("adding with cropping waits for the editor before completing photo processing", async () => {
  const h = harness();
  h.queuePhotos([new Blob(['original'], { type: 'image/jpeg' })], true);
  await new Promise(setImmediate);
  assert.equal(h.dialog.open, true);
  let completed = false;
  h.state.photoWork.then(() => { completed = true; });
  await new Promise(setImmediate);
  assert.equal(completed, false);
  h.controls.width.value = '960'; h.controls.width.fire('change');
  h.controls.height.value = '720'; h.controls.height.fire('change');
  await h.controls.save.fire('click');
  await h.state.photoWork;
  assert.equal(completed, true);
  assert.equal(h.state.pending.length, 1);
  assert.equal(h.encodings.at(-1).width, 960);
  assert.equal(h.encodings.at(-1).height, 720);
});

test("dragging the crop corner resizes the frame and dragging its center moves it", async () => {
  const h = harness(); h.state.pending.push({ file: new Blob(['original']), url: 'blob:original' });
  await h.crop(0);
  h.preview.fire('pointerdown', { clientX: 600, clientY: 450, pointerId: 1, button: 0 });
  h.preview.fire('pointermove', { clientX: 300, clientY: 225, pointerId: 1 });
  h.preview.fire('pointerup', { pointerId: 1 });
  assert.equal(h.controls.width.value, 2000);
  assert.equal(h.controls.height.value, 1500);
  h.controls.width.value = '800'; h.controls.width.fire('change');
  h.controls.height.value = '600'; h.controls.height.fire('change');
  // The frame is centered within its previous bounds: x=600,y=450.
  h.preview.fire('pointerdown', { clientX: 150, clientY: 112.5, pointerId: 2, button: 0 });
  h.preview.fire('pointermove', { clientX: 900, clientY: 900, pointerId: 2 });
  h.preview.fire('pointercancel', { pointerId: 2 });
  assert.equal(h.controls.width.value, 800);
  assert.equal(h.controls.height.value, 600);
  h.dialog.close(); await h.state.photoWork;
});
