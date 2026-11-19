// Confirm destructive actions marked with data-confirm.
document.addEventListener('click', (event) => {
  const el = event.target.closest('[data-confirm]');
  if (!el) return;
  if (!window.confirm(el.getAttribute('data-confirm'))) {
    event.preventDefault();
    event.stopImmediatePropagation();
  }
});

// htmx swallows 401 responses; follow the server's redirect instead.
document.addEventListener('htmx:responseError', (event) => {
  const xhr = event.detail.xhr;
  const target = xhr.getResponseHeader('HX-Redirect');
  if (xhr.status === 401 && target) {
    window.location.href = target;
  }
});

// Tick the round timer shown on recall cards.
function formatElapsed(seconds) {
  const h = Math.floor(seconds / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  const mm = String(m).padStart(2, '0');
  const ss = String(s).padStart(2, '0');
  return h > 0 ? h + ':' + mm + ':' + ss : mm + ':' + ss;
}

function updateRoundTimers() {
  const now = Math.floor(Date.now() / 1000);
  document.querySelectorAll('[data-round-timer]').forEach((el) => {
    const start = Number(el.dataset.start);
    if (!Number.isFinite(start) || start <= 0) return;
    el.textContent = formatElapsed(Math.max(0, now - start));
  });
}

updateRoundTimers();
setInterval(updateRoundTimers, 1000);
document.body.addEventListener('htmx:afterSwap', updateRoundTimers);

// Read recall words aloud with the browser's speech synthesis. A card marked
// with data-speak-auto speaks once when it appears; speaker buttons marked with
// data-speak replay their word on demand.
let lastAutoSpokenCard = null;

function speakWord(text) {
  if (!text || !('speechSynthesis' in window)) return;
  window.speechSynthesis.cancel();
  const utterance = new SpeechSynthesisUtterance(text);
  utterance.lang = 'en-US';
  utterance.rate = 0.9;
  window.speechSynthesis.speak(utterance);
}

function autoSpeakRecallCard() {
  const el = document.querySelector('[data-speak-auto]');
  if (!el || el === lastAutoSpokenCard) return;
  lastAutoSpokenCard = el;
  speakWord(el.getAttribute('data-speak-auto'));
}

autoSpeakRecallCard();
document.body.addEventListener('htmx:afterSwap', autoSpeakRecallCard);

document.addEventListener('click', (event) => {
  const btn = event.target.closest('[data-speak]');
  if (btn) speakWord(btn.getAttribute('data-speak'));
});

// Toggle the settings panel on a notebook card.
document.addEventListener('click', (event) => {
  const toggle = event.target.closest('[data-settings-toggle]');
  if (!toggle) return;
  const panel = document.getElementById(toggle.getAttribute('aria-controls'));
  if (!panel) return;
  const opening = panel.hasAttribute('hidden');
  panel.toggleAttribute('hidden', !opening);
  toggle.setAttribute('aria-expanded', String(opening));
});

// Add or remove example sentence rows, keeping field names sequential.
function renumberExamples(list) {
  Array.from(list.querySelectorAll('.example-row')).forEach((row, index) => {
    row.querySelectorAll('input[name^="example_en_"], input[name^="example_zh_"], input[name^="example_source_"]').forEach((input) => {
      input.name = input.name.replace(/_\d+$/, '_' + (index + 1));
    });
  });
}

document.addEventListener('click', (event) => {
  const add = event.target.closest('[data-add-example]');
  if (add) {
    const list = add.parentElement.querySelector('.example-list');
    const max = Number(list.dataset.max || 5);
    if (list.querySelectorAll('.example-row').length >= max) return;
    const tpl = document.getElementById('example-row-template');
    if (!tpl) return;
    list.appendChild(tpl.content.firstElementChild.cloneNode(true));
    renumberExamples(list);
    return;
  }

  const remove = event.target.closest('[data-remove-example]');
  if (remove) {
    const list = remove.closest('.example-list');
    if (list.querySelectorAll('.example-row').length <= 1) {
      list.querySelectorAll('input').forEach((input) => { input.value = ''; });
      return;
    }
    remove.closest('.example-row').remove();
    renumberExamples(list);
  }
});

// Open and close modal dialogs marked with data-open-dialog / data-close-dialog.
document.addEventListener('click', (event) => {
  const opener = event.target.closest('[data-open-dialog]');
  if (opener) {
    const dialog = document.getElementById(opener.getAttribute('data-open-dialog'));
    if (dialog && typeof dialog.showModal === 'function') dialog.showModal();
    return;
  }
  const closer = event.target.closest('[data-close-dialog]');
  if (closer) {
    const dialog = closer.closest('dialog');
    if (dialog) dialog.close();
    return;
  }
  // A click on the backdrop lands on the dialog element itself, outside its box.
  const dialog = event.target;
  if (dialog instanceof HTMLDialogElement && dialog.open) {
    const box = dialog.getBoundingClientRect();
    const outside = event.clientX < box.left || event.clientX > box.right ||
      event.clientY < box.top || event.clientY > box.bottom;
    if (outside) dialog.close();
  }
});

// Show 更多 only when the inline group chips overflow their row.
function updateGroupChips() {
  document.querySelectorAll('[data-group-chips]').forEach((list) => {
    const more = list.parentElement.querySelector('[data-group-more]');
    if (!more) return;
    const overflowing = list.scrollWidth > list.clientWidth + 1;
    more.hidden = !overflowing;
    list.classList.toggle('is-clipped', overflowing);
  });
}

updateGroupChips();
window.addEventListener('load', updateGroupChips);
window.addEventListener('resize', updateGroupChips);

// Submit filter forms when their select changes, without inline handlers.
document.addEventListener('change', (event) => {
  const el = event.target.closest('[data-auto-submit]');
  if (el && el.form) el.form.requestSubmit();
});

// Keep the scope radio in sync with the proficiency select and group boxes.
document.addEventListener('change', (event) => {
  const select = event.target.closest('[data-scope-proficiency]');
  if (select) {
    const radio = select.form.querySelector('input[name="scope"][value="proficiency"]');
    if (radio) radio.checked = true;
  }
  const box = event.target.closest('[data-scope-group]');
  if (box && box.checked) {
    const radio = box.form.querySelector('input[name="scope"][value="groups"]');
    if (radio) radio.checked = true;
  }
});

// Copy a value into the clipboard from a button marked with data-copy.
document.addEventListener('click', async (event) => {
  const btn = event.target.closest('[data-copy]');
  if (!btn) return;
  const input = document.querySelector(btn.getAttribute('data-copy'));
  if (!input) return;
  const text = input.value ?? input.textContent;
  try {
    await navigator.clipboard.writeText(text);
    btn.textContent = '已复制';
  } catch {
    input.focus();
    input.select();
    let copied = false;
    try {
      copied = document.execCommand('copy');
    } catch {
      copied = false;
    }
    btn.textContent = copied ? '已复制' : '复制失败，请手动选择';
  }
});

