/**
 * companies.js — company picker (/companies).
 *
 * Autocomplete: debounced search, up to 20 matches, arrow keys + Enter to select.
 */

requireAuth();

const SEARCH_DEBOUNCE_MS = 250;
const SEARCH_LIMIT = 20;

let searchTimer = null;
let inFlight = null;
let rows = [];
let activeIndex = -1;

const inputEl = document.getElementById('company-search');
const resultsEl = document.getElementById('search-results');
const stateEl = document.getElementById('search-state');

const IDLE_HINT = 'Start typing to search — for example “Kamada”, “KMDA”, or “1567529”.';

function setState(msg, hintClass) {
  stateEl.textContent = msg || '';
  stateEl.classList.toggle('search-state-hint', !!hintClass);
  stateEl.style.display = msg ? 'block' : 'none';
}

function clearResults() {
  rows = [];
  activeIndex = -1;
  resultsEl.innerHTML = '';
  resultsEl.hidden = true;
  inputEl.setAttribute('aria-expanded', 'false');
  inputEl.removeAttribute('aria-activedescendant');
}

function companyURL(cik) {
  return '/companies/' + encodeURIComponent(cik);
}

function optionId(index) {
  return 'search-option-' + index;
}

function updateAriaActive() {
  if (activeIndex >= 0 && rows[activeIndex]) {
    inputEl.setAttribute('aria-activedescendant', optionId(activeIndex));
  } else {
    inputEl.removeAttribute('aria-activedescendant');
  }
}

function selectionHint() {
  if (!rows.length) return '';
  if (rows.length === 1) {
    return '1 match — press Enter to open, or keep typing.';
  }
  var hint = rows.length + ' matches — use ↑↓ to select, Enter to open';
  if (rows.length >= SEARCH_LIMIT) {
    hint += ' (showing top ' + SEARCH_LIMIT + ')';
  }
  return hint;
}

function renderResults() {
  if (!rows.length) {
    clearResults();
    return;
  }

  resultsEl.innerHTML = rows.map(function (r, i) {
    var tickers = (r.tickers || []).map(function (t) {
      return '<span class="badge">' + escHtml(t) + '</span>';
    }).join(' ');
    return '<div class="search-result-row' + (i === activeIndex ? ' active' : '') + '"' +
      ' id="' + optionId(i) + '"' +
      ' role="option" tabindex="-1" data-index="' + i + '"' +
      ' aria-selected="' + (i === activeIndex ? 'true' : 'false') + '">' +
      '<span class="search-result-name">' + escHtml(r.name) + '</span>' +
      '<span>' + tickers + '</span>' +
      '<span class="search-result-cik">CIK ' + escHtml(r.cik) + '</span>' +
      '</div>';
  }).join('');

  resultsEl.hidden = false;
  inputEl.setAttribute('aria-expanded', 'true');
  updateAriaActive();

  Array.prototype.forEach.call(resultsEl.children, function (el) {
    el.addEventListener('click', function () {
      openCompany(Number(el.getAttribute('data-index')));
    });
    el.addEventListener('mouseenter', function () {
      activeIndex = Number(el.getAttribute('data-index'));
      renderResults();
    });
  });

  if (activeIndex >= 0) {
    var activeEl = resultsEl.children[activeIndex];
    if (activeEl && activeEl.scrollIntoView) {
      activeEl.scrollIntoView({ block: 'nearest' });
    }
  }
}

function openCompany(index) {
  var row = rows[index];
  if (row) window.location.href = companyURL(row.cik);
}

function moveActive(delta) {
  if (!rows.length) return;
  if (activeIndex < 0 && delta > 0) {
    activeIndex = 0;
  } else {
    activeIndex = (activeIndex + delta + rows.length) % rows.length;
  }
  renderResults();
}

async function runSearch(q) {
  if (inFlight) inFlight.abort();
  var controller = new AbortController();
  inFlight = controller;

  clearResults();
  setState('Searching…');
  try {
    var url = '/api/companies/search?q=' + encodeURIComponent(q) +
      '&limit=' + SEARCH_LIMIT;
    var data = await apiFetch(url, { signal: controller.signal });
    if (controller !== inFlight) return;
    rows = data || [];
    activeIndex = -1;
    if (!rows.length) {
      clearResults();
      setState('No companies match “' + q + '”.');
      return;
    }
    renderResults();
    setState(selectionHint(), true);
  } catch (err) {
    if (err && err.name === 'AbortError') return;
    if (controller !== inFlight) return;
    clearResults();
    setState('Search failed: ' + err.message);
  }
}

inputEl.addEventListener('input', function () {
  var q = inputEl.value.trim();
  clearTimeout(searchTimer);
  if (!q) {
    if (inFlight) inFlight.abort();
    clearResults();
    setState(IDLE_HINT);
    return;
  }
  searchTimer = setTimeout(function () { runSearch(q); }, SEARCH_DEBOUNCE_MS);
});

inputEl.addEventListener('keydown', function (e) {
  switch (e.key) {
    case 'ArrowDown':
      if (rows.length) {
        e.preventDefault();
        moveActive(1);
      }
      break;
    case 'ArrowUp':
      if (rows.length) {
        e.preventDefault();
        moveActive(-1);
      }
      break;
    case 'Enter':
      if (rows.length === 1 && activeIndex < 0) {
        e.preventDefault();
        openCompany(0);
      } else if (activeIndex >= 0) {
        e.preventDefault();
        openCompany(activeIndex);
      }
      break;
    case 'Escape':
      e.preventDefault();
      inputEl.value = '';
      if (inFlight) inFlight.abort();
      clearResults();
      setState(IDLE_HINT);
      break;
  }
});

setState(IDLE_HINT);
inputEl.focus();

(async function () {
  try {
    var me = await apiFetch('/auth/me');
    document.getElementById('header-email').textContent = me.email;
    if (me.role === 'admin') {
      document.getElementById('admin-link').style.display = '';
    }
  } catch (err) {
    logout();
  }
})();
