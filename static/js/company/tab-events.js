/**
 * tab-events.js — curated "special events", grouped by calendar year.
 *
 * The curation rule lives on the server (companyview.IsEvent): a category
 * allowlist, not a tier threshold. Measured against the sample corpus that is
 * ~100 of 381 filings, 5–13 per year — which is why this tab groups by year
 * rather than trying to show one flat "top 10–30" list.
 *
 * Reuses the timeline endpoint with filter=financials; no separate API.
 */

(function () {
  let ctx = null;
  let loaded = false;
  let yearJumpWired = false;

  function el(id) { return document.getElementById(id); }

  function eventsPanel() { return el('tab-events'); }

  function scrollToEventYear(year) {
    const panel = eventsPanel();
    if (!panel) return;

    if (!year) {
      panel.scrollTo({ top: 0, behavior: 'smooth' });
      return;
    }

    const section = el('event-year-' + year);
    if (!section) return;

    const top = section.getBoundingClientRect().top -
      panel.getBoundingClientRect().top + panel.scrollTop;
    panel.scrollTo({ top: Math.max(0, top - 8), behavior: 'smooth' });
  }

  function populateYearJump(years) {
    const sel = el('events-year-jump');
    if (!sel) return;

    const prev = sel.value;
    sel.innerHTML = '<option value="">All years</option>' +
      years.map(function (y) {
        return '<option value="' + escHtml(y) + '">' + escHtml(y) + '</option>';
      }).join('');

    if (prev && years.indexOf(prev) !== -1) sel.value = prev;
  }

  function wireYearJump() {
    if (yearJumpWired) return;
    const sel = el('events-year-jump');
    if (!sel) return;
    yearJumpWired = true;
    sel.addEventListener('change', function () {
      scrollToEventYear(sel.value);
    });
  }

  function card(e) {
    const cat = String(e.category || '').replace(/_/g, ' ');
    const tier = e.tierLabel ? '<span class="badge">' + escHtml(e.tierLabel) + '</span>' : '';
    return '<article class="event-card" data-date="' + escHtml(e.filingDate) + '">' +
      '<div class="event-card-head">' +
        '<span class="event-date">' + escHtml(e.filingDate) + '</span>' +
        ctx.chip(cat, null, false) +
        '<span class="event-form">' + escHtml(e.form) + '</span>' +
        tier +
      '</div>' +
      (e.why ? '<p class="event-why">' + escHtml(e.why) + '</p>' : '') +
      (e.summary ? '<p class="event-summary">' + escHtml(e.summary) + '</p>' : '') +
      '<code class="event-acc">' + escHtml(e.accessionNumber) + '</code>' +
      '</article>';
  }

  function render(events) {
    const box = el('events-list');
    if (!events.length) {
      box.innerHTML = '<p class="muted">No curated events in the filing history.</p>';
      populateYearJump([]);
      wireYearJump();
      return;
    }

    // Group by calendar year, newest year first.
    const byYear = {};
    events.forEach(function (e) {
      const y = String(e.filingDate).slice(0, 4);
      (byYear[y] = byYear[y] || []).push(e);
    });

    const years = Object.keys(byYear).sort().reverse();
    populateYearJump(years);
    wireYearJump();

    box.innerHTML = years.map(function (y) {
      return '<section class="event-year" id="event-year-' + escHtml(y) + '">' +
        '<h3 class="event-year-heading">' + escHtml(y) +
        ' <span class="muted">· ' + escHtml(byYear[y].length) + ' events</span></h3>' +
        byYear[y].map(card).join('') +
        '</section>';
    }).join('');

    // Clicking a card jumps to the timeline.
    Array.prototype.forEach.call(box.querySelectorAll('.event-card'), function (c) {
      c.addEventListener('click', function () { location.hash = 'timeline'; });
    });

    const count = el('events-count');
    if (count) count.textContent = events.length + ' curated events across the filing history';
  }

  async function load() {
    if (loaded) return;
    const box = el('events-list');
    box.innerHTML = '<p class="muted">Loading…</p>';
    try {
      const cov = ctx.detail.coverage || {};
      const qs = 'filter=financials' +
        (cov.earliestFilingDate ? '&from=' + encodeURIComponent(cov.earliestFilingDate) : '') +
        (cov.latestFilingDate ? '&to=' + encodeURIComponent(cov.latestFilingDate) : '');
      const tl = await ctx.apiFetch('/api/companies/' + encodeURIComponent(ctx.cik) + '/timeline?' + qs);
      loaded = true;
      render(tl.events || []);
    } catch (err) {
      box.innerHTML = '<p class="muted">Could not load events: ' + escHtml(err.message) + '</p>';
    }
  }

  CompanyTabs.register('events', {
    init: function (c) {
      ctx = c;
      wireYearJump();
      load();
    },
  });
})();
