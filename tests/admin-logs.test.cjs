const test = require('node:test');
const assert = require('node:assert/strict');
const {rows, create} = require('../web/assets/admin-logs.js');
const flush = () => new Promise(resolve => setImmediate(resolve));
function element() {
  const events = new Map();
  return {innerHTML: '', textContent: '', disabled: false,
    addEventListener(name, callback) { events.set(name, callback); },
    removeEventListener(name) { events.delete(name); },
    emit(name, event) { events.get(name)?.(event); },
  };
}
function page(message) {
  return {items: [{id: 1, time: '2026-10-17T10:00:00Z', level: 'ERROR', message, details: '{}'}], retained: 1, matched: 1, capacity: 500};
}
function harness() {
  const form = element(), output = element(), status = element(), refresh = element();
  form.elements = {auto: {checked: true}, level: {value: ''}, q: {value: ''}};
  const elements = {'[data-log-filters]': form, '[data-log-list]': output, '[data-log-state]': status, '[data-log-refresh]': refresh};
  const requests = [], timers = new Map(); let id = 0;
  const viewer = create({container: {querySelector: selector => elements[selector]}, active: () => true,
    api(path) { return new Promise((resolve, reject) => requests.push({path, resolve, reject})); },
    later(callback) { const key = ++id; timers.set(key, callback); return key; },
    cancel(key) { timers.delete(key); },
  });
  return {viewer, form, output, status, refresh, requests, timers};
}
test('log messages and details remain text, including malicious HTML', () => {
  const html = rows([{id: 1, time: 'invalid" onmouseover="alert(1)', level: '<svg onload=alert(1)>', message: '<img src=x onerror=alert(1)>', details: '</pre><script>alert(1)</script>'}]);
  assert.doesNotMatch(html, /<img|<script|<svg/);
  assert.match(html, /&lt;img/);
  assert.match(html, /&lt;\/pre&gt;/);
});
test('changed filters cannot be overwritten by an old response; navigation cancels updates', async () => {
  const h = harness();
  h.form.elements.q.value = 'new & request';
  h.form.emit('input', {target: h.form.elements.q});
  assert.equal(h.requests.length, 1, 'overlapping requests');
  h.requests[0].resolve(page('old results')); await flush();
  assert.doesNotMatch(h.output.innerHTML, /old results/);
  assert.equal(h.requests.length, 2);
  assert.match(h.requests[1].path, /q=new\+%26\+request/);
  h.requests[1].resolve(page('new results')); await flush();
  assert.match(h.output.innerHTML, /new results/);
  const timer = [...h.timers.values()][0]; timer();
  assert.equal(h.requests.length, 3);
  h.viewer.stop();
  h.requests[2].resolve(page('after logout')); await flush();
  assert.doesNotMatch(h.output.innerHTML, /after logout/);
  assert.equal(h.timers.size, 0);
});
test('lost administrator access clears log content and stops automatic requests', async () => {
  const h = harness();
  h.output.innerHTML = 'previous private logs';
  const error = new Error('Недостаточно прав'); error.status = 403;
  h.requests[0].reject(error); await flush();
  assert.equal(h.output.innerHTML, '');
  assert.equal(h.form.elements.auto.checked, false);
  assert.equal(h.timers.size, 0);
  assert.match(h.status.textContent, /Недостаточно прав/);
  h.viewer.stop();
});
