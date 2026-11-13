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
