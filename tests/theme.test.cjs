const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync(require('node:path').join(__dirname, '../web/assets/theme.js'), 'utf8');
function harness({dark = false, saved = null, unavailable = false} = {}) {
 const events = {}, windowEvents = {}, attrs = {};
 const button = {textContent: '', setAttribute(key, value) { attrs[key] = value; }};
 const root = {dataset: {}}, meta = {};
 const system = {matches: dark, addEventListener(name, callback) { this.change = callback; }};
 let stored = saved, observer;
 const document = {
  documentElement: root, body: {}, readyState: 'complete',
  querySelector() { return meta; }, querySelectorAll() { return [button]; },
  addEventListener(name, callback) { events[name] = callback; },
 };
 vm.runInNewContext(source, {document, window: {matchMedia() { return system; }, addEventListener(name, callback) { windowEvents[name] = callback; }},
  localStorage: {getItem() { if (unavailable) throw Error('blocked'); return stored; }, setItem(key, value) { if (unavailable) throw Error('blocked'); stored = value; }},
  MutationObserver: class { constructor(callback) { observer = callback; } observe() {} },
 });
 return {root, meta, button, attrs, system, observer,
  click(toggle = true) { events.click({target: {closest() { return toggle ? button : null; }}}); },
  stored() { return stored; }, sync(value) { stored = value; windowEvents.storage({key: 'dormhub.theme'}); },
 };
}
test('system theme applies before rendering and follows system changes', () => {
 const app = harness({dark: true});
 assert.equal(app.root.dataset.theme, 'dark');
 assert.equal(app.meta.content, '#17191d');
 assert.equal(app.attrs['aria-label'], 'Включить светлую тему');
 app.system.matches = false; app.system.change();
 assert.equal(app.root.dataset.theme, 'light');
});
test('manual choice persists and overrides later system changes', () => {
 const app = harness(); app.click();
 assert.equal(app.root.dataset.theme, 'dark'); assert.equal(app.stored(), 'dark');
 app.system.change(); assert.equal(app.root.dataset.theme, 'dark');
 app.click(false); assert.equal(app.root.dataset.theme, 'dark');
 assert.equal(harness({saved: app.stored()}).root.dataset.theme, 'dark');
});
test('blocked storage still permits theme switching', () => {
 const app = harness({unavailable: true}); app.click();
 assert.equal(app.root.dataset.theme, 'dark');
});
test('storage synchronization validates preferences and falls back to system', () => {
 const app = harness(); app.sync('dark'); assert.equal(app.root.dataset.theme, 'dark');
 app.sync('invalid'); assert.equal(app.root.dataset.theme, 'light');
});
test('buttons inserted after navigation reflect the current theme', () => {
 const app = harness({saved: 'dark'}); app.button.textContent = '◐'; app.observer();
 assert.equal(app.button.textContent, '☀'); assert.equal(app.button.title, 'Включить светлую тему');
});
