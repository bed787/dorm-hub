"use strict";

// Steps only control presentation. The existing editor owns saves, uploads and
// recovery; changing steps never replaces the form or its selected files.
globalThis.DormHubWizard = {
  init(form, { preview, changed, initial = 1 }) {
    let step = initial;
    let busy = false;
    const panes = [...form.querySelectorAll("[data-editor-step]")];
    function show(value) {
      step = value;
      form.dataset.step = String(step);
      panes.forEach(p => p.hidden = Number(p.dataset.editorStep) !== step);
      form.querySelectorAll("[data-step-label]").forEach(el => {
        el.classList.toggle("selected", Number(el.dataset.stepLabel) === step);
        if (Number(el.dataset.stepLabel) === step) el.setAttribute("aria-current", "step");
        else el.removeAttribute("aria-current");
      });
      const category = form.elements.namedItem("category").value;
      form.querySelectorAll("[data-wizard-category]").forEach(b => b.setAttribute("aria-pressed", String(b.dataset.wizardCategory === category)));
      form.querySelectorAll("[data-wizard-kind]").forEach(b => b.setAttribute("aria-pressed", String(b.dataset.wizardKind === form.elements.namedItem("kind").value)));
      const next = form.querySelector('[data-wizard="next"]');
      next.disabled = !category;
    }
    function notify(input) {
      input.dispatchEvent(new Event("input", { bubbles: true }));
      changed();
    }
    form.addEventListener("click", async event => {
      const button = event.target.closest("[data-wizard],[data-wizard-category],[data-wizard-kind]");
      if (!button || !form.contains(button) || form.dataset.busy || busy) return;
      event.preventDefault();
      if (button.dataset.wizardCategory) {
        const input = form.elements.namedItem("category");
        input.value = button.dataset.wizardCategory;
        show(step); notify(input); return;
      }
      if (button.dataset.wizardKind) {
        const input = form.elements.namedItem("kind");
        input.value = button.dataset.wizardKind;
        show(step); notify(input); return;
      }
      if (button.dataset.wizard === "next") { if (form.elements.namedItem("category").value) show(2); return; }
      if (button.dataset.wizard === "back") { show(Math.max(1, step - 1)); return; }
      if (button.dataset.wizard !== "preview") return;
      for (const control of form.querySelector('[data-editor-step="2"]').querySelectorAll("input,select,textarea")) {
        if (!control.disabled && !control.reportValidity()) return;
      }
      busy = true; button.disabled = true;
      const error = form.querySelector("[data-preview-error]");
      error.hidden = true;
      try {
        const html = await preview();
        if (!form.isConnected) return;
        form.querySelector("[data-listing-preview]").innerHTML = html;
        show(3);
      } catch (e) { error.textContent = e.message; error.hidden = false; }
      finally { busy = false; button.disabled = false; }
    });
    // Native validation must be able to focus the fields on step 2.
    form.addEventListener("invalid", event => {
      if (event.target.closest('[data-editor-step="2"]') && step !== 2) show(2);
    }, true);
    form.addEventListener("keydown", event => {
      if (event.key === "Enter" && step === 2 && event.target.matches("input:not([type=file])")) {
        event.preventDefault(); form.querySelector('[data-wizard="preview"]').click();
      }
    });
    form.addEventListener("submit", event => {
      if (event.submitter?.value === "draft" || step === 3) return;
      event.preventDefault(); event.stopPropagation();
      if (step === 2) form.querySelector('[data-wizard="preview"]').click();
      else show(2);
    });
    show(step);
  }
};
