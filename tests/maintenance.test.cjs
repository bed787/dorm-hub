const test = require('node:test');
const assert = require('node:assert/strict');
const {createClient} = require('../web/assets/requests.js');

test('maintenance response identifies closure and notifies the open application once', async () => {
  let notices = 0, calls = 0;
  const {api} = createClient({}, async () => {
    calls++;
    return {ok: false, status: 503, json: async () => ({error: {code: 'maintenance', message: 'Технические работы'}})};
  }, {onMaintenance() {notices++;}});
  await assert.rejects(api('/listings', {method: 'POST', body: {title: 'test'}, guest: true}), error => error.status === 503 && error.code === 'maintenance');
  assert.equal(notices, 1);
  assert.equal(calls, 1, 'maintenance must not trigger a login-token retry or repeat the write');
});

test('an unrelated server outage does not put the application in maintenance mode', async () => {
  let notices = 0;
  const {api} = createClient({}, async () => ({ok: false, status: 503, json: async () => ({error: {code: 'unavailable'}})}), {onMaintenance() {notices++;}});
  await assert.rejects(api('/listings'));
  assert.equal(notices, 0);
});
