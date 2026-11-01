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
  const background = {};
  const photoList = {innerHTML: "", closest() { return null; }};
  const form = {dataset: {}, entries: fields(), querySelectorAll() { return []; }};
  class EditorData extends FormData { constructor(f) { super(); if (f) for (const [key,value] of f.entries) this.append(key,value); } }
  const image = {
    src: "/api/photos/first", alt: 'Лампа "<белая>"',
    getAttribute(name) { return this[name]; },
  };
  const dialog = {
    open: false,
    showModal() { this.open = true; },
    close() { this.open = false; dialogEvents.close?.(); },
    removeEventListener(name, fn) { if (dialogEvents[name] === fn) delete dialogEvents[name]; },
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
      Promise.resolve(encode ? encode(call, encodings.length) : new Blob(["compressed"], { type })).then(callback);
    },
  };
  function control(value = "") {
    const listeners = {};
    return {value, disabled:false, isConnected:true, addEventListener(name,fn) { listeners[name] = fn; }, fire(name) { return listeners[name]?.({target:this}); }};
  }
  const controls = Object.fromEntries(["ratio","width","height","save","reset"].map(name => ["#crop-"+name,control(name === "ratio" ? "0" : "")]));
  const pointerEvents = {};
  const preview = {width:1200,height:900,isConnected:true,
    getContext() { return {fillRect() {},drawImage() {},strokeRect() {}}; },
    getBoundingClientRect() { return {left:0,top:0,width:600,height:450}; },
    setPointerCapture() {}, addEventListener(name,fn) { pointerEvents[name] = fn; },
    fire(name,x,y) { pointerEvents[name]?.({clientX:x,clientY:y,pointerId:1,preventDefault() {}}); },
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
    Blob, FormData: EditorData, URL: PhotoURLs, URLSearchParams, Image,
    document: {
      getElementById(id) { return id === "brand-mark" ? { innerHTML: '<svg class="brand-mark"></svg>' } : null; },
      querySelector(selector) {
        return { "#app": {}, "#dialog": dialog, "#dialog-content": content,
          "#gallery img": image, "#gallery .photo-backdrop": background, "#toast": toast, "#view": view, "#photo-list": photoList, "#listing-form": form, "#crop-canvas": preview, ...controls }[selector] || null;
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
  vm.runInContext(source + "\nglobalThis.photos = {state, queuePhotos, normalizePhoto, saveListing, detailView, modal, card, photoMarkup, renderPhotos, formView, crop, cropRect, moveCrop, resizeCrop};", context);
  return { ...context.photos, encodings, revoked, fills, canvas, toast, dialog, content, view, image, background, photoList, form, controls, preview,
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
  assert.match(h.view.innerHTML, /<button type="button" class="gallery-open photo-frame" data-action="open-photo"/);
  listing.photos = [];
  await h.detailView(1, h.state.seq);
  assert.doesNotMatch(h.view.innerHTML, /data-action="open-photo"/);
});

function listing(overrides = {}) {
  return {id:1, author_id:2, author_name:'Студент', title:'Лампа', description:'Лампа: https://example.org/test.',
    category:'things',kind:'offer',price_mode:'unset',location:'EXTERNAL',address:'Улица, 12',
    status:'active',created_at:'2026-10-21T00:00:00Z',photos:[{id:'saved',position:0}], ...overrides};
}
test('compact public cards omit redundant labels while moderation keeps status and kind', () => {
  const h = harness(); h.state.me = {id:3};
  const html = h.card(listing());
  assert.match(html, /listing-card compact-card/);
  assert.doesNotMatch(html, /Предложение|Заявка|card-description/);
  assert.doesNotMatch(html.split('class="card-content"')[0], /Предложение|Заявка/);
  assert.doesNotMatch(html, /Опубликовано/);
  assert.match(html, /Улица, 12/);
  assert.match(h.card(listing(),'mine'), /Опубликовано/);
  assert.match(h.card(listing({kind:'request', status:'pending'}),'moderation'), /Заявка/);
  assert.match(h.card(listing({status:'pending'}),'moderation'), /На проверке/);
});
test('detail safely links description URLs and displays an optional external address', async () => {
  const h = harness({respond: () => listing({address:'<img onerror=bad>'})}); h.state.me = {id:3};
  await h.detailView(1,h.state.seq);
  assert.match(h.view.innerHTML, /href="https:\/\/example.org\/test" target="_blank" rel="noopener noreferrer"/);
  assert.match(h.view.innerHTML, /&lt;img onerror=bad&gt;/);
  assert.doesNotMatch(h.view.innerHTML, /<img onerror/);
});
test('new and saved thumbnails both expose crop buttons directly on their photo', () => {
  const h = harness(); h.state.formListing = listing();
  h.state.pending.push({file:new Blob(['new']),url:'blob:new'});
  h.renderPhotos();
  assert.match(h.photoList.innerHTML, /class="photo-crop-button" data-action="crop" data-photo="saved"/);
  assert.match(h.photoList.innerHTML, /class="photo-crop-button" data-action="crop" data-index="0"/);
  assert.equal((h.photoList.innerHTML.match(/class="photo-preview photo-frame"/g) || []).length,2);
});
test('new listing has one multiple-file picker and includes the external address field', async () => {
  const h = harness(); h.state.me = {id:2,telegram:'',vk:''};
  h.state.catalog = {locations:[{code:'EXTERNAL',name:'Не УНК университета'}],categories:[],dorms:[]};
  await h.formView(null,h.state.seq);
  assert.equal((h.view.innerHTML.match(/type="file"/g) || []).length,1);
  assert.match(h.view.innerHTML, /id="photo-input"[^>]+multiple/);
  assert.match(h.view.innerHTML, /value="EXTERNAL"/);
  assert.match(h.view.innerHTML, /name="address" maxlength="300"/);
});
test('crop geometry stays inside portrait and landscape images with any aspect ratio', () => {
  const h = harness();
  for (const [w,hh] of [[1920,1080],[800,1600],[31,17]]) for (const ratio of [0,1,4/3,16/9,3/4]) {
    const box = h.cropRect(w,hh,ratio);
    for (const corner of ['nw','ne','sw','se']) for (const [dx,dy] of [[-1e5,-1e5],[1e5,1e5],[-1e5,1e5],[33,-55]]) {
      const b = h.resizeCrop(box,corner,dx,dy,w,hh,ratio);
      assert.ok(b.x>=-1e-8 && b.y>=-1e-8 && b.width>0 && b.height>0);
      assert.ok(b.x+b.width<=w+1e-8 && b.y+b.height<=hh+1e-8);
      if (ratio) assert.ok(Math.abs(b.width/b.height-ratio)<1e-8);
    }
  }
});
test('cropping a saved photo creates one replacement, retains its position and gates autosave', async () => {
  const h = harness({width:1600,height:1200}); h.state.formListing = listing();
  const editing = await h.crop(null,'saved'); let released = false;
  h.state.photoWork.then(() => {released=true;}); await Promise.resolve(); assert.equal(released,false);
  h.controls['#crop-width'].value='800'; h.controls['#crop-width'].fire('change');
  h.controls['#crop-height'].value='600'; h.controls['#crop-height'].fire('change');
  await h.controls['#crop-save'].fire('click'); await editing.closed;
  assert.equal(h.state.pending.length,1); assert.equal(h.state.pending[0].replaceID,'saved');
  assert.equal(h.encodings[0].width,800); assert.equal(h.encodings[0].height,600);
  assert.equal(h.state.formListing.photos.length,1);
  assert.equal((h.photoList.innerHTML.match(/class="photo-item"/g)||[]).length,1);
  assert.equal(h.state.cropSession,null); assert.equal(h.dialog.open,false);
});
test('saved crop retries the same PUT after a lost response without appending a photo', async () => {
  const keys = []; let attempts = 0;
  const h = harness({respond: (url,options) => {
    if (url === '/api/listings/1') return listing({revision:3});
    assert.equal(url,'/api/listings/1/photos/saved'); assert.equal(options.method,'PUT');
    keys.push(options.headers['Idempotency-Key']);
    if (++attempts === 1) throw new Error('Connection lost');
    return {id:'cropped',position:0,width:800,height:600};
  }});
  h.state.formListing = listing({revision:1});
  h.state.pending.push({file:new Blob(['crop'],{type:'image/jpeg'}),url:'blob:crop',replaceID:'saved'});
  await assert.rejects(h.saveListing({},fields(),'autosave'), /Connection lost/);
  await h.saveListing({},fields(),'autosave');
  assert.equal(keys.length,2); assert.equal(keys[0],keys[1]);
  // The test responder returns the current listing on the final refresh.
  assert.equal(h.state.pending.length,0);
});
test('cancelling crop preserves the source and a late encoder cannot change it', async () => {
  let complete;
  const h = harness({encode: () => new Promise(resolve => {complete=resolve;})});
  const item = {file:new Blob(['original']),url:'blob:original'};h.state.pending.push(item);
  const editing=await h.crop(0);
  const applying = h.controls['#crop-save'].fire('click');
  await h.click('close-dialog'); await editing.closed;
  complete(new Blob(['cropped'],{type:'image/jpeg'})); await applying;
  assert.equal(item.url,'blob:original'); assert.equal(await item.file.text(),'original');
  assert.equal(h.state.cropSession,null);
});
test('a crop click during upload follows that same photo after autosave finishes', async () => {
  let finish;
  const h=harness();const item={file:new Blob(['original']),url:'blob:original'};
  h.state.pending.push(item);h.state.formListing=listing();
  h.state.editor={work:new Promise(resolve=>{finish=resolve;})};
  const opening=h.crop(0);
  h.state.pending.shift();item.savedID='saved';finish();
  await opening;
  assert.equal(h.dialog.open,true);await h.click('close-dialog');
});
test('gallery switches both the main photo and decorative background', async () => {
  const h=harness();await h.click('gallery',{photo:'second'});
  assert.equal(h.image.src,'/api/photos/second'); assert.equal(h.background.src,h.image.src);
});

test('external address is restored in the editor and saved without a dorm selection', async () => {
  const current=listing({revision:1}); const sent=[];
  const h=harness({respond:(url,options) => {
    if (options.method==='PUT') sent.push(JSON.parse(options.body));
    return current;
  }});
  h.state.me={id:2}; h.state.catalog={locations:[{code:'EXTERNAL',name:'Не УНК университета'}],categories:[],dorms:[]};
  await h.formView(1,h.state.seq);
  assert.match(h.view.innerHTML,/name="address" maxlength="300" value="Улица, 12"/);
  const data=fields();data.set('location','EXTERNAL');data.set('address','Другой адрес');data.set('dorm_id','5');
  await h.saveListing({},data,'autosave');
  assert.equal(sent[0].address,'Другой адрес');assert.equal(sent[0].dorm_id,null);
  data.set('location','NEVSKY');await h.saveListing({},data,'autosave');
  assert.equal(sent[1].address,'');
});

test('removing a pending file during upload cannot shift away the next photo', async () => {
  let complete, entered;
  const uploading=new Promise(resolve=>{entered=resolve;});
  const first={file:new Blob(['first']),url:'blob:first'}, second={file:new Blob(['second']),url:'blob:second'};
  const h=harness({respond:async(url,options)=>{
    if (url==='/api/listings') return {id:12,photos:[],status:'archived'};
    const contents=await options.body.get('file').text();
    if (contents==='first') {entered();await new Promise(resolve=>{complete=resolve;});}
    return {id:contents,position:contents==='first'?0:1};
  }});
  h.state.pending.push(first,second);
  const saving=h.saveListing({},fields(),'autosave');await uploading;
  await h.click('remove-pending',{index:'0'});
  assert.equal(h.state.pending.length,2);assert.equal(h.state.pending[0],first);
  complete();await saving;
  assert.equal(h.state.formListing.photos.length,2);
  assert.deepEqual(Array.from(h.state.formListing.photos,p=>p.id),['first','second']);
});
