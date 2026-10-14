'use strict';

// Keep native selects as the form's source of truth, and use a themed dialog
// for their presentation. This avoids OS-specific mobile select popups.
(function () {
  const controls = new WeakMap();
  let active = null;
  const picker = document.createElement('dialog');
  picker.id = 'select-picker';
  picker.className = 'select-picker';
  picker.setAttribute('aria-labelledby', 'select-picker-title');
  const heading = document.createElement('div');
  heading.className = 'select-picker-heading';
  const title = document.createElement('h2');
  title.id = 'select-picker-title';
  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'select-picker-close';
  close.textContent = '×';
  close.setAttribute('aria-label', 'Закрыть выбор');
  heading.append(title, close);
  const list = document.createElement('div');
  list.className = 'select-picker-options';
  list.setAttribute('role', 'listbox');
  list.setAttribute('aria-labelledby', title.id);
  picker.append(heading, list);
  document.body.append(picker);

  function caption(select) {
    return select.getAttribute('aria-label') ||
      select.closest('label')?.querySelector('span')?.textContent.trim() || 'Выберите';
  }

  function sync(select) {
    const control = controls.get(select);
    if (!control) return;
    const text = select.selectedOptions[0]?.textContent || 'Выберите';
    if (control.text.textContent !== text) control.text.textContent = text;
    if (control.button.disabled !== select.disabled) control.button.disabled = select.disabled;
    const label = caption(select) + ': ' + text;
    if (control.button.getAttribute('aria-label') !== label) control.button.setAttribute('aria-label', label);
  }

  function openPicker(select) {
    if (select.disabled || !select.isConnected) return;
    if (picker.open) picker.close();
    const control = controls.get(select);
    active = {select, button: control.button};
    title.textContent = caption(select);
    list.replaceChildren();
    for (const option of select.options) {
      if (option.hidden) continue;
      const row = document.createElement('button');
      row.type = 'button';
      row.className = 'select-picker-option';
      row.setAttribute('role', 'option');
      row.setAttribute('aria-selected', String(option.selected));
      row.disabled = option.disabled || Boolean(option.closest('optgroup')?.disabled);
      row.tabIndex = -1;
      const label = document.createElement('span');
      label.textContent = option.textContent;
      const mark = document.createElement('span');
      mark.className = 'select-picker-mark';
      mark.setAttribute('aria-hidden', 'true');
      mark.textContent = option.selected ? '✓' : '';
      row.append(label, mark);
      row.addEventListener('click', () => {
        if (!select.isConnected || select.disabled) { picker.close(); return; }
        select.value = option.value;
        select.dispatchEvent(new Event('input', {bubbles: true}));
        select.dispatchEvent(new Event('change', {bubbles: true}));
        control.wrapper.classList.remove('select-invalid');
        sync(select);
        picker.close();
      });
      list.append(row);
    }
    control.button.setAttribute('aria-expanded', 'true');
    picker.showModal();
    const selected = list.querySelector('[aria-selected="true"]:not(:disabled)') || list.querySelector('button:not(:disabled)');
    if (selected) { selected.tabIndex = 0; selected.focus(); }
  }

  function enhance(select) {
    if (select.multiple || select.size > 1) return;
    if (controls.has(select)) { sync(select); return; }
    const wrapper = document.createElement('div');
    wrapper.className = 'select-control';
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'select-trigger';
    button.setAttribute('aria-haspopup', 'dialog');
    button.setAttribute('aria-controls', picker.id);
    button.setAttribute('aria-expanded', 'false');
    const text = document.createElement('span');
    text.className = 'select-trigger-text';
    button.append(text);
    select.before(wrapper);
    wrapper.append(select, button);
    select.classList.add('select-native');
    select.tabIndex = -1;
    select.setAttribute('aria-hidden', 'true');
    controls.set(select, {wrapper, button, text});
    button.addEventListener('click', () => openPicker(select));
    button.addEventListener('keydown', event => {
      if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
        event.preventDefault(); openPicker(select);
      }
    });
    select.addEventListener('change', () => { sync(select); wrapper.classList.remove('select-invalid'); });
    select.addEventListener('invalid', event => {
      event.preventDefault();
      wrapper.classList.add('select-invalid');
      // HTML validation may emit multiple invalid events. Keep focus on the
      // first invalid select rather than repeatedly replacing the open picker.
      if (!picker.open) openPicker(select);
    });
    sync(select);
  }

  close.addEventListener('click', () => picker.close());
  picker.addEventListener('close', () => {
    if (!active || picker.open) return;
    active.button.setAttribute('aria-expanded', 'false');
    if (active.button.isConnected && !active.button.disabled) active.button.focus();
    active = null;
  });
  picker.addEventListener('click', event => {
    if (event.target !== picker) return;
    const rect = picker.getBoundingClientRect();
    if (event.clientX < rect.left || event.clientX > rect.right || event.clientY < rect.top || event.clientY > rect.bottom) picker.close();
  });
  list.addEventListener('keydown', event => {
    if (!['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return;
    event.preventDefault();
    const rows = Array.from(list.querySelectorAll('button:not(:disabled)'));
    if (!rows.length) return;
    const current = rows.indexOf(document.activeElement);
    const index = event.key === 'Home' ? 0 : event.key === 'End' ? rows.length - 1 :
      Math.max(0, Math.min(rows.length - 1, current + (event.key === 'ArrowDown' ? 1 : -1)));
    rows.forEach(row => { row.tabIndex = -1; });
    rows[index].tabIndex = 0;
    rows[index].focus();
  });

  const observer = new MutationObserver(() => {
    if (active && (!active.select.isConnected || active.select.disabled)) picker.close();
    document.querySelectorAll('select').forEach(enhance);
  });
  observer.observe(document.body, {subtree: true, childList: true, attributes: true, attributeFilter: ['disabled', 'selected', 'value']});
  document.querySelectorAll('select').forEach(enhance);
  document.addEventListener('reset', event => {
    setTimeout(() => event.target.querySelectorAll('select').forEach(sync), 0);
  });
})();
