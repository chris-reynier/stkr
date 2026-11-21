// Small helpers for the idea box and for pages shown inside the STKR desktop
// app (WebView2). Plain browsers only get the Enter-to-save behaviour; the
// rest needs window.chrome.webview, which only the desktop app provides.
(function () {
  'use strict';

  var host = window.chrome && window.chrome.webview;

  function send(message) {
    if (host) {
      host.postMessage(message);
    }
  }

  function onPanel() {
    return !!document.body && document.body.getAttribute('data-surface') === 'panel';
  }

  // Enter saves the idea, Shift+Enter adds a new line. Esc hides the panel.
  document.addEventListener('keydown', function (e) {
    var t = e.target;
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing && t && t.matches && t.matches('textarea[data-enter-submit]')) {
      e.preventDefault();
      if (t.value.trim() !== '' && t.form) {
        t.form.requestSubmit();
      }
      return;
    }
    if (e.key === 'Escape' && host && onPanel()) {
      send('hide');
    }
  });

  // After Settings were saved the desktop app restarts its server. The page
  // waits until the server answers again, then reloads itself.
  var waiting = false;
  function waitForRestart() {
    if (waiting || !document.querySelector('[data-wait-restart]')) {
      return;
    }
    waiting = true;
    var tries = 0;
    var probe = function () {
      tries++;
      fetch('/healthz', { cache: 'no-store' }).then(function (r) {
        if (r.ok && tries > 1) {
          window.location.reload();
        } else if (tries < 60) {
          setTimeout(probe, 600);
        }
      }).catch(function () {
        if (tries < 60) {
          setTimeout(probe, 600);
        }
      });
    };
    setTimeout(probe, 1200);
  }
  document.addEventListener('DOMContentLoaded', waitForRestart);
  document.addEventListener('htmx:afterSettle', waitForRestart);

  if (!host) {
    return;
  }

  // Desktop only: buttons marked data-desktop are shown and send their value.
  function reveal() {
    var list = document.querySelectorAll('[data-desktop]');
    for (var i = 0; i < list.length; i++) {
      list[i].hidden = false;
    }
  }
  document.addEventListener('DOMContentLoaded', reveal);
  document.addEventListener('htmx:afterSettle', reveal);

  function localPath(url) {
    return url.pathname + url.search;
  }

  // Links never navigate in a surprising window: another site opens in the
  // default browser, and a link in the panel opens in the app window.
  document.addEventListener('click', function (e) {
    var el = e.target && e.target.closest ? e.target.closest('a[href], [data-desktop]') : null;
    if (!el) {
      return;
    }
    if (el.hasAttribute('data-desktop')) {
      e.preventDefault();
      send(el.getAttribute('data-desktop'));
      return;
    }
    var url;
    try {
      url = new URL(el.href, window.location.href);
    } catch (err) {
      return;
    }
    if (url.origin !== window.location.origin) {
      e.preventDefault();
      e.stopImmediatePropagation();
      send('url:' + url.href);
    } else if (onPanel()) {
      e.preventDefault();
      e.stopImmediatePropagation();
      send('app:' + localPath(url));
    } else if (el.target === '_blank') {
      e.preventDefault();
      window.location.href = url.href;
    }
  }, true);

  window.open = function (target) {
    try {
      var url = new URL(target, window.location.href);
      send(url.origin === window.location.origin ? 'app:' + localPath(url) : 'url:' + url.href);
    } catch (err) {
      // an invalid address is ignored
    }
    return null;
  };

  // Called by the desktop app each time the panel is shown.
  window.stkrPanelShown = function (focusIdea) {
    var live = document.getElementById('live');
    if (live && window.htmx) {
      window.htmx.trigger(live, 'refresh');
    }
    if (focusIdea) {
      var box = document.getElementById('idea');
      if (box) {
        box.focus();
      }
    }
  };

  // Called by the desktop app after the pin was toggled.
  window.stkrPanelPinned = function (pinned) {
    var list = document.querySelectorAll('[data-desktop="pin"]');
    for (var i = 0; i < list.length; i++) {
      list[i].setAttribute('aria-pressed', pinned ? 'true' : 'false');
      list[i].textContent = pinned ? 'Pinned' : 'Pin';
    }
  };
})();
