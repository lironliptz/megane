/**
 * tab-general.js — identity, coverage, and filings-summary cards.
 *
 * Moved verbatim from company-page.js during the tab refactor; behavior and
 * element ids are unchanged.
 */

(function () {
  const TOP_FORMS = 8;

  let ctx = null;
  let showAllForms = false;
  let formCounts = {};

  // Absent optional fields are omitted entirely rather than rendered as "—".
  function renderIdentity(id) {
    let html = '';
    if (id.sic || id.sicDescription) {
      html += ctx.metaItem('SIC', (id.sic ? id.sic + ' — ' : '') + (id.sicDescription || ''));
    }
    if (id.stateOfIncorporation) html += ctx.metaItem('Incorporated in', id.stateOfIncorporation);
    if (id.fiscalYearEnd) html += ctx.metaItem('Fiscal year end', id.fiscalYearEnd);
    if (id.category) html += ctx.metaItem('Filer category', id.category);
    if (id.hq && (id.hq.city || id.hq.country)) {
      html += ctx.metaItem('Headquarters', [id.hq.city, id.hq.country].filter(Boolean).join(', '));
    }
    if (id.phone) html += ctx.metaItem('Phone', id.phone);
    if (id.formerNames && id.formerNames.length) {
      html += ctx.metaItem('Former names', id.formerNames.join(' · '));
    }
    document.getElementById('identity-grid').innerHTML = html ||
      '<p class="muted">No additional identity details on file.</p>';
  }

  function renderCoverage(cov) {
    let line = 'We have filings from <strong>' + escHtml(cov.earliestFilingDate || '—') +
      '</strong> to <strong>' + escHtml(cov.latestFilingDate || '—') + '</strong> · <strong>' +
      escHtml(cov.totalFilings) + '</strong> filings on file';
    // The disk-vs-index gap, surfaced rather than hidden.
    if (cov.indexedNotOnDisk > 0) {
      line += ' <span class="coverage-gap">· ' + escHtml(cov.indexedNotOnDisk) +
        ' more known to SEC, not yet downloaded</span>';
    }
    document.getElementById('coverage-line').innerHTML = line;
    document.getElementById('coverage-years').innerHTML =
      (cov.yearsOnDisk || []).map(function (y) { return ctx.chip(y, null, false); }).join('');
  }

  function renderFormBars() {
    const entries = ctx.sortedEntries(formCounts);
    const shown = showAllForms ? entries : entries.slice(0, TOP_FORMS);
    const max = entries.length ? entries[0][1] : 1;

    document.getElementById('form-bars').innerHTML = shown.map(function (e) {
      const pct = Math.max(1, Math.round((e[1] / max) * 100));
      return '<div class="bar-row">' +
        '<span class="bar-label" title="' + escHtml(e[0]) + '">' + escHtml(e[0]) + '</span>' +
        '<span class="bar-track"><span class="bar-fill" style="width:' + pct + '%"></span></span>' +
        '<span class="bar-count">' + escHtml(e[1]) + '</span>' +
        '</div>';
    }).join('');

    const toggle = document.getElementById('form-toggle');
    if (entries.length > TOP_FORMS) {
      toggle.style.display = '';
      toggle.textContent = showAllForms
        ? 'Show top ' + TOP_FORMS
        : '+' + (entries.length - TOP_FORMS) + ' more form types';
    } else {
      toggle.style.display = 'none';
    }
  }

  function renderSummary(sum) {
    formCounts = sum.byForm || {};
    renderFormBars();

    // "unknown" still shows, as a muted chip, but never leads the list.
    const cats = ctx.sortedEntries(sum.byCategory).filter(function (e) { return e[0] !== 'unknown'; });
    const unknown = (sum.byCategory || {}).unknown;
    let catHtml = cats.map(function (e) { return ctx.chip(e[0].replace(/_/g, ' '), e[1], false); }).join('');
    if (unknown) catHtml += ctx.chip('Uncategorized', unknown, true);
    document.getElementById('category-chips').innerHTML = catHtml ||
      '<span class="muted">No categories.</span>';

    // Tier labels are rendered as returned; nothing hardcodes MAJOR/MINOR.
    document.getElementById('tier-chips').innerHTML =
      ctx.sortedEntries(sum.byTier).map(function (e) { return ctx.chip(e[0], e[1], false); }).join('') ||
      '<span class="muted">No tiers.</span>';
  }

  CompanyTabs.register('general', {
    init: function (c) {
      ctx = c;
      renderIdentity(c.detail.identity);
      renderCoverage(c.detail.coverage);
      renderSummary(c.detail.filingsSummary);

      document.getElementById('form-toggle').addEventListener('click', function () {
        showAllForms = !showAllForms;
        renderFormBars();
      });
    },
  });
})();
