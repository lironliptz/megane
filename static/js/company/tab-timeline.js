/**
 * tab-timeline.js — price line with filing-event markers.
 *
 * Chart.js notes that are load-bearing here:
 *
 *  - The x-axis is a CATEGORY axis, not a time axis. Chart.js 4 needs a date
 *    adapter for `type: 'time'` and none is bundled (only the stub that throws),
 *    so labels are the server's YYYY-MM-DD strings and markers are positioned by
 *    index into those labels.
 *  - Event markers are three extra scatter DATASETS (one per weight) rather than
 *    an annotation plugin, which is likewise not vendored. Separate datasets make
 *    the lane toggle a `hidden` flag and give tooltips for free.
 *  - Chart.js cannot measure a canvas inside display:none, so a chart built while
 *    the tab is hidden renders 0x0 until resize() runs on show().
 */

(function () {
  const PRESETS = { '1Y': 1, '2Y': 2, '5Y': 5, 'All': 0 };
  const WEIGHTS = [
    { key: 'major',  label: 'Major',   radius: 7, color: '#b91c1c', style: 'triangle' },
    { key: 'medium', label: 'Moderate', radius: 5, color: '#d97706', style: 'rect' },
    { key: 'minor',  label: 'Routine', radius: 3, color: '#78716c', style: 'circle' },
  ];

  let ctx = null;
  let chart = null;
  let current = null;     // last timeline payload
  let lane = 'all';       // all | major | financials
  let preset = '2Y';
  let loading = false;

  function el(id) { return document.getElementById(id); }

  function setStatus(msg, isError) {
    const box = el('timeline-status');
    if (!box) return;
    box.textContent = msg || '';
    box.style.display = msg ? 'block' : 'none';
    box.classList.toggle('timeline-status-error', !!isError);
  }

  function windowParams() {
    const years = PRESETS[preset];
    const cov = ctx.detail.coverage || {};
    const to = cov.latestFilingDate || '';
    if (!years) return to ? 'from=' + encodeURIComponent(cov.earliestFilingDate || '') + '&to=' + encodeURIComponent(to) : '';
    if (!to) return '';
    const d = new Date(to + 'T00:00:00Z');
    d.setUTCFullYear(d.getUTCFullYear() - years);
    return 'from=' + d.toISOString().slice(0, 10) + '&to=' + encodeURIComponent(to);
  }

  async function load() {
    if (loading) return;
    loading = true;
    setStatus('Loading price history…');
    try {
      const qs = windowParams();
      current = await ctx.apiFetch('/api/companies/' + encodeURIComponent(ctx.cik) +
        '/timeline' + (qs ? '?' + qs : ''));
      render();
    } catch (err) {
      setStatus('Could not load the timeline: ' + err.message, true);
    } finally {
      loading = false;
    }
  }

  function render() {
    if (!current) return;

    const labels = current.prices.map(function (p) { return p.date; });
    const series = current.prices.map(function (p) { return p.adjClose || p.close; });

    // A filing can land on a non-trading day (measured: 1 of 294 — Good Friday).
    // Snap forward to the next trading day so a filing never appears to precede
    // itself; drop events after the last bar.
    const indexByDate = {};
    labels.forEach(function (d, i) { indexByDate[d] = i; });
    function snapIndex(date) {
      if (indexByDate[date] !== undefined) return indexByDate[date];
      for (let i = 0; i < labels.length; i++) if (labels[i] >= date) return i;
      return -1;
    }

    const visible = current.events.filter(function (e) {
      if (lane === 'major') return e.weight === 'major';
      if (lane === 'financials') return !!e.why;   // curated events carry why text
      return true;
    });

    const datasets = [{
      type: 'line',
      label: current.ticker ? current.ticker + ' close' : 'Price',
      data: series,
      borderColor: '#7ac0f5',
      backgroundColor: 'rgba(122,192,245,.15)',
      borderWidth: 2,
      pointRadius: 0,
      tension: 0,
      spanGaps: true,
      order: 10,
    }];

    WEIGHTS.forEach(function (w) {
      const pts = [];
      visible.forEach(function (e) {
        if (e.weight !== w.key) return;
        const i = snapIndex(e.filingDate);
        if (i < 0 || series[i] == null) return;
        pts.push({ x: i, y: series[i], ev: e });
      });
      datasets.push({
        type: 'scatter',
        label: w.label + ' filings',
        data: pts,
        backgroundColor: w.color,
        borderColor: w.color,
        pointRadius: w.radius,
        pointHoverRadius: w.radius + 3,
        pointStyle: w.style,
        showLine: false,
        order: 1,
      });
    });

    const canvas = el('timeline-chart');
    if (chart) { chart.destroy(); chart = null; }

    if (!labels.length && !visible.length) {
      setStatus('No filings or prices in this window.');
      return;
    }

    chart = new Chart(canvas.getContext('2d'), {
      data: { labels: labels, datasets: datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        interaction: { mode: 'nearest', intersect: true },
        scales: {
          // Category axis: Chart.js 4 has no bundled date adapter.
          x: { type: 'category', ticks: { maxTicksLimit: 10, autoSkip: true }, grid: { display: false } },
          y: { title: { display: true, text: 'Adjusted close' } },
        },
        plugins: {
          legend: { position: 'bottom', labels: { usePointStyle: true, boxWidth: 8 } },
          tooltip: {
            callbacks: {
              // Canvas-rendered text: no HTML, no injection surface. Summaries
              // are free text from meta.json, so they are truncated.
              title: function (items) {
                const it = items[0];
                if (it && it.raw && it.raw.ev) return it.raw.ev.filingDate;
                return labels[it ? it.dataIndex : 0] || '';
              },
              label: function (item) {
                const ev = item.raw && item.raw.ev;
                if (!ev) return 'Close: ' + Number(item.parsed.y).toFixed(2);
                const out = [ev.form + ' · ' + String(ev.category || '').replace(/_/g, ' ')];
                if (ev.tierLabel) out.push(ev.tierLabel);
                if (ev.summary) {
                  const s = String(ev.summary);
                  out.push(s.length > 140 ? s.slice(0, 140) + '…' : s);
                }
                return out;
              },
            },
          },
        },
      },
    });

    // Status line: which half of the data, if any, is missing.
    if (!current.ticker) {
      setStatus('No ticker on file for this company — showing filing events only.');
    } else if (!labels.length) {
      const note = (current.priceCoverage && current.priceCoverage.note) || 'no price data available';
      setStatus('Price history unavailable (' + note + ') — showing filing events only.', true);
    } else {
      setStatus('');
    }

    const counts = el('timeline-counts');
    if (counts) {
      counts.textContent = current.prices.length + ' trading days · ' +
        visible.length + ' of ' + current.events.length + ' filings shown';
    }
  }

  function wireControls() {
    Object.keys(PRESETS).forEach(function (p) {
      const b = el('tl-preset-' + p);
      if (!b) return;
      b.addEventListener('click', function () {
        preset = p;
        Object.keys(PRESETS).forEach(function (o) {
          const ob = el('tl-preset-' + o);
          if (ob) ob.classList.toggle('active', o === p);
        });
        load();
      });
    });

    const laneSel = el('tl-lane');
    if (laneSel) {
      laneSel.addEventListener('change', function () {
        lane = laneSel.value;
        render();   // client-side only; no request
      });
    }
  }

  CompanyTabs.register('timeline', {
    init: function (c) {
      ctx = c;
      wireControls();
      load();
    },
    show: function () {
      // Chart.js cannot size a canvas inside display:none.
      if (chart) chart.resize();
    },
  });
})();
