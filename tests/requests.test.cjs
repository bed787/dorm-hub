const test = require('node:test');
const assert = require('node:assert/strict');
const {createClient, requestKey} = require('../web/assets/requests.js');
const ok = data => ({ok: true, json: async () => ({data})});
const csrfError = () => ({ok: false, status: 403, json: async () => ({error: {code: 'csrf', message: 'Обновите страницу входа'}})});

test('request keys are random and meet the server format', () => {
  const keys = Array.from({length: 100}, requestKey);
  assert.equal(new Set(keys).size, 100);
  keys.forEach(key => assert.match(key, /^[a-f0-9]{64}$/));
});

test('guest CSRF recovery retries the original JSON once with the current cookie token', async () => {
  const state = {guestCSRF: 'old'}, calls = [];
  const {api} = createClient(state, async (url, options) => {
    calls.push({url, options});
    if (url === '/api/auth/bootstrap') return ok({csrf: 'current'});
    if (options.headers['X-CSRF-Token'] === 'old') return csrfError();
    assert.deepEqual(JSON.parse(options.body), {email: 'student@students.example'});
    return ok({sent: true});
  });
  await api('/auth/request', {method: 'POST', body: {email: 'student@students.example'}, guest: true});
  assert.deepEqual(calls.map(call => call.url), ['/api/auth/request', '/api/auth/bootstrap', '/api/auth/request']);
  assert.equal(state.guestCSRF, 'current');
});

test('a persistent guest CSRF error stops after one recovery attempt', async () => {
  let posts = 0;
  const {api} = createClient({}, async url => {
    if (url === '/api/auth/bootstrap') return ok({csrf: 'current'});
    posts++;
    return csrfError();
  });
  await assert.rejects(api('/auth/verify', {method: 'POST', body: {}, guest: true}), /Обновите страницу/);
  assert.equal(posts, 2);
});

test('authenticated requests do not retry against a session changed in another tab', async () => {
  let calls = 0;
  const {api} = createClient({csrf: 'session'}, async () => { calls++; return csrfError(); });
  await assert.rejects(api('/me', {method: 'PUT', body: {}}), /Обновите страницу/);
  assert.equal(calls, 1);
});

test('idempotency keys reach the server without being changed', async () => {
  const key = requestKey();
  const {api} = createClient({csrf: 'session'}, async (url, options) => {
    assert.equal(options.headers['Idempotency-Key'], key);
    return ok({id: 1});
  });
  await api('/listings', {method: 'POST', body: {}, idempotencyKey: key});
});

test('ordinary forbidden errors do not trigger CSRF recovery', async () => {
  let calls = 0;
  const {api} = createClient({}, async () => {
    calls++;
    return {ok: false, status: 403, json: async () => ({error: {code: 'forbidden', message: 'Аккаунт заблокирован'}})};
  });
  await assert.rejects(api('/auth/verify', {method: 'POST', guest: true, body: {}}), /заблокирован/);
  assert.equal(calls, 1);
});
