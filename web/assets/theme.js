'use strict';
(() => {
 const key = 'dormhub.theme';
 const system = window.matchMedia('(prefers-color-scheme: dark)');
 let preference = null;
 const valid = value => value === 'light' || value === 'dark' ? value : null;
 try { preference = valid(localStorage.getItem(key)); } catch (_) {}
 function updateButtons() {
  const dark = document.documentElement.dataset.theme === 'dark';
  document.querySelectorAll('[data-theme-toggle]').forEach(button => {
   const label = dark ? 'Включить светлую тему' : 'Включить тёмную тему';
   const icon = dark ? '☀' : '☾';
   if (button.textContent !== icon) button.textContent = icon;
   button.setAttribute('aria-label', label);
   button.title = label;
  });
 }
 function apply() {
  const theme = preference || (system.matches ? 'dark' : 'light');
  document.documentElement.dataset.theme = theme;
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.content = theme === 'dark' ? '#17191d' : '#f4f3ef';
  updateButtons();
 }
 apply();
 document.addEventListener('click', event => {
  if (!event.target.closest('[data-theme-toggle]')) return;
  preference = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark';
  try { localStorage.setItem(key, preference); } catch (_) {}
  apply();
 });
 system.addEventListener('change', () => { if (!preference) apply(); });
 window.addEventListener('storage', event => {
  if (event.key === key || event.key === null) {
   try { preference = valid(localStorage.getItem(key)); } catch (_) { preference = null; }
   apply();
  }
 });
 function observe() {
  updateButtons();
  new MutationObserver(updateButtons).observe(document.body, { childList: true, subtree: true });
 }
 if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', observe, { once: true });
 else observe();
})();
