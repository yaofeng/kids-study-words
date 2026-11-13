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

// Submit filter forms when their select changes, without inline handlers.
document.addEventListener('change', (event) => {
  const el = event.target.closest('[data-auto-submit]');
  if (el && el.form) el.form.requestSubmit();
});
