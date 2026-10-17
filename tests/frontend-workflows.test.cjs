const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');
const utils = require('../web/assets/utils.js');

// Execute the actual application script with a small DOM adapter. These tests
// cover asynchronous state transitions, not layout or browser compatibility.
function appHarness(responder) {
  const listeners = {};const changeListeners=[];
  const revoked = [];
  const toast = {classList: {toggle() {}}, hidden: true};
  const app = {innerHTML: ''}, view = {innerHTML: ''};
  const dialog = {close() {}, addEventListener() {}};
  const location = {hash: '#/new'};
  let nextURL = 0;
  class ObjectURLs extends URL {
    static createObjectURL() { return 'blob:photo-' + ++nextURL; }
    static revokeObjectURL(url) { revoked.push(url); }
  }
  const context = vm.createContext({
    DormHubUtils: utils, DormHubRequests: require('../web/assets/requests.js'), Blob, FormData, URL: ObjectURLs, URLSearchParams,
    document: {
      getElementById(id) { return id === "brand-mark" ? {innerHTML: '<svg class="brand-mark"></svg>'} : null; },
      querySelector(selector) {
        return {'#app': app, '#dialog': dialog, '#toast': toast, '#view': view}[selector] || null;
      },
      querySelectorAll() { return []; },
      addEventListener(name, callback) { if(name==='change'){changeListeners.push(callback);listeners.change=event=>changeListeners.forEach(fn=>fn(event));}else listeners[name] = callback; },
    },
    window: {addEventListener() {}}, location,
    setInterval() {}, setTimeout() {}, clearTimeout() {},
    fetch(url, options) {
      // Suppress the startup request while tests drive individual workflows.
      if (url === '/api/me') return new Promise(() => {});
      return Promise.resolve().then(() => responder(url, options));
    },
  });
  const source = fs.readFileSync(path.join(__dirname, '../web/assets/app.js'), 'utf8');
  vm.runInContext(source + '\n globalThis.workflows = {state, saveListing, queuePhotos, releasePending, profileView, draftHasContent, statusBadge, changeSavedPhoto, captureEditor, enterApp, authPath, loginView};', context);
  return {...context.workflows, context, location, revoked, listeners, view, app};
}
function response(data) {
  return {ok: true, json: async () => ({data})};
}
function failure(message) {
  return {ok: false, status: 500, json: async () => ({error: {message}})};
}
function deferred() {
  let resolve;
  const promise = new Promise(done => { resolve = done; });
  return {promise, resolve};
}
function fields() {
  return new Map(Object.entries({
    kind: 'offer', category: 'things', title: 'Настольная лампа',
    description: 'Рабочая лампа для комнаты.', location: 'PETRODVORTSOVY',
    dorm_id: '', price_mode: 'unset', telegram: 'https://t.me/student', vk: '',
  }));
}
function photo(name) {
  return {file: new Blob([name], {type: 'image/jpeg'}), url: 'blob:' + name};
}

test('navigation during upload keeps photos attached to the original listing', async () => {
  const entered = deferred(), upload = deferred(), calls = [];
  const harness = appHarness(async (url, options) => {
    calls.push(url);
    if (url === '/api/listings'||url.endsWith('/status')) return response({id: 10, photos: []});
    if (calls.filter(x => x.endsWith('/photos')).length === 1) {
      entered.resolve();
      await upload.promise;
    }
    assert.equal(url, '/api/listings/10/photos');
    return response({id: 'saved-' + calls.length});
  });
  harness.state.pending.push(photo('one'), photo('two'));
  const saving = harness.saveListing({}, fields());
  await entered.promise;
  const newListing = {id: 20, photos: []};
  harness.state.seq++;
  harness.releasePending();
  harness.state.formListing = newListing;
  harness.state.pending.push(photo('new-editor'));
  harness.location.hash = '#/edit/20';
  upload.resolve();
  await saving;
  assert.deepEqual(calls, ['/api/listings', '/api/listings/10/photos', '/api/listings/10/photos', '/api/listings/10/status']);
  assert.equal(harness.state.formListing, newListing);
  assert.equal(newListing.photos.length, 0);
  assert.equal(harness.state.pending.length, 1);
  assert.equal(harness.state.pending[0].url, 'blob:new-editor');
  assert.equal(harness.location.hash, '#/edit/20');
});

test('retry after a photo failure updates the saved listing and sends only remaining photos', async () => {
  const calls = [], sent = [];
  let fail = true;
  const harness = appHarness(async (url, options) => {
    calls.push({url, method: options.method});
    if (!url.endsWith('/photos')) return response({id: 10, photos: sent.map((_, i) => ({id: String(i)}))});
    const text = await options.body.get('file').text();
    if (text === 'two' && fail) { fail = false; return failure('Сбой загрузки'); }
    sent.push(text);
    return response({id: text});
  });
  harness.state.pending.push(photo('one'), photo('two'));
  await assert.rejects(harness.saveListing({}, fields()), /Объявление №10 сохранено/);
  assert.equal(harness.state.formListing.id, 10);
  assert.equal(harness.state.pending.length, 1);
  await harness.saveListing({}, fields());
  assert.deepEqual(calls.filter(x => !x.url.endsWith('/photos')), [
    {url: '/api/listings', method: 'POST'},
    {url: '/api/listings/10', method: 'GET'},
    {url: '/api/listings/10', method: 'PUT'},
    {url: '/api/listings/10/status', method: 'POST'},
  ]);
  assert.deepEqual(sent, ['one', 'two']);
  assert.equal(harness.state.pending.length, 0);
  assert.equal(harness.location.hash, '/listing/10');
});

test('submit waits for selected images to finish processing', async () => {
  const entered = deferred(), processing = deferred(), sent = [];
  const harness = appHarness(async (url, options) => {
    sent.push(url);
    if (url === '/api/listings'||url.endsWith('/status')) return response({id: 10, photos: []});
    assert.equal(await options.body.get('file').text(), 'normalized');
    return response({id: 'photo'});
  });
  harness.context.imageReady = async () => { entered.resolve(); return {width: 100, height: 60}; };
  harness.context.normalizeReady = () => processing.promise;
  vm.runInContext('imageFrom = imageReady; normalizePhoto = normalizeReady;', harness.context);
  const selection = [{type: 'image/jpeg', size: 1}];
  harness.queuePhotos(selection);
  selection.length = 0; // Clearing a file input must not discard its selection.
  await entered.promise;
  const saving = harness.saveListing({}, fields());
  await new Promise(setImmediate);
  assert.equal(sent.length, 0);
  processing.resolve(new Blob(['normalized'], {type: 'image/jpeg'}));
  await saving;
  assert.deepEqual(sent, ['/api/listings', '/api/listings/10/photos', '/api/listings/10/status']);
});

test('late image processing cannot insert photos into another editor', async () => {
  const entered = deferred(), processing = deferred();
  const harness = appHarness(() => { throw new Error('Unexpected HTTP request'); });
  harness.context.imageReady = async () => { entered.resolve(); return {width: 100, height: 60}; };
  harness.context.normalizeReady = () => processing.promise;
  vm.runInContext('imageFrom = imageReady; normalizePhoto = normalizeReady;', harness.context);
  harness.queuePhotos([{type: 'image/jpeg', size: 1}]);
  await entered.promise;
  harness.state.seq++;
  harness.releasePending();
  processing.resolve(new Blob(['old-photo'], {type: 'image/jpeg'}));
  await harness.state.photoWork;
  assert.equal(harness.state.pending.length, 0);
  assert.deepEqual(harness.revoked, ['blob:photo-1']);
});

test('overlapping photo selections share the ten-photo limit', async () => {
  const harness = appHarness(() => { throw new Error('Unexpected HTTP request'); });
  harness.state.formListing = {id: 10, photos: Array.from({length: 9}, (_, i) => ({id: String(i)}))};
  harness.context.imageReady = async () => ({width: 100, height: 60});
  harness.context.normalizeReady = async () => new Blob(['photo'], {type: 'image/jpeg'});
  vm.runInContext('imageFrom = imageReady; normalizePhoto = normalizeReady;', harness.context);
  harness.queuePhotos([{type: 'image/jpeg', size: 1}]);
  harness.queuePhotos([{type: 'image/jpeg', size: 1}]);
  await harness.state.photoWork;
  assert.equal(harness.state.pending.length, 1);
});

test('district changes reset and restrict the dormitory selector', () => {
  const harness = appHarness(() => { throw new Error('Unexpected HTTP request'); });
  harness.state.catalog = {dorms: [
    {id: 1, location: 'VASILEOSTROVSKY', name: '№ 1 — ул. Кораблестроителей, 20'},
    {id: 2, location: 'NEVSKY', name: '№ 6 — пр. Солидарности, 27/1'},
  ]};
  const dorm = {innerHTML: '', disabled: true};
  const form = {id: 'listing-form', dataset: {}, querySelector: () => dorm};
  const input = {name: 'location', value: 'NEVSKY', closest: (selector) => selector==='#listing-form'?null:form};
  harness.listeners.change({target: input});
  assert.equal(dorm.disabled, false);
  assert.match(dorm.innerHTML, /№ 6/);
  assert.doesNotMatch(dorm.innerHTML, /№ 1/);
  assert.match(dorm.innerHTML, /value=""/);
  input.value = '';
  harness.listeners.change({target: input});
  assert.equal(dorm.disabled, true);
  assert.match(dorm.innerHTML, /Сначала выберите район/);
  assert.doesNotMatch(dorm.innerHTML, /№ 6/);
});

test('a lost create response retries the same operation without another listing', async () => {
  const records = new Map(), keys = [];
  let loseResponse = true;
  const harness = appHarness((url, options) => {
    if(url.endsWith('/status'))return response({id:1,photos:[]});
    assert.equal(url, '/api/listings');
    const key = options.headers['Idempotency-Key'];
    assert.match(key, /^[a-f0-9]{64}$/);
    keys.push(key);
    if (!records.has(key)) records.set(key, {id: records.size + 1, photos: []});
    if (loseResponse) { loseResponse = false; throw new Error('Ответ потерялся после сохранения'); }
    return response(records.get(key));
  });
  await assert.rejects(harness.saveListing({}, fields()), /Ответ потерялся/);
  await harness.saveListing({}, fields());
  assert.equal(records.size, 1);
  assert.equal(keys[0], keys[1]);
  assert.equal(harness.location.hash, '/listing/1');
});

test('edits after a lost create response update the recovered listing', async () => {
  const calls = [];
  let lost = true;
  const harness = appHarness((url, options) => {
    calls.push({url, options});
    if (lost) { lost = false; throw new Error('Lost response'); }
    return response({id: 10, photos: []});
  });
  await assert.rejects(harness.saveListing({}, fields()), /Lost response/);
  const edited = fields(); edited.set('title', 'Другое название');
  await harness.saveListing({}, edited);
  assert.deepEqual(calls.map(call => [call.url, call.options.method]), [
    ['/api/listings', 'POST'], ['/api/listings', 'POST'], ['/api/listings/10', 'PUT'], ['/api/listings/10/status','POST'],
  ]);
  assert.equal(calls[0].options.headers['Idempotency-Key'], calls[1].options.headers['Idempotency-Key']);
  assert.equal(JSON.parse(calls[1].options.body).title, 'Настольная лампа');
  assert.equal(JSON.parse(calls[2].options.body).title, 'Другое название');
});

test('a lost photo response retries its key and does not duplicate the returned photo', async () => {
  const records = new Map(), keys = [];
  let lost = true;
  const harness = appHarness((url, options) => {
    if (!url.endsWith('/photos')) return response({id: 10, photos: [...records.values()]});
    const key = options.headers['Idempotency-Key']; keys.push(key);
    if (!records.has(key)) records.set(key, {id: 'saved-photo'});
    if (lost) { lost = false; throw new Error('Lost photo response'); }
    return response(records.get(key));
  });
  harness.state.pending.push(photo('one'));
  await assert.rejects(harness.saveListing({}, fields()), /Lost photo response/);
  await harness.saveListing({}, fields());
  assert.equal(records.size, 1);
  assert.equal(keys[0], keys[1]);
  assert.equal(harness.state.formListing.photos.length, 1);
  assert.equal(harness.state.pending.length, 0);
});

test('navigation while image processing waits cannot borrow another editor retry key', async () => {
  const processing = deferred(), calls = [];
  const harness = appHarness((url, options) => {
    calls.push(options.headers['Idempotency-Key']);
    return response({id: 10, photos: []});
  });
  harness.state.photoWork = processing.promise;
  const saving = harness.saveListing({}, fields());
  const original = harness.state.createAttempt;
  harness.state.seq++;
  const nextAttempt = {key: 'f'.repeat(64), input: {title: 'Another editor'}};
  harness.state.createAttempt = nextAttempt;
  processing.resolve();
  await saving;
  assert.equal(calls[0], original.key);
  assert.equal(harness.state.createAttempt, nextAttempt);
});

test('moderator profile view requests the private author scope and shows the blocked state', async () => {
  const harness = appHarness((url) => {
    if (url.startsWith('/api/users/')) return response({id: 2, nickname: 'Студент_2', created_at: '2026-10-08T00:00:00Z', stars: 0, blocked: true, active: true});
    const query = new URLSearchParams(url.split('?')[1]);
    assert.equal(query.get('scope'), 'admin_author');
    assert.equal(query.get('author_id'), '2');
    return response({items: [], total: 0, limit: 24, offset: 0});
  });
  harness.state.me = {id: 1, is_admin: true};
  await harness.profileView('2', new URLSearchParams(), harness.state.seq);
  assert.match(harness.view.innerHTML, /Пользователь заблокирован/);
});

test('student profile view keeps the public feed scope', async () => {
  const harness = appHarness((url) => {
    if (url.startsWith('/api/users/')) return response({id: 2, nickname: 'Студент_2', created_at: '2026-10-08T00:00:00Z', stars: 0});
    const query = new URLSearchParams(url.split('?')[1]);
    assert.equal(query.get('scope'), 'feed');
    return response({items: [], total: 0, limit: 24, offset: 0});
  });
  harness.state.me = {id: 1, is_admin: false};
  await harness.profileView('2', new URLSearchParams(), harness.state.seq);
  assert.doesNotMatch(harness.view.innerHTML, /Пользователь заблокирован/);
});


test('blank profile contacts do not prevent submitting listing contacts',async()=>{
 const calls=[];
 const h=appHarness((url,options)=>{calls.push({url,body:JSON.parse(options.body)});return response({id:7,photos:[],status:url.endsWith('/status')?'pending':'archived'});});
 h.state.me={id:1,telegram:'',vk:''};
 await h.saveListing({},fields());
 assert.equal(calls[0].body.telegram,'https://t.me/student');
 assert.equal(calls[0].body.draft,true);
 assert.equal(calls.at(-1).body.action,'submit');
});
test('incomplete draft is saved without submitting for moderation',async()=>{
 const calls=[];
 const h=appHarness((url,options)=>{calls.push({url,body:JSON.parse(options.body)});return response({id:7,photos:[],status:'archived'});});
 const data=fields();data.set('title','Л');data.set('description','');data.set('category','');data.set('location','');data.set('telegram','');
 await h.saveListing({},data,'draft');
 assert.equal(calls.length,1);assert.equal(calls[0].body.title,'Л');assert.equal(calls[0].body.telegram,'');
});
test('completed edit remains completed without a submission request',async()=>{
 const calls=[];
 const h=appHarness((url,options)=>{calls.push(url);return response({id:7,photos:[],status:'completed'});});
 h.state.formListing={id:7,photos:[],status:'completed'};
 await h.saveListing({},fields());
 assert.deepEqual(calls,['/api/listings/7']);
});
test('empty new form has no draft content',()=>{
 const h=appHarness(()=>{});
 assert.equal(h.draftHasContent({title:'',description:'',category:'',location:'',price_minor:null}),false);
 assert.equal(h.draftHasContent({title:'Л',description:'',category:'',location:'',price_minor:null}),true);
});


test('changing a cover waits for autosave and updates the current revision', async () => {
 const gate=deferred(),calls=[];
 const harness=appHarness((url,options)=>{calls.push({url,body:JSON.parse(options.body)});return response(true);});
 harness.state.formListing={id:12,revision:7,status:'active',photos:[{id:'one',position:0},{id:'two',position:1}]};
 harness.state.editor={work:gate.promise};
 const changing=harness.changeSavedPhoto('cover','two');
 await new Promise(setImmediate);
 assert.equal(calls.length,0,'photo edit overtook autosave');
 harness.state.formListing={...harness.state.formListing,revision:9};
 gate.resolve();await changing;
 assert.equal(harness.state.formListing.revision,10);
 assert.equal(harness.state.formListing.status,'pending');
 assert.deepEqual(Array.from(harness.state.formListing.photos,p=>p.id),['two','one']);
 assert.deepEqual(Array.from(harness.state.formListing.photos,p=>p.position),[0,1]);
 assert.deepEqual(calls,[{url:'/api/listings/12/photos',body:{ids:['two','one']}}]);
});

test('deleting a photo advances the revision used for the next save', async () => {
 let updateBody;
 const harness=appHarness((url,options)=>{
  if(options.method==='DELETE')return response(true);
  if(options.method==='PUT'){updateBody=JSON.parse(options.body);return response({id:12,revision:6,status:'archived',photos:[]});}
  return response({id:12,revision:6,status:'archived',photos:[]});
 });
 harness.state.formListing={id:12,revision:4,status:'archived',photos:[{id:'one',position:0}]};
 await harness.changeSavedPhoto('delete-photo','one');
 assert.equal(harness.state.formListing.revision,5);
 assert.equal(harness.state.formListing.photos.length,0);
 await harness.saveListing({},fields(),'draft');
 assert.equal(updateBody.revision,5,'next save used the obsolete revision');
});

test('disabled form controls do not overwrite recovery data while saving',()=>{
 const harness=appHarness(()=>{throw new Error('unexpected request');});
 const input={title:'Сохранённый текст'};
 harness.state.editor={form:{dataset:{busy:'1'}},input,dirty:2};
 harness.captureEditor();
 assert.equal(harness.state.editor.input,input);
 assert.equal(harness.state.editor.dirty,2);
});

test('login keeps the requested listing and administrator entry chooses moderation',()=>{
 const harness=appHarness(()=>{throw new Error('unexpected request');});
 let destination;
 harness.context.history={replaceState(_state,_title,url){destination=url;}};
 harness.location.hash='#/listing/123';harness.enterApp();
 assert.equal(destination,'/app#/listing/123');
 harness.location.hash='';harness.state.adminLogin=true;harness.state.me={is_admin:true};harness.enterApp();
 assert.equal(destination,'/app#/admin?tab=site');
 assert.equal(harness.authPath('request'),'/auth/admin/request');
 harness.state.adminLogin=false;
 assert.equal(harness.authPath('request'),'/auth/request');
});


test('administrator login omits public registration checks and copy', () => {
 const harness = appHarness(() => response({}));
 harness.state.adminLogin = true;
 harness.loginView();
 assert.match(harness.app.innerHTML, /Вход администратора/);
 assert.doesNotMatch(harness.app.innerHTML, /name="(?:consent|rules)"|login-note|login-legal|brand\.png/);
 assert.match(harness.app.innerHTML, /<svg class="brand-mark"/);
 harness.state.adminLogin = false;
 harness.loginView();
 assert.match(harness.app.innerHTML, /name="consent" required/);
 assert.match(harness.app.innerHTML, /name="rules" required/);
});
