/**
 * shell.js — company view shell (/companies/:cik).
 *
 * Owns the header, the company detail fetch, and the tab bar + hash router.
 * Tab modules register themselves and receive a shared context; the shell never
 * knows what a tab renders.
 *
 * Panels are hidden with .tab-panel's display:none rather than unmounted, so
 * chart state and scroll position survive a tab switch.
 */

requireAuth();

const TAB_NAMES = ['timeline', 'general', 'filings', 'events'];
const DEFAULT_TAB = 'timeline';

const cik = decodeURIComponent(window.location.pathname.split('/').pop() || '');

/* ------------------------------------------------------------ shared bits */

function chip(label, count, muted) {
  return '<span class="chip' + (muted ? ' chip-muted' : '') + '">' +
    escHtml(label) +
    (count == null ? '' : ' <span class="chip-count">' + escHtml(count) + '</span>') +
    '</span>';
}

function metaItem(label, value) {
  return '<div class="meta-item"><span class="meta-label">' + escHtml(label) +
    '</span><span class="meta-value">' + escHtml(value) + '</span></div>';
}

function sortedEntries(obj) {
  return Object.keys(obj || {})
    .map(function (k) { return [k, obj[k]]; })
    .sort(function (a, b) { return b[1] - a[1] || a[0].localeCompare(b[0]); });
}

/* -------------------------------------------------------------- registry */

const CompanyTabs = (function () {
  const mods = {};
  const started = {};
  let ctx = null;

  return {
    /** Tab modules call this at load time. */
    register: function (name, mod) { mods[name] = mod; },

    /** Shell calls this once the company detail is known. */
    setContext: function (c) { ctx = c; },

    /** Lazily init a tab the first time it is shown, then show/hide. */
    activate: function (name) {
      Object.keys(mods).forEach(function (n) {
        if (n !== name && started[n] && typeof mods[n].hide === 'function') mods[n].hide();
      });
      const mod = mods[name];
      if (!mod || !ctx) return;
      if (!started[name]) {
        started[name] = true;
        if (typeof mod.init === 'function') mod.init(ctx);
      }
      if (typeof mod.show === 'function') mod.show();
    },
  };
})();

/* ------------------------------------------------------------ tab routing */

function tabFromHash() {
  const raw = (location.hash || '').replace(/^#/, '').toLowerCase();
  return TAB_NAMES.indexOf(raw) !== -1 ? raw : DEFAULT_TAB;
}

function switchTab(name, opts) {
  opts = opts || {};
  if (TAB_NAMES.indexOf(name) === -1) name = DEFAULT_TAB;

  TAB_NAMES.forEach(function (n) {
    const btn = document.getElementById('tabbtn-' + n);
    const panel = document.getElementById('tab-' + n);
    const active = n === name;
    if (btn) {
      btn.classList.toggle('active', active);
      btn.setAttribute('aria-selected', active ? 'true' : 'false');
      btn.tabIndex = active ? 0 : -1;   // roving tabindex
    }
    if (panel) panel.classList.toggle('active', active);
  });

  if (!opts.skipHash && location.hash !== '#' + name) location.hash = name;
  CompanyTabs.activate(name);
}

function wireTabBar() {
  TAB_NAMES.forEach(function (n) {
    const btn = document.getElementById('tabbtn-' + n);
    if (!btn) return;
    btn.addEventListener('click', function () { switchTab(n); });
    btn.addEventListener('keydown', function (e) {
      let delta = 0;
      if (e.key === 'ArrowRight') delta = 1;
      else if (e.key === 'ArrowLeft') delta = -1;
      else if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); switchTab(n); return; }
      else return;
      e.preventDefault();
      const i = TAB_NAMES.indexOf(n);
      const next = TAB_NAMES[(i + delta + TAB_NAMES.length) % TAB_NAMES.length];
      const nextBtn = document.getElementById('tabbtn-' + next);
      if (nextBtn) nextBtn.focus();
    });
  });
  window.addEventListener('hashchange', function () {
    switchTab(tabFromHash(), { skipHash: true });
  });
}

/* ------------------------------------------------------------- rendering */

function renderHeader(id) {
  document.title = 'megane — ' + id.name;
  document.getElementById('company-name').textContent = id.name;
  document.getElementById('company-tickers').innerHTML =
    (id.tickers || []).map(function (t) { return '<span class="badge">' + escHtml(t) + '</span>'; }).join(' ');

  const bits = ['CIK ' + id.cik];
  if (id.exchanges && id.exchanges.length) bits.push(id.exchanges.join(', '));
  if (id.sicDescription) bits.push(id.sicDescription);
  document.getElementById('company-subtitle').textContent = bits.join(' · ');
}

function renderNotFound(message) {
  document.getElementById('company-name').textContent = 'Company not found';
  document.getElementById('company-subtitle').textContent =
    'No company on file for CIK ' + cik + ' (' + message + ').';
  // Same ids as before the tab refactor, so this path is unchanged.
  ['identity-card', 'coverage-card', 'summary-card'].forEach(function (id) {
    const el = document.getElementById(id);
    if (el) el.style.display = 'none';
  });
  const tabs = document.querySelector('.tabs');
  if (tabs) tabs.style.display = 'none';
  const tl = document.getElementById('timeline-panel-body');
  if (tl) tl.innerHTML = '<p class="muted">—</p>';
  document.getElementById('filings-tbody').innerHTML =
    '<tr><td colspan="6" class="cell-muted">—</td></tr>';
  showError('Company not found');
}

async function init() {
  let detail;
  try {
    detail = await apiFetch('/api/companies/' + encodeURIComponent(cik));
  } catch (err) {
    renderNotFound(err.message);
    return;
  }

  renderHeader(detail.identity);
  CompanyTabs.setContext({
    cik: cik,
    detail: detail,
    apiFetch: apiFetch,
    escHtml: escHtml,
    chip: chip,
    metaItem: metaItem,
    sortedEntries: sortedEntries,
  });
  wireTabBar();
  switchTab(tabFromHash(), { skipHash: true });
}

// Tab modules are loaded after this file, so bootstrap once they have all
// registered themselves.
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}

// Header chrome. A failing /auth/me means a stale token — bounce to login.
(async () => {
  try {
    const me = await apiFetch('/auth/me');
    document.getElementById('header-email').textContent = me.email;
    if (me.role === 'admin') {
      document.getElementById('admin-link').style.display = '';
    }
  } catch (err) {
    logout();
  }
})();
