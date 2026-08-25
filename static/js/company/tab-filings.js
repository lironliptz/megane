/**
 * tab-filings.js — paginated filings table with year/form filters.
 *
 * Moved verbatim from company-page.js during the tab refactor; same PAGE_SIZE,
 * same API, same element ids.
 */

(function () {
  const PAGE_SIZE = 25;

  let ctx = null;
  let offset = 0;
  let total = 0;

  function renderFilters(cov, sum) {
    const yearSel = document.getElementById('filter-year');
    yearSel.innerHTML = '<option value="">All years</option>' +
      (cov.yearsOnDisk || []).slice().reverse().map(function (y) {
        return '<option value="' + escHtml(y) + '">' + escHtml(y) + '</option>';
      }).join('');

    const formSel = document.getElementById('filter-form');
    formSel.innerHTML = '<option value="">All forms</option>' +
      ctx.sortedEntries(sum.byForm).map(function (e) {
        return '<option value="' + escHtml(e[0]) + '">' + escHtml(e[0]) + ' (' + escHtml(e[1]) + ')</option>';
      }).join('');
  }

  function renderFilings(page) {
    total = page.total;
    const tbody = document.getElementById('filings-tbody');

    if (!page.items.length) {
      tbody.innerHTML = '<tr><td colspan="6" class="cell-muted">No filings match this filter.</td></tr>';
    } else {
      tbody.innerHTML = page.items.map(function (f) {
        const cat = (f.category && f.category !== 'unknown')
          ? escHtml(f.category.replace(/_/g, ' '))
          : '<span class="muted">Uncategorized</span>';
        const tier = f.tierLabel ? '<span class="badge">' + escHtml(f.tierLabel) + '</span>' : '';
        const fin = f.hasFinancials ? ' <span class="badge complete">FIN</span>' : '';
        return '<tr>' +
          '<td>' + escHtml(f.filingDate) + '</td>' +
          '<td><strong>' + escHtml(f.form) + '</strong>' + fin + '</td>' +
          '<td>' + cat + '</td>' +
          '<td>' + tier + '</td>' +
          '<td><code>' + escHtml(f.accessionNumber) + '</code></td>' +
          '<td class="filing-summary-cell">' + escHtml(f.summary || '') + '</td>' +
          '</tr>';
      }).join('');
    }

    const from = total === 0 ? 0 : page.offset + 1;
    const to = Math.min(page.offset + page.items.length, total);
    document.getElementById('page-status').textContent = from + '–' + to + ' of ' + total;
    document.getElementById('page-prev').disabled = page.offset <= 0;
    document.getElementById('page-next').disabled = to >= total;
  }

  function filingsQuery() {
    const params = ['limit=' + PAGE_SIZE, 'offset=' + offset];
    const year = document.getElementById('filter-year').value;
    const form = document.getElementById('filter-form').value;
    if (year) params.push('year=' + encodeURIComponent(year));
    if (form) params.push('form=' + encodeURIComponent(form));
    return '/api/companies/' + encodeURIComponent(ctx.cik) + '/filings?' + params.join('&');
  }

  async function loadFilings() {
    try {
      renderFilings(await ctx.apiFetch(filingsQuery()));
    } catch (err) {
      document.getElementById('filings-tbody').innerHTML =
        '<tr><td colspan="6" class="cell-muted">Could not load filings: ' + escHtml(err.message) + '</td></tr>';
    }
  }

  CompanyTabs.register('filings', {
    init: function (c) {
      ctx = c;
      renderFilters(c.detail.coverage, c.detail.filingsSummary);

      document.getElementById('page-prev').addEventListener('click', function () {
        offset = Math.max(0, offset - PAGE_SIZE);
        loadFilings();
      });
      document.getElementById('page-next').addEventListener('click', function () {
        if (offset + PAGE_SIZE < total) {
          offset += PAGE_SIZE;
          loadFilings();
        }
      });
      ['filter-year', 'filter-form'].forEach(function (id) {
        document.getElementById(id).addEventListener('change', function () {
          offset = 0;
          loadFilings();
        });
      });

      loadFilings();
    },
  });
})();
