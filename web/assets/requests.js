'use strict';
(function (root) {
  function requestKey() {
    const bytes = new Uint8Array(32);
    const crypto = root.crypto || (typeof require === 'function' ? require('node:crypto').webcrypto : null);
    if (!crypto) throw new Error('Браузер не поддерживает безопасные идентификаторы запросов');
    crypto.getRandomValues(bytes);
    return Array.from(bytes, byte => byte.toString(16).padStart(2, '0')).join('');
  }

  function createClient(state, fetcher, {onMaintenance = () => {}} = {}) {
    async function api(path, {method = 'GET', body, guest = false, idempotencyKey, retryGuest = true} = {}) {
      const headers = {};
      if (method !== 'GET' && method !== 'HEAD') {
        headers['X-CSRF-Token'] = guest ? state.guestCSRF : state.csrf;
      }
      if (idempotencyKey) headers['Idempotency-Key'] = idempotencyKey;
      let requestBody = body;
      if (body !== undefined && !(body instanceof FormData)) {
        headers['Content-Type'] = 'application/json';
        requestBody = JSON.stringify(body);
      }
      const response = await fetcher('/api' + path, {method, headers, body: requestBody, credentials: 'same-origin'});
      let payload;
      try { payload = await response.json(); }
      catch { throw new Error('Не удалось прочитать ответ сервера'); }
      if (!response.ok) {
        // Concurrent first visits can race to set the shared guest cookie.
        // Recover only the explicit CSRF error, and retry the original body once.
        if (guest && retryGuest && response.status === 403 && payload.error?.code === 'csrf') {
          state.guestCSRF = (await api('/auth/bootstrap')).csrf;
          return api(path, {method, body, guest, idempotencyKey, retryGuest: false});
        }
        const error = new Error(payload.error?.message || 'Ошибка запроса');
        error.status = response.status;
        error.code = payload.error?.code;
        if (response.status === 503 && error.code === 'maintenance') onMaintenance();
        throw error;
      }
      return payload.data;
    }
    return {api, requestKey};
  }

  const requests = {createClient, requestKey};
  if (typeof module !== 'undefined' && module.exports) module.exports = requests;
  else root.DormHubRequests = requests;
})(globalThis);
