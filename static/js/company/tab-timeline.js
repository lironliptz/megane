/**
 * tab-timeline.js — stock chart (price + volume + general filing markers) and
 * a separate, smaller financial chart (reported revenue) stacked above it.
 *
 * Two independent Chart.js instances on two <canvas> elements, not two scales
 * sharing one canvas — the earlier single-canvas "financial lane + yRevenue
 * overlay" design was scrapped: financial figures must never render on the
 * stock chart at all, and the two charts must read as visually separate,
 * not as bands of one chart.
 *
 * Chart.js notes that are load-bearing here:
 *
 *  - The x-axis is a CATEGORY axis, not a time axis. Chart.js 4 needs a date
 *    adapter for `type: 'time'` and none is bundled (only the stub that throws),
 *    so labels are the server's YYYY-MM-DD strings. Event markers use object
 *    notation ({x, y}) and MUST set x to one of those date strings — the
 *    category scale resolves object data by matching x against `labels`
 *    (CategoryScale.parse), not by array index. A numeric index there was a
 *    real bug: every marker collapsed toward the same position.
 *  - Price uses `y` (left, adjusted close); volume uses `yVolume` (right, share
 *    count) — separate scales and vertical bands, never mixed on one axis.
 *  - A layout plugin (`applySplitLayout`) splits the stock canvas into two
 *    bands: price + events (top) and volume (bottom, ~17% linear / ~33% log).
 *    With no volume data the price/events band is the full chart, unclipped.
 *  - Event markers use the hidden `yEvents` axis — two lanes: financial
 *    reports (top, blue squares) and general filings (below, by weight).
 *    Revenue dollar amounts are NOT on this canvas — only on the financial
 *    graph above (renderFinancialChart).
 *  - **Both charts share the same `labels` array and an identical fixed
 *    y-axis pixel width (`afterFit`)** so their category positions land at
 *    the same x pixel — this is what makes "same x-axis" true across two
 *    independent Chart.js layouts, not just "same date range."
 *  - Chart.js cannot measure a canvas inside display:none, so a chart built
 *    while the tab is hidden renders 0x0 until resize() runs on show() —
 *    true for both canvases.
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
  let finChart = null;  // separate financial graph above the stock chart
  let current = null;     // last timeline payload
  let preset = '2Y';
  let loading = false;
  let rangeSyncing = false;
  let suppressNextClick = false;   // set by installDragSelect; skips the click a shift-drag ends on

  // ---- event type filter (prompt 7) ----------------------------------
  const FILTER_KEY = 'megane.timelineEventFilter.v1';
  let filterChecked = null;   // "form|category" -> bool; null until first buildFilterTree
  let filterOpen = false;

  const VOLUME_SCALE_KEY = 'megane.timelineVolumeLog.v1';
  let volumeLogScale = false;

  function loadVolumeScalePref() {
    try {
      return localStorage.getItem(VOLUME_SCALE_KEY) === '1';
    } catch (e) {
      return false;
    }
  }

  function saveVolumeScalePref() {
    try {
      localStorage.setItem(VOLUME_SCALE_KEY, volumeLogScale ? '1' : '0');
    } catch (e) {
      // storage blocked — preference lasts this session only
    }
  }

  function filterLeafKey(e) { return e.form + '|' + e.category; }

  // Groups the loaded window's events into Form -> category, counts included.
  // Only pairs actually present in `events` appear — never the full classifier
  // vocabulary (prompt 7 idea file, "only show pairs that exist").
  function buildFilterTree(events) {
    const byForm = {};
    events.forEach(function (e) {
      const f = byForm[e.form] || (byForm[e.form] = { count: 0, cats: {} });
      f.count++;
      f.cats[e.category] = (f.cats[e.category] || 0) + 1;
    });
    return Object.keys(byForm)
      .map(function (form) { return { form: form, count: byForm[form].count, cats: byForm[form].cats }; })
      .sort(function (a, b) { return b.count - a.count || a.form.localeCompare(b.form); });
  }

  function loadStoredFilter() {
    try {
      const raw = localStorage.getItem(FILTER_KEY);
      if (!raw) return {};
      const parsed = JSON.parse(raw);
      return (parsed && parsed.version === 1 && parsed.checked) || {};
    } catch (e) {
      return {};   // corrupt/blocked storage: fall back to defaults
    }
  }

  function saveFilter() {
    try {
      localStorage.setItem(FILTER_KEY, JSON.stringify({ version: 1, checked: filterChecked }));
    } catch (e) {
      // storage full/blocked (e.g. private mode): filter still works this session
    }
  }

  // Merges stored choices onto the tree built from THIS window's events.
  // Unseen leaves default to true (visible) — new categories opt in
  // automatically unless the user explicitly unchecked them before.
  function mergeFilterState(tree) {
    const stored = loadStoredFilter();
    const next = {};
    tree.forEach(function (f) {
      Object.keys(f.cats).forEach(function (cat) {
        const key = f.form + '|' + cat;
        next[key] = key in stored ? !!stored[key] : true;
      });
    });
    filterChecked = next;
  }

  function eventVisible(e) {
    if (!filterChecked) return true;   // tree not built yet (first render): show everything
    const v = filterChecked[filterLeafKey(e)];
    return v === undefined ? true : v;
  }

  // Financial-results lane (prompt 10): every quarterly/annual public report
  // gets its own top-of-chart row, never the general lane too (no duplicates).
  function isFinancialReport(e) {
    return e.category === 'quarterly_results' || e.category === 'annual_report';
  }

  function parseDollarAmount(str) {
    if (!str) return null;
    const m = /\$([\d,.]+)\s*([BMK])?/i.exec(String(str));
    if (!m) return null;
    let n = parseFloat(m[1].replace(/,/g, ''));
    if (isNaN(n)) return null;
    const u = (m[2] || '').toUpperCase();
    if (u === 'B') n *= 1e9;
    else if (u === 'M') n *= 1e6;
    else if (u === 'K') n *= 1e3;
    return n;
  }

  function findMetricAmount(ev, labelRe) {
    const h = ev.highlights;
    if (!h || !h.metrics) return null;
    for (let i = 0; i < h.metrics.length; i++) {
      if (labelRe.test(h.metrics[i].label)) {
        return parseDollarAmount(h.metrics[i].value);
      }
    }
    return null;
  }

  // Unique snapped trading-day labels for visible financial reports — drives
  // the full-height vertical guide lines on both charts.
  function financialReportLabels(labels, financialEvents, snapIndex) {
    const seen = {};
    const out = [];
    financialEvents.forEach(function (e) {
      const i = snapIndex(anchorDateForEvent(e));
      if (i < 0) return;
      const label = labels[i];
      if (seen[label]) return;
      seen[label] = true;
      out.push(label);
    });
    return out;
  }

  function drawFinancialReportLines(chart) {
    const reportLabels = chart._finReportLabels;
    if (!reportLabels || !reportLabels.length) return;
    const xScale = chart.scales.x;
    const area = chart.chartArea;
    if (!xScale || !area || area.bottom <= area.top) return;
    const c2d = chart.ctx;
    c2d.save();
    c2d.strokeStyle = cssVar('--chart-fin-report-line', 'rgba(15, 23, 42, 0.32)');
    c2d.lineWidth = 1.5;
    reportLabels.forEach(function (label) {
      const x = xScale.getPixelForValue(label);
      if (x < area.left - 1 || x > area.right + 1) return;
      c2d.beginPath();
      c2d.moveTo(x, area.top);
      c2d.lineTo(x, area.bottom);
      c2d.stroke();
    });
    c2d.restore();
  }

  function updateFilterBadge(tree) {
    let total = 0;
    let checked = 0;
    tree.forEach(function (f) {
      Object.keys(f.cats).forEach(function (cat) {
        total++;
        if (filterChecked[f.form + '|' + cat]) checked++;
      });
    });
    const badge = el('tl-filter-badge');
    if (badge) badge.textContent = checked + ' / ' + total;
    const btn = el('tl-filter-btn');
    if (btn) btn.classList.toggle('active', checked < total);
  }

  // form/category are classifier-controlled strings, not filing prose, but
  // escHtml costs nothing and keeps the "no unescaped filing text" rule
  // uniform across the panel.
  function renderFilterTree(tree) {
    let html = '';
    tree.forEach(function (f) {
      const cats = Object.keys(f.cats).sort();
      const allOn = cats.every(function (c) { return filterChecked[f.form + '|' + c]; });
      html += '<div class="tl-filter-form">';
      html += '<label><input type="checkbox" data-form="' + escHtml(f.form) + '"' +
        (allOn ? ' checked' : '') + '> ' + escHtml(f.form) + ' (' + f.count + ')</label>';
      cats.forEach(function (cat) {
        const key = f.form + '|' + cat;
        html += '<label class="tl-filter-cat"><input type="checkbox" data-key="' +
          escHtml(key) + '"' + (filterChecked[key] ? ' checked' : '') + '> ' +
          escHtml(cat.replace(/_/g, ' ')) + ' (' + f.cats[cat] + ')</label>';
      });
      html += '</div>';
    });
    const host = el('tl-filter-tree');
    if (host) host.innerHTML = html;
  }

  function setFilterOpen(open) {
    filterOpen = open;
    const panel = el('tl-filter-panel');
    const btn = el('tl-filter-btn');
    if (panel) panel.hidden = !open;
    if (btn) btn.setAttribute('aria-expanded', String(open));
  }

  function el(id) { return document.getElementById(id); }

  function parseDay(dateStr) {
    return Math.floor(new Date(dateStr + 'T00:00:00Z').getTime() / 86400000);
  }

  function formatDay(dayNum) {
    return new Date(dayNum * 86400000).toISOString().slice(0, 10);
  }

  function coverageBounds() {
    const cov = ctx.detail.coverage || {};
    const lo = cov.earliestFilingDate || '';
    const hi = cov.latestFilingDate || '';
    if (!lo || !hi || lo > hi) return null;
    return { lo: lo, hi: hi, minDay: parseDay(lo), maxDay: parseDay(hi) };
  }

  function clearPresetActive() {
    Object.keys(PRESETS).forEach(function (p) {
      const ob = el('tl-preset-' + p);
      if (ob) ob.classList.remove('active');
    });
  }

  function setPresetActive(p) {
    Object.keys(PRESETS).forEach(function (o) {
      const ob = el('tl-preset-' + o);
      if (ob) ob.classList.toggle('active', o === p);
    });
    preset = p;
  }

  function updateRangeLabels(fromDay, toDay) {
    const fromLabel = el('tl-range-from-label');
    const toLabel = el('tl-range-to-label');
    if (fromLabel) fromLabel.textContent = formatDay(fromDay);
    if (toLabel) toLabel.textContent = formatDay(toDay);
  }

  function readRangeDays() {
    const fromInput = el('tl-range-from');
    const toInput = el('tl-range-to');
    if (!fromInput || !toInput) return null;
    let fromDay = +fromInput.value;
    let toDay = +toInput.value;
    if (fromDay > toDay) {
      const tmp = fromDay;
      fromDay = toDay;
      toDay = tmp;
    }
    return { fromDay: fromDay, toDay: toDay };
  }

  function updateBridgePosition() {
    const bridge = el('tl-range-bridge');
    const fromInput = el('tl-range-from');
    const toInput = el('tl-range-to');
    if (!bridge || !fromInput || !toInput) return;

    const min = +fromInput.min || 0;
    const max = +fromInput.max || 100;
    if (max === min) return;

    const fromVal = +fromInput.value;
    const toVal = +toInput.value;

    const pctFrom = (fromVal - min) / (max - min) * 100;
    const pctTo = (toVal - min) / (max - min) * 100;

    bridge.style.left = pctFrom + '%';
    bridge.style.width = (pctTo - pctFrom) + '%';
    bridge.style.display = 'block';
  }

  function setRangeDays(fromDay, toDay, opts) {
    opts = opts || {};
    const bounds = coverageBounds();
    if (!bounds) return;
    fromDay = Math.max(bounds.minDay, Math.min(fromDay, bounds.maxDay));
    toDay = Math.max(bounds.minDay, Math.min(toDay, bounds.maxDay));
    if (fromDay > toDay) {
      if (opts.moved === 'from') toDay = fromDay;
      else fromDay = toDay;
    }

    const fromInput = el('tl-range-from');
    const toInput = el('tl-range-to');
    if (!fromInput || !toInput) return;

    rangeSyncing = true;
    fromInput.value = String(fromDay);
    toInput.value = String(toDay);
    rangeSyncing = false;
    updateRangeLabels(fromDay, toDay);
    updateBridgePosition();
  }

  function rangeForPreset(p) {
    const bounds = coverageBounds();
    if (!bounds) return null;
    let fromDay = bounds.minDay;
    const toDay = bounds.maxDay;
    if (p !== 'All') {
      const years = PRESETS[p];
      const d = new Date(formatDay(toDay) + 'T00:00:00Z');
      d.setUTCFullYear(d.getUTCFullYear() - years);
      fromDay = Math.max(bounds.minDay, parseDay(d.toISOString().slice(0, 10)));
    }
    return { fromDay: fromDay, toDay: toDay };
  }

  function initRangeSliders() {
    const wrap = el('tl-range-wrap');
    const bounds = coverageBounds();
    const fromInput = el('tl-range-from');
    const toInput = el('tl-range-to');
    if (!wrap || !bounds || !fromInput || !toInput) {
      if (wrap) wrap.hidden = true;
      return;
    }

    wrap.hidden = false;
    fromInput.min = toInput.min = String(bounds.minDay);
    fromInput.max = toInput.max = String(bounds.maxDay);

    const initial = rangeForPreset(preset) || bounds;
    setRangeDays(initial.fromDay, initial.toDay);
  }

  function windowParams() {
    const days = readRangeDays();
    if (days) {
      return 'from=' + encodeURIComponent(formatDay(days.fromDay)) +
        '&to=' + encodeURIComponent(formatDay(days.toDay));
    }
    const cov = ctx.detail.coverage || {};
    const to = cov.latestFilingDate || '';
    if (!to) return '';
    return 'from=' + encodeURIComponent(cov.earliestFilingDate || to) +
      '&to=' + encodeURIComponent(to);
  }

  function setStatus(msg, isError) {
    const box = el('timeline-status');
    if (!box) return;
    box.textContent = msg || '';
    box.style.display = msg ? 'block' : 'none';
    box.classList.toggle('timeline-status-error', !!isError);
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

  function cssVar(name, fallback) {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v || fallback;
  }

  function hexToRgba(hex, alpha) {
    if (!hex || hex.charAt(0) !== '#') return 'rgba(37, 99, 235, ' + alpha + ')';
    let h = hex.slice(1);
    if (h.length === 3) h = h.split('').map(function (c) { return c + c; }).join('');
    if (h.length !== 6) return 'rgba(37, 99, 235, ' + alpha + ')';
    const r = parseInt(h.slice(0, 2), 16);
    const g = parseInt(h.slice(2, 4), 16);
    const b = parseInt(h.slice(4, 6), 16);
    return 'rgba(' + r + ',' + g + ',' + b + ',' + alpha + ')';
  }

  function formatVolume(n) {
    const v = Number(n) || 0;
    if (v >= 1e9) return (v / 1e9).toFixed(1) + 'B';
    if (v >= 1e6) return (v / 1e6).toFixed(1) + 'M';
    if (v >= 1e3) return (v / 1e3).toFixed(0) + 'K';
    return String(v);
  }

  function formatVolumeAxis(n) {
    const v = Number(n) || 0;
    if (v >= 1e6) {
      const m = v / 1e6;
      if (Math.abs(m - 2.5) < 1e-6) return '2.5M';
      return (Math.abs(m - Math.round(m)) < 1e-6 ? String(Math.round(m)) : String(m)) + 'M';
    }
    if (v >= 1e3) {
      const k = v / 1e3;
      if (Math.abs(k - 2.5) < 1e-6) return '2.5K';
      return (Math.abs(k - Math.round(k)) < 1e-6 ? String(Math.round(k)) : String(k)) + 'K';
    }
    return String(Math.round(v));
  }

  function formatMoney(n) {
    const v = Number(n) || 0;
    if (v >= 1e9) return '$' + (v / 1e9).toFixed(1) + 'B';
    if (v >= 1e6) return '$' + (v / 1e6).toFixed(1) + 'M';
    if (v >= 1e3) return '$' + (v / 1e3).toFixed(0) + 'K';
    return '$' + String(Math.round(v));
  }

  function formatEPS(n) {
    const v = Number(n) || 0;
    if (Math.abs(v) >= 1) return '$' + v.toFixed(2);
    return '$' + v.toFixed(2);
  }

  function cssNum(name, fallback) {
    const v = parseFloat(cssVar(name, String(fallback)));
    return isNaN(v) ? fallback : v;
  }

  // All financial-graph bar width / spacing / opacity tunables live in
  // static/css/style.css (:root --chart-fin-*). Change them there only.
  function finChartLayout() {
    return {
      metricThickness: cssNum('--chart-fin-bar-metric-thickness-px', 12),
      revenueThickness: cssNum('--chart-fin-bar-revenue-thickness-px', 16),
      quarterlyOpacity: cssNum('--chart-fin-bar-quarterly-opacity', 0.85),
      annualOpacity: cssNum('--chart-fin-bar-annual-opacity', 0.38),
    };
  }

  function finMetricDefs() {
    const lay = finChartLayout();
    return [
      { key: 'net_income', label: 'Net income', match: /net income/i, isFlow: true,
        colorVar: '--chart-net-income', fallback: '#7c3aed',
        dayOffset: cssNum('--chart-fin-offset-net-income', -2),
        axis: 'yFin', barThickness: lay.metricThickness },
      { key: 'eps', label: 'Basic EPS', match: /eps/i, isFlow: true,
        colorVar: '--chart-eps', fallback: '#d97706',
        dayOffset: cssNum('--chart-fin-offset-eps', -3),
        axis: 'yEPS', barThickness: lay.metricThickness },
      { key: 'revenue', label: 'Revenue', match: /revenue/i, isFlow: true,
        colorVar: '--chart-financial-q', fallback: '#3b82f6',
        dayOffset: cssNum('--chart-fin-offset-revenue', 0),
        axis: 'yFin', barThickness: lay.revenueThickness },
      { key: 'cash', label: 'Cash', match: /cash/i, isFlow: false,
        colorVar: '--chart-cash', fallback: '#0891b2',
        dayOffset: cssNum('--chart-fin-offset-cash', 2),
        axis: 'yFin', barThickness: lay.metricThickness },
    ];
  }

  function extractFinMetrics(ev) {
    const out = {};
    finMetricDefs().forEach(function (def) {
      const amt = findMetricAmount(ev, def.match);
      if (amt != null) out[def.key] = amt;
    });
    return out;
  }

  function mergeFinMetricMaps(a, b) {
    const out = Object.assign({}, a);
    Object.keys(b).forEach(function (k) {
      if (out[k] == null || b[k] > out[k]) out[k] = b[k];
    });
    return out;
  }

  function isAnnualPeriod(p) {
    return !!(p && (p.duration === 'P1Y' || p.focus === 'FY'));
  }

  function isQ4Period(p) {
    if (!p) return false;
    if (p.focus === 'Q4') return true;
    return /^Q4\b/i.test(p.label || '');
  }

  function fiscalYearFromPeriod(p) {
    if (!p || !p.label) return null;
    const m = /\b(20\d{2})\b/.exec(p.label);
    return m ? m[1] : null;
  }

  // Chart x-position uses the filing date to align perfectly with the timeline events.
  function anchorDateForEvent(ev) {
    return ev.filingDate;
  }

  function periodSpanLabel(p, isAnnual) {
    if (!p) return isAnnual ? 'FY' : '';
    if (p.label) return p.label;
    return isAnnual ? 'FY' : (p.focus || '');
  }

  // Builds chart clusters. Quarterly filings sit at their period end; annual
  // filings merge onto the Q4 cluster for the same fiscal year when one
  // exists, otherwise stand alone at year-end (common for 20-F filers).
  function buildFinClusters(labels, financialEvents, snapIndex) {
    const byLabel = {};
    const q4ByFY = {};
    const q123ByFY = {};

    // Build q123ByFY from ALL loaded financial events to ensure robust synthesis
    // even when some quarters are outside the currently visible timeline window.
    if (current && current.events) {
      current.events.forEach(function (ev) {
        if (!isFinancialReport(ev)) return;
        const metrics = extractFinMetrics(ev);
        if (!Object.keys(metrics).length) return;
        const p = ev.reportPeriod || null;
        if (isAnnualPeriod(p) || ev.category === 'annual_report' || isQ4Period(p)) {
          return;
        }
        const anchor = anchorDateForEvent(ev);
        const fy = fiscalYearFromPeriod(p) || (p && p.endDate ? p.endDate.slice(0, 4) : anchor.slice(0, 4));
        if (fy) {
          if (!q123ByFY[fy]) q123ByFY[fy] = { _count: 0 };
          q123ByFY[fy]._count++;
          Object.keys(metrics).forEach(function (k) {
            q123ByFY[fy][k] = (q123ByFY[fy][k] || 0) + metrics[k];
          });
        }
      });
    }

    function ensureCluster(i, label, fy) {
      if (!byLabel[label]) {
        byLabel[label] = { i: i, label: label, fy: fy, quarterly: null, annual: null };
      }
      return byLabel[label];
    }

    financialEvents.forEach(function (ev) {
      const metrics = extractFinMetrics(ev);
      if (!Object.keys(metrics).length) return;
      const p = ev.reportPeriod || null;
      const anchor = anchorDateForEvent(ev);
      const i = snapIndex(anchor);
      if (i < 0) return;
      const label = labels[i];
      const fy = fiscalYearFromPeriod(p) || (p && p.endDate ? p.endDate.slice(0, 4) : label.slice(0, 4));
      const slot = { ev: ev, metrics: metrics };

      if (isAnnualPeriod(p) || ev.category === 'annual_report') {
        const q4Cluster = q4ByFY[fy];
        if (q4Cluster) {
          q4Cluster.annual = slot;
        } else {
          ensureCluster(i, label, fy).annual = slot;
        }
        return;
      }

      const cluster = ensureCluster(i, label, fy);
      cluster.quarterly = slot;
      if (isQ4Period(p)) {
        q4ByFY[fy] = cluster;
      }
    });

    // Synthesize missing Q4 metrics from FY and Q1-Q3
    const defs = finMetricDefs();
    Object.keys(byLabel).forEach(function (k) {
      const cluster = byLabel[k];
      if (cluster.annual && !cluster.quarterly && cluster.fy) {
        const q123 = q123ByFY[cluster.fy];
        // Only synthesize if we have exactly 3 quarters of data for this FY
        if (q123 && q123._count === 3) {
          const synth = {};
          defs.forEach(function (def) {
            const annVal = cluster.annual.metrics[def.key];
            if (annVal != null) {
              if (def.isFlow && q123[def.key] != null) {
                synth[def.key] = annVal - q123[def.key];
              } else if (!def.isFlow) {
                synth[def.key] = annVal;
              }
            }
          });
          if (Object.keys(synth).length > 0) {
            cluster.quarterly = { ev: cluster.annual.ev, metrics: synth, synthesized: true };
          }
        }
      }
    });

    return Object.keys(byLabel).map(function (k) { return byLabel[k]; });
  }

  function addFinMetricDataset(datasets, def, clusters, layer, counters, lay) {
    const isAnnual = layer === 'annual';
    const color = cssVar(def.colorVar, def.fallback);
    const pts = [];
    clusters.forEach(function (cluster) {
      const slot = isAnnual ? cluster.annual : cluster.quarterly;
      if (!slot) return;
      const val = slot.metrics[def.key];
      if (val == null) return;

      let yVal = val;
      if (isAnnual && cluster.quarterly) {
        const qVal = cluster.quarterly.metrics[def.key];
        // Stack the annual bar on top of Q4 if they share the same sign
        // and the annual total is larger in magnitude than Q4.
        if (qVal != null && (qVal * val > 0) && Math.abs(val) > Math.abs(qVal)) {
          yVal = [qVal, val];
        }
      }

      const ti = cluster.i + def.dayOffset;
      if (ti < 0 || ti >= counters.labelCount) return;
      if (def.axis === 'yEPS') {
        if (val > counters.epsMax) counters.epsMax = val;
      } else if (val > counters.finMax) {
        counters.finMax = val;
      }
      const p = slot.ev.reportPeriod;
      let pLabel = periodSpanLabel(p, isAnnual);
      let pDur = (p && p.duration) || (isAnnual ? 'P1Y' : 'P3M');
      if (slot.synthesized) {
        pLabel = (pLabel || '').replace('FY', 'Q4');
        pDur = 'P3M';
      }
      pts.push({
        x: counters.labels[ti],
        y: yVal,
        ev: slot.ev,
        metricLabel: def.label,
        reportLabel: cluster.label,
        periodLabel: pLabel,
        isAnnual: isAnnual,
        duration: pDur,
        synthesized: slot.synthesized,
      });
    });
    if (!pts.length) return;
    datasets.push({
      type: 'bar',
      label: def.label + (isAnnual ? ' (FY)' : ''),
      data: pts,
      backgroundColor: hexToRgba(color, isAnnual ? lay.annualOpacity : lay.quarterlyOpacity),
      hoverBackgroundColor: color,
      borderWidth: 0,
      // Chart.js grouped layout shifts each dataset by barThickness×index,
      // which beats the CSS dayOffsets and spaces a cluster unevenly.
      grouped: false,
      barThickness: def.barThickness,
      yAxisID: def.axis,
      order: isAnnual ? 0 : 4,
    });
  }

  // Both charts pin every vertical axis to the same pixel width so their
  // plot areas start and end at identical x positions — this is what makes
  // "same x-axis" true across two independent Chart.js instances.
  const FIN_AXIS_W = 64;
  function fitAxisWidth(scale) {
    scale.width = FIN_AXIS_W;
  }

  // Volume strip: linear ~17%; log uses the bottom third of the chart.
  const VOLUME_BAND_RATIO_LINEAR = 0.17;
  const VOLUME_LOG_BAND_RATIO = 1 / 3;
  const VOLUME_BAND_GAP = 5;
  // Log axis: 1 · 2.5 · 10 · 20 · 50 per decade (1K, 2.5K, 10K …); never above 500M.
  const VOLUME_LOG_STEP_MULTS = [1, 2.5, 10, 20, 50];
  const VOLUME_LOG_AXIS_MAX_MULTIPLIER = 10;
  const VOLUME_LOG_TICK_CAP = 500e6;

  function volumeBandRatio() {
    return volumeLogScale ? VOLUME_LOG_BAND_RATIO : VOLUME_BAND_RATIO_LINEAR;
  }

  function hasVolumeData(volumes) {
    return volumes && volumes.some(function (v) { return v > 0; });
  }

  function volumeDataMax(volumes) {
    let maxVol = 0;
    volumes.forEach(function (v) { if (v > maxVol) maxVol = v; });
    return maxVol;
  }

  function volumeMax(volumes) {
    const maxVol = volumeDataMax(volumes);
    return maxVol > 0 ? maxVol * 1.05 : 1;
  }

  function volumeMinPositive(volumes) {
    let minVol = Infinity;
    volumes.forEach(function (v) {
      if (v > 0 && v < minVol) minVol = v;
    });
    return minVol < Infinity ? minVol * 0.85 : 1;
  }

  function volumeLogLadder(maxCeiling) {
    const seen = {};
    const out = [];
    for (let p = 0; p <= 8; p++) {
      const decade = Math.pow(10, p);
      VOLUME_LOG_STEP_MULTS.forEach(function (m) {
        const v = m * decade;
        if (v < 1 || v > maxCeiling || v > VOLUME_LOG_TICK_CAP) return;
        const key = String(v);
        if (seen[key]) return;
        seen[key] = true;
        out.push(v);
      });
    }
    return out.sort(function (a, b) { return a - b; });
  }

  // Axis top is the next ladder step strictly above data max, capped at 10× max.
  function volumeLogAxisBounds(volumes) {
    const dataMin = volumeMinPositive(volumes);
    const dataMax = volumeDataMax(volumes);
    const cap = Math.min(
      Math.max(dataMax * VOLUME_LOG_AXIS_MAX_MULTIPLIER, dataMax || 1),
      VOLUME_LOG_TICK_CAP
    );
    const ladder = volumeLogLadder(cap);

    let axisMin = ladder[0] || 1;
    for (let i = 0; i < ladder.length; i++) {
      if (ladder[i] <= dataMin) axisMin = ladder[i];
      else break;
    }

    let axisMax = null;
    for (let i = 0; i < ladder.length; i++) {
      if (ladder[i] > dataMax) {
        axisMax = ladder[i];
        break;
      }
    }
    if (!axisMax || axisMax > cap) {
      const above = ladder.filter(function (v) { return v > dataMax && v <= cap; });
      axisMax = above.length ? above[0] : (ladder.filter(function (v) { return v <= cap; }).pop() || cap);
    }

    const ticks = ladder.filter(function (v) {
      return v >= axisMin && v <= axisMax;
    });

    return { min: axisMin, max: axisMax, ticks: ticks, dataMax: dataMax };
  }

  // Shared log → pixel mapping for bars and hand-drawn axis labels (volume band only).
  function volumeLogPixelY(value, layout, bounds) {
    const lo = Math.log(bounds.min);
    const hi = Math.log(bounds.max);
    const span = hi - lo || 1;
    const v = Math.max(Number(value) || 0, bounds.min);
    const t = (Math.log(v) - lo) / span;
    return layout.bottom - t * (layout.bottom - layout.top);
  }

  // Splits chartArea vertically into up to three stacked bands, top to bottom:
  // price + events (top) and volume (bottom). Each scale keeps its own
  // min/max — never mixed. With no volume data, price/events is the full
  // chart, unclipped. No financial content lives on this canvas at all —
  // see the separate financial chart below.
  function applySplitLayout(chart) {
    const area = chart.chartArea;
    if (!area || area.bottom <= area.top) return;

    if (!chart.scales.yVolume) {
      chart._volLayout = null;
      if (chart.scales.y) {
        chart.scales.y.top = area.top;
        chart.scales.y.bottom = area.bottom;
      }
      if (chart.scales.yEvents) {
        chart.scales.yEvents.top = area.top;
        chart.scales.yEvents.bottom = area.bottom;
      }
      return;
    }

    const areaH = area.bottom - area.top;
    const band = Math.round(areaH * volumeBandRatio());
    const priceBottom = area.bottom - band - VOLUME_BAND_GAP;
    const volTop = priceBottom + VOLUME_BAND_GAP;

    chart.scales.y.top = area.top;
    chart.scales.y.bottom = priceBottom;
    chart.scales.yEvents.top = area.top;
    chart.scales.yEvents.bottom = priceBottom;

    chart.scales.yVolume.top = volTop;
    chart.scales.yVolume.bottom = area.bottom;
    chart.scales.yVolume.height = area.bottom - volTop;
    chart._volLayout = { top: volTop, bottom: area.bottom, band: band };

    chart.data.datasets.forEach(function (ds) {
      if (ds.yAxisID === 'yVolume') {
        ds.clip = { top: areaH - band, left: 0, right: 0, bottom: 0 };
      } else if (ds.yAxisID === 'y' || ds.yAxisID === 'yEvents') {
        ds.clip = { top: 0, left: 0, right: 0, bottom: band + VOLUME_BAND_GAP };
      }
    });
  }

  // Bar elements are laid out before scale bands exist — re-map y/base in the volume strip.
  function refitVolumeBars(chart) {
    const scale = chart.scales.yVolume;
    const layout = chart._volLayout;
    const bounds = chart._volLogAxis;
    if (!scale || !layout) return;
    chart.data.datasets.forEach(function (ds, i) {
      if (ds.yAxisID !== 'yVolume') return;
      const meta = chart.getDatasetMeta(i);
      if (!meta || !meta.data) return;
      meta.data.forEach(function (bar, idx) {
        const vol = Number(ds.data[idx]) || 0;
        bar.base = layout.bottom;
        if (vol <= 0) {
          bar.y = bar.base;
          bar.height = 0;
          bar.skip = true;
          return;
        }
        bar.skip = false;
        if (volumeLogScale && bounds) {
          bar.y = volumeLogPixelY(vol, layout, bounds);
        } else {
          bar.base = scale.bottom;
          bar.y = scale.getPixelForValue(vol);
        }
        bar.height = bar.base - bar.y;
      });
    });
  }

  function drawVolumeLogAxisLabels(chart) {
    const bounds = chart._volLogAxis;
    const layout = chart._volLayout;
    if (!volumeLogScale || !bounds || !layout || !bounds.ticks.length) return;
    applySplitLayout(chart);
    const c2d = chart.ctx;
    const volumeColor = cssVar('--chart-volume', '#94a3b8');
    const labelX = chart.width - 6;
    const minGap = 13;
    const placed = [];
    bounds.ticks.forEach(function (v, i) {
      const y = volumeLogPixelY(v, layout, bounds);
      if (y < layout.top - 2 || y > layout.bottom + 2) return;
      const last = placed.length ? placed[placed.length - 1] : null;
      const isEdge = i === 0 || i === bounds.ticks.length - 1;
      if (last && !isEdge && Math.abs(y - last.y) < minGap) return;
      if (last && isEdge && Math.abs(y - last.y) < minGap * 0.6) {
        placed[placed.length - 1] = { v: v, y: y };
        return;
      }
      placed.push({ v: v, y: y });
    });
    c2d.save();
    c2d.font = '11px "IBM Plex Sans", sans-serif';
    c2d.fillStyle = volumeColor;
    c2d.textAlign = 'right';
    c2d.textBaseline = 'middle';
    placed.forEach(function (item) {
      c2d.fillText(formatVolumeAxis(item.v), labelX, item.y);
    });
    c2d.restore();
  }

  const volumeLanePlugin = {
    id: 'volumeLane',
    beforeDraw: function (chart) {
      applySplitLayout(chart);
    },
    afterLayout: function (chart) {
      applySplitLayout(chart);
    },
    beforeDatasetsDraw: function (chart) {
      applySplitLayout(chart);
      refitVolumeBars(chart);
      const volLayout = chart._volLayout;
      if (!volLayout) return;
      const area = chart.chartArea;
      const c2d = chart.ctx;
      c2d.save();
      c2d.strokeStyle = cssVar('--border', '#d3d3d3');
      c2d.lineWidth = 1;
      c2d.beginPath();
      c2d.moveTo(area.left, volLayout.top - VOLUME_BAND_GAP * 0.5);
      c2d.lineTo(area.right, volLayout.top - VOLUME_BAND_GAP * 0.5);
      c2d.stroke();
      c2d.restore();
    },
    afterDraw: function (chart) {
      drawVolumeLogAxisLabels(chart);
    },
  };
  Chart.register(volumeLanePlugin);

  // ---- period stripes (prompt 7 P1) -----------------------------------
  // Returns an array of strictly-increasing INDEXES into `labels` marking
  // calendar-period starts, snapped forward to the first trading-day label
  // on/after each boundary (same snap philosophy as snapIndex in render()),
  // always including index 0 and the last index so bands cover the full
  // chart width.
  function periodBoundaries(labels, yearly) {
    if (!labels.length) return [];
    const firstDate = new Date(labels[0] + 'T00:00:00Z');
    const lastDate = new Date(labels[labels.length - 1] + 'T00:00:00Z');
    const starts = [];
    if (yearly) {
      for (let y = firstDate.getUTCFullYear(); y <= lastDate.getUTCFullYear(); y++) {
        starts.push(y + '-01-01');
      }
    } else {
      let y = firstDate.getUTCFullYear();
      let m = firstDate.getUTCMonth();
      const endY = lastDate.getUTCFullYear();
      const endM = lastDate.getUTCMonth();
      while (y < endY || (y === endY && m <= endM)) {
        starts.push(y + '-' + String(m + 1).padStart(2, '0') + '-01');
        m++;
        if (m > 11) { m = 0; y++; }
      }
    }
    const idx = [0];
    starts.forEach(function (d) {
      for (let i = 0; i < labels.length; i++) {
        if (labels[i] >= d) {
          if (idx[idx.length - 1] !== i) idx.push(i);
          break;
        }
      }
    });
    if (idx[idx.length - 1] !== labels.length - 1) idx.push(labels.length - 1);
    return idx;
  }

  // Very subtle month/year bands behind price + events, so long windows are
  // easier to scan. Reads `chart.data.labels` at draw time (not a module-scope
  // `labels` closure) so it stays correct across chart rebuilds in render().
  const periodStripesPlugin = {
    id: 'periodStripes',
    beforeDatasetsDraw: function (chart) {
      const chartLabels = chart.data.labels;
      if (!chartLabels || chartLabels.length < 2) return;   // no prices: skip stripes
      const spanDays = parseDay(chartLabels[chartLabels.length - 1]) - parseDay(chartLabels[0]);
      const yearly = spanDays > 730;   // > 2 years
      const boundaries = periodBoundaries(chartLabels, yearly);
      if (boundaries.length < 2) return;
      const area = chart.chartArea;
      const xScale = chart.scales.x;
      const c2d = chart.ctx;
      // Stripes only behind price + events — not the volume band.
      const volLayout = chart._volLayout;
      const stripeTop = area.top;
      const stripeBottom = volLayout ? volLayout.top - VOLUME_BAND_GAP : area.bottom;
      if (stripeBottom <= stripeTop) return;
      c2d.save();
      for (let i = 0; i < boundaries.length - 1; i++) {
        c2d.fillStyle = (i % 2 === 0)
          ? cssVar('--chart-period-a', 'rgba(15, 23, 42, 0.025)')
          : cssVar('--chart-period-b', 'rgba(15, 23, 42, 0.055)');
        const x0 = xScale.getPixelForValue(boundaries[i]);
        const x1 = xScale.getPixelForValue(boundaries[i + 1]);
        c2d.fillRect(x0, stripeTop, x1 - x0, stripeBottom - stripeTop);
      }
      c2d.restore();
    },
  };
  Chart.register(periodStripesPlugin);

  // Full-height vertical guides at each financial-report trading day (both charts).
  const financialReportLinesPlugin = {
    id: 'financialReportLines',
    beforeDatasetsDraw: function (chart) {
      drawFinancialReportLines(chart);
    },
  };
  Chart.register(financialReportLinesPlugin);

  // Custom legend rows below the charts (the canvases' own legends are off):
  // tl-legend-stock lists the stock chart's datasets, tl-legend-fin is filled
  // by renderFinancialChart. Swatch colors come from our own cssVar/WEIGHTS
  // constants; labels are escaped like any other inserted text.
  function renderChartLegend(datasets) {
    const host = el('tl-legend-stock');
    if (!host) return;
    let html = '';
    datasets.forEach(function (ds) {
      let swatch = 'timeline-legend-swatch-square';
      let color = ds.borderColor || ds.backgroundColor;
      if (ds.type === 'line') {
        swatch = 'timeline-legend-swatch-line';
        color = ds.borderColor;
      } else if (ds.type === 'scatter') {
        swatch = ds.pointStyle === 'triangle' ? 'timeline-legend-swatch-triangle'
          : ds.pointStyle === 'circle' ? 'timeline-legend-swatch-circle'
          : 'timeline-legend-swatch-square';
        color = ds.backgroundColor;
      }
      if (!color || !ds.label) return;
      html += '<span class="timeline-legend-item">' +
        '<span class="timeline-legend-swatch ' + swatch + '" style="color:' + color + '"></span>' +
        escHtml(ds.label) + '</span>';
    });
    host.innerHTML = html;
  }

  // Financial graph: headline metrics per report cluster. Quarterly (P3M) bars
  // are solid; full-year (P1Y) bars are outlined/semi-transparent and merge
  // onto the Q4 cluster when a Q4 filing exists for that fiscal year.
  function renderFinancialChart(labels, financialEvents, snapIndex, showVolume, finReportLabels) {
    const wrap = el('tl-financial-wrap');
    const canvas = el('timeline-chart-financial');
    const legendHost = el('tl-legend-fin');
    if (finChart) { finChart.destroy(); finChart = null; }
    if (!wrap || !canvas) return;

    const clusters = buildFinClusters(labels, financialEvents, snapIndex);

    const datasets = [];
    const lay = finChartLayout();
    const counters = { finMax: 0, epsMax: 0, labelCount: labels.length, labels: labels };
    finMetricDefs().forEach(function (def) {
      addFinMetricDataset(datasets, def, clusters, 'quarterly', counters, lay);
      addFinMetricDataset(datasets, def, clusters, 'annual', counters, lay);
    });

    if (!datasets.length) {
      wrap.hidden = true;
      if (legendHost) legendHost.innerHTML = '';
      return;
    }
    wrap.hidden = false;

    const finMax = counters.finMax;
    const epsMax = counters.epsMax;
    const finColor = cssVar('--chart-financial-q', '#3b82f6');
    const epsColor = cssVar('--chart-eps', '#d97706');
    const showFinAxis = finMax > 0;
    const hasEps = epsMax > 0;
    const scales = {
      x: { type: 'category', offset: true, display: false, ticks: { display: false }, grid: { display: false } },
      yFin: {
        type: 'linear',
        position: 'left',
        beginAtZero: true,
        min: 0,
        max: showFinAxis ? finMax * 1.08 : 1,
        afterFit: fitAxisWidth,
        title: { display: showFinAxis, text: 'Reported ($)', color: finColor },
        ticks: {
          display: showFinAxis,
          maxTicksLimit: 4,
          color: finColor,
          callback: function (v) { return formatMoney(v); },
        },
        grid: { display: showFinAxis, drawOnChartArea: showFinAxis },
        border: { display: showFinAxis },
      },
    };

    if (hasEps) {
      scales.yEPS = {
        type: 'linear',
        position: 'right',
        beginAtZero: true,
        min: 0,
        max: epsMax * 1.25,
        afterFit: fitAxisWidth,
        title: { display: true, text: 'EPS', color: epsColor },
        ticks: {
          maxTicksLimit: 4,
          color: epsColor,
          callback: function (v) { return formatEPS(v); },
        },
        grid: { display: false, drawOnChartArea: false },
      };
    } else if (showVolume) {
      scales.yFinPad = {
        type: 'linear',
        position: 'right',
        afterFit: fitAxisWidth,
        border: { display: false },
        ticks: { display: false },
        grid: { display: false, drawOnChartArea: false },
      };
    }

    finChart = new Chart(canvas.getContext('2d'), {
      type: 'bar',
      data: { labels: labels, datasets: datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        datasets: { bar: { grouped: false } },
        layout: { autoPadding: false, padding: { top: 2, bottom: 2, left: 0, right: 16 } },
        interaction: { mode: 'nearest', intersect: true },
        onClick: function (evt, elements) {
          if (suppressNextClick) { suppressNextClick = false; return; }   // shift-drag just ended here
          if (!elements.length) return;
          const el0 = elements[0];
          const pt = finChart.data.datasets[el0.datasetIndex].data[el0.index];
          if (pt && pt.ev) openEventModal(pt.ev);
        },
        scales: scales,
        plugins: {
          legend: { display: false },
          tooltip: {
            callbacks: {
              title: function (items) {
                const raw = items[0] && items[0].raw;
                if (!raw) return '';
                const parts = [];
                if (raw.periodLabel) parts.push(raw.periodLabel);
                if (raw.duration === 'P1Y') parts.push('12 mo');
                else if (raw.duration === 'P3M') parts.push('3 mo');
                if (raw.reportLabel && raw.reportLabel !== raw.periodLabel) {
                  parts.push('filed ' + raw.ev.filingDate);
                }
                return parts.join(' · ') || (raw.ev ? raw.ev.filingDate : '');
              },
              label: function (item) {
                const raw = item.raw;
                const ds = finChart.data.datasets[item.datasetIndex];
                const isEps = ds.yAxisID === 'yEPS';
                // For floating bars (annual stacked on Q4), item.parsed.y is the top value.
                // We want to show the total FY value, which is item.parsed.y.
                // But wait, if it's a floating bar, item.parsed.y might be the array?
                // In Chart.js, item.parsed.y is the top of the bar.
                let val = item.parsed.y;
                if (raw && raw.y && Array.isArray(raw.y)) val = raw.y[1];
                const fmt = isEps ? formatEPS(val) : formatMoney(val);
                const name = (raw && raw.metricLabel) || ds.label;
                const out = [name + ': ' + fmt];
                if (raw && raw.synthesized) out[0] += ' (implied)';
                if (raw && raw.isAnnual) out.push('Full fiscal year');
                else if (raw && raw.duration === 'P3M') out.push('Quarter');
                if (raw && raw.ev) {
                  out.push(raw.ev.form + ' · ' +
                    String(raw.ev.category || '').replace(/_/g, ' '));
                }
                return out;
              },
            },
          },
        },
      },
    });

    finChart._finReportLabels = finReportLabels || [];

    if (legendHost) {
      let html = '';
      datasets.forEach(function (ds) {
        const color = ds.hoverBackgroundColor || ds.backgroundColor;
        html += '<span class="timeline-legend-item">' +
          '<span class="timeline-legend-swatch timeline-legend-swatch-square" style="color:' +
          color + '"></span>' + escHtml(ds.label) + '</span>';
      });
      legendHost.innerHTML = html;
    }
  }

  function render() {
    if (!current) return;

    if (current.window && current.window.from && current.window.to) {
      setRangeDays(parseDay(current.window.from), parseDay(current.window.to));
    }

    const labels = current.prices.map(function (p) { return p.date; });
    const series = current.prices.map(function (p) { return p.adjClose || p.close; });
    const volumes = current.prices.map(function (p) { return p.volume || 0; });
    const showVolume = hasVolumeData(volumes);

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

    const tree = buildFilterTree(current.events);
    if (!filterChecked) {
      mergeFilterState(tree);   // first load: merge stored choices + defaults
    } else {
      // A later render (e.g. widening the preset from 2Y to All) can surface
      // leaves never seen before. Same rule as the first-load merge: default
      // to visible. Without this, eventVisible() (which already treats an
      // unknown leaf as visible) would disagree with the badge/checkboxes
      // (which read filterChecked[key] directly and treat undefined as
      // unchecked) — found via the verification harness's widen-mid-session
      // case, not called out in the LLD.
      tree.forEach(function (f) {
        Object.keys(f.cats).forEach(function (cat) {
          const key = f.form + '|' + cat;
          if (!(key in filterChecked)) filterChecked[key] = true;
        });
      });
    }
    const visible = current.events.filter(eventVisible);
    const financialEvents = visible.filter(isFinancialReport);
    const generalEvents = visible.filter(function (e) { return !isFinancialReport(e); });

    const priceColor = cssVar('--chart-price', '#2563eb');
    const volumeColor = cssVar('--chart-volume', '#94a3b8');
    const datasets = [{
      type: 'line',
      label: current.ticker ? current.ticker + ' close' : 'Price',
      data: series,
      yAxisID: 'y',
      xAxisID: 'x',
      borderColor: priceColor,
      backgroundColor: hexToRgba(priceColor, 0.18),
      borderWidth: 2,
      pointRadius: 0,
      tension: 0,
      spanGaps: true,
      fill: 'start',
      order: 10,
    }];

    if (showVolume) {
      datasets.push({
        type: 'bar',
        label: 'Volume',
        data: volumes,
        yAxisID: 'yVolume',
        xAxisID: 'x',
        backgroundColor: hexToRgba(volumeColor, 0.82),
        hoverBackgroundColor: hexToRgba(volumeColor, 0.95),
        borderWidth: 0,
        barPercentage: 0.82,
        categoryPercentage: 1,
        order: 5,
      });
    }

    // Two marker lanes on the hidden `yEvents` axis (0 = bottom, 1 = top):
    // financial reports (top, squares) and general filings (below, by weight).
    const FINANCIAL_LANE_BASE_Y = 0.98;
    const FINANCIAL_LANE_MAX_SPREAD = 0.05;
    const FINANCIAL_LANE_STEP = 0.012;

    const GENERAL_LANE_BASE_Y = 0.93;
    const GENERAL_LANE_MAX_SPREAD = 0.08;
    const GENERAL_LANE_STEP = 0.018;

    function laneY(base, maxSpread, step, slot, count) {
      if (count <= 1) return base;
      const s = Math.min(step, maxSpread / (count - 1));
      return base - slot * s;
    }

    const finColorQ = cssVar('--chart-financial-q', '#3b82f6');
    const finColorA = cssVar('--chart-financial-a', '#1e3a8a');
    const FINANCIAL_CADENCES = [
      { key: 'quarterly_results', label: 'Quarterly report', color: finColorQ },
      { key: 'annual_report', label: 'Annual report', color: finColorA },
    ];

    const finGroups = {};
    financialEvents.forEach(function (e) {
      const i = snapIndex(e.filingDate);
      if (i < 0) return;
      (finGroups[labels[i]] = finGroups[labels[i]] || []).push({ e: e, i: i });
    });
    Object.keys(finGroups).forEach(function (label) {
      finGroups[label].sort(function (a, b) {
        return a.e.category.localeCompare(b.e.category);
      });
    });

    FINANCIAL_CADENCES.forEach(function (cad) {
      const pts = [];
      Object.keys(finGroups).forEach(function (label) {
        const g = finGroups[label];
        g.forEach(function (item, slot) {
          if (item.e.category !== cad.key) return;
          const y = laneY(FINANCIAL_LANE_BASE_Y, FINANCIAL_LANE_MAX_SPREAD,
            FINANCIAL_LANE_STEP, slot, g.length);
          pts.push({ x: label, y: y, ev: item.e, dayCount: g.length });
        });
      });
      if (!pts.length) return;
      datasets.push({
        type: 'scatter',
        label: cad.label,
        data: pts,
        backgroundColor: cad.color,
        borderColor: cad.color,
        pointRadius: 5,
        pointHoverRadius: 8,
        pointStyle: 'rect',
        showLine: false,
        yAxisID: 'yEvents',
        order: 0,
      });
    });

    const weightRank = { major: 0, medium: 1, minor: 2 };
    const groups = {};   // label -> [{e, i}]
    generalEvents.forEach(function (e) {
      const i = snapIndex(e.filingDate);
      if (i < 0 || series[i] == null) return;
      (groups[labels[i]] = groups[labels[i]] || []).push({ e: e, i: i });
    });
    Object.keys(groups).forEach(function (label) {
      groups[label].sort(function (a, b) {
        return weightRank[a.e.weight] - weightRank[b.e.weight];
      });
    });

    WEIGHTS.forEach(function (w) {
      const pts = [];
      Object.keys(groups).forEach(function (label) {
        const g = groups[label];
        g.forEach(function (item, slot) {
          if (item.e.weight !== w.key) return;
          const y = laneY(GENERAL_LANE_BASE_Y, GENERAL_LANE_MAX_SPREAD, GENERAL_LANE_STEP, slot, g.length);
          // x must be the category LABEL (the date string), not the array index —
          // Chart.js's category scale resolves object-notation points by matching
          // `x` against `labels` (CategoryScale.parse: `labels[e]===x`).
          pts.push({ x: label, y: y, ev: item.e, dayCount: g.length });
        });
      });
      const dense = pts.some(function (p) { return p.dayCount > 5; });
      datasets.push({
        type: 'scatter',
        label: w.label + ' filings',
        data: pts,
        backgroundColor: w.color,
        borderColor: w.color,
        pointRadius: (w.key === 'minor' && dense) ? Math.max(w.radius - 2, 2) : w.radius,
        pointHoverRadius: w.radius + 3,
        pointStyle: w.style,
        showLine: false,
        yAxisID: 'yEvents',
        order: 1,
      });
    });

    const canvas = el('timeline-chart');
    if (chart) { chart.destroy(); chart = null; }

    if (!labels.length && !visible.length) {
      if (finChart) { finChart.destroy(); finChart = null; }
      const finWrap = el('tl-financial-wrap');
      if (finWrap) finWrap.hidden = true;
      setStatus('No filings or prices in this window.');
      return;
    }

    const scales = {
      x: { type: 'category', offset: true, ticks: { maxTicksLimit: 10, autoSkip: true }, grid: { display: false } },
      y: {
        type: 'linear',
        position: 'left',
        afterFit: fitAxisWidth,
        title: { display: true, text: 'Adjusted close', color: priceColor },
        ticks: { maxTicksLimit: 6, color: priceColor },
        grid: { drawOnChartArea: true },
      },
      yEvents: {
        type: 'linear',
        display: false,
        min: 0,
        max: 1,
        reverse: false,
        grid: { display: false },
      },
    };

    let volLogAxis = null;
    if (showVolume) {
      volLogAxis = volumeLogScale ? volumeLogAxisBounds(volumes) : null;
      scales.yVolume = {
        type: volumeLogScale ? 'logarithmic' : 'linear',
        position: 'right',
        afterFit: fitAxisWidth,
        min: volumeLogScale ? volLogAxis.min : 0,
        max: volumeLogScale ? volLogAxis.max : volumeMax(volumes),
        beginAtZero: !volumeLogScale,
        title: {
          display: !volumeLogScale,
          text: volumeLogScale ? 'Volume (log)' : 'Volume',
          color: volumeColor,
          padding: { top: 2, bottom: 0 },
        },
        ticks: {
          display: !volumeLogScale,
          maxTicksLimit: 3,
          color: volumeColor,
          padding: 2,
          callback: function (val) { return formatVolume(val); },
        },
        grid: { display: false, drawOnChartArea: false },
      };
    }

    const showFinGraph = financialEvents.some(function (ev) {
      return Object.keys(extractFinMetrics(ev)).length > 0;
    });

    chart = new Chart(canvas.getContext('2d'), {
      data: { labels: labels, datasets: datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        layout: { autoPadding: false, padding: { top: showFinGraph ? 2 : 8, bottom: 0, left: 0, right: 16 } },
        interaction: { mode: 'nearest', intersect: true },
        // interaction.mode 'nearest' already resolves elements[0] to the
        // closest point, so a dense day's markers stay individually clickable.
        onClick: function (evt, elements) {
          if (suppressNextClick) { suppressNextClick = false; return; }   // shift-drag just ended here
          if (!elements.length) return;
          const el0 = elements[0];
          const ds = chart.data.datasets[el0.datasetIndex];
          const pt = ds.data[el0.index];
          if (pt && pt.ev) openEventModal(pt.ev);
        },
        scales: scales,
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
                if (ev) {
                  const out = [ev.form + ' · ' + String(ev.category || '').replace(/_/g, ' ')];
                  if (ev.tierLabel) out.push(ev.tierLabel);
                  if (ev.summary) {
                    const s = String(ev.summary);
                    out.push(s.length > 140 ? s.slice(0, 140) + '…' : s);
                  }
                  return out;
                }
                const ds = chart.data.datasets[item.datasetIndex];
                if (ds.yAxisID === 'yVolume') {
                  return 'Volume: ' + formatVolume(item.parsed.y);
                }
                return 'Close: ' + Number(item.parsed.y).toFixed(2);
              },
            },
          },
        },
      },
    });

    if (showVolume && volumeLogScale && volLogAxis) {
      chart._volLogAxis = volLogAxis;
    }

    const finReportLabels = financialReportLabels(labels, financialEvents, snapIndex);
    chart._finReportLabels = finReportLabels;

    renderFinancialChart(labels, financialEvents, snapIndex, showVolume, finReportLabels);

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

    renderFilterTree(tree);
    updateFilterBadge(tree);
    renderChartLegend(datasets);

    const volScaleWrap = el('tl-volume-log-wrap');
    const volLogInput = el('tl-volume-log');
    if (volScaleWrap) volScaleWrap.hidden = !showVolume;
    if (volLogInput) volLogInput.checked = volumeLogScale;
  }

  /* ------------------------------------------------------------ event modal */

  // EDGAR's canonical index URL: unpadded CIK, undashed accession in the path,
  // dashed accession in the filename.
  function secFilingUrl(cik, accession) {
    const cikNum = String(Number(cik));
    const noDash = String(accession).replace(/-/g, '');
    return 'https://www.sec.gov/Archives/edgar/data/' + cikNum + '/' + noDash +
      '/' + accession + '-index.htm';
  }

  // Highlights are best-effort (only ~4 of 66 financial-results summaries carry
  // a parseable figure), so "unavailable" is a normal outcome, not an error.
  function renderHighlights(h) {
    if (!h || !h.metrics || !h.metrics.length) {
      return '<p class="muted">Financial highlights unavailable.</p>';
    }
    let html = '<div class="meta-grid">';
    h.metrics.forEach(function (m) {
      html += ctx.metaItem(m.label, m.value + (m.delta ? ' (' + m.delta + ')' : ''));
    });
    html += '</div>';
    return html;
  }

  function openEventModal(ev) {
    const overlay = el('timeline-event-modal');
    if (!overlay) return;
    el('timeline-event-modal-title').textContent = ev.filingDate + ' \u00b7 ' + ev.form;

    const w = WEIGHTS.filter(function (x) { return x.key === ev.weight; })[0];
    let html = '<span class="badge">' + escHtml(w ? w.label : ev.weight) + '</span> ';
    html += '<strong>' + escHtml(String(ev.category || '').replace(/_/g, ' ')) + '</strong>';
    if (ev.tierLabel) html += ' <span class="badge">' + escHtml(ev.tierLabel) + '</span>';
    if (ev.reportPeriod && ev.reportPeriod.label) {
      const span = ev.reportPeriod.duration === 'P1Y' ? '12 mo' : '3 mo';
      html += ' <span class="badge">' + escHtml(ev.reportPeriod.label) +
        ' (' + escHtml(span) + ')</span>';
    }
    if (ev.why) html += '<p class="muted">' + escHtml(ev.why) + '</p>';
    html += '<p>' + escHtml(ev.summary || '\u2014') + '</p>';
    html += renderHighlights(ev.highlights);
    html += '<p><code>' + escHtml(ev.accessionNumber) + '</code></p>';
    html += '<p><a href="' + secFilingUrl(ctx.cik, ev.accessionNumber) +
      '" target="_blank" rel="noopener" class="btn btn-ghost">Open on SEC EDGAR</a></p>';
    el('timeline-event-modal-body').innerHTML = html;
    overlay.classList.add('open');
  }

  function closeEventModal() {
    const overlay = el('timeline-event-modal');
    if (overlay) overlay.classList.remove('open');
  }

  // ---- shift+drag period selection ------------------------------------
  // Holding Shift and dragging across either chart draws a selection band
  // (a plain absolutely-positioned div, not a Chart.js plugin — simplest way
  // to track a live drag without fighting the redraw cycle) and, on mouseup,
  // sets that span as the timeline's period via the same setRangeDays/load
  // path the range sliders already use. Both charts install this the same
  // way and land on the same period, since they share one labels array and
  // an identical x-axis pixel width (see the file header).
  function installDragSelect(canvasId, overlayId, getChart) {
    const canvas = el(canvasId);
    const overlay = el(overlayId);
    if (!canvas || !overlay) return;

    const DRAG_THRESHOLD_PX = 4;
    let dragging = false;
    let startPx = 0;

    function clampToArea(px, area) {
      return Math.max(area.left, Math.min(px, area.right));
    }

    function pixelFromEvent(e, area) {
      const rect = canvas.getBoundingClientRect();
      return clampToArea(e.clientX - rect.left, area);
    }

    // Category scale pixel->value isn't reliably public across Chart.js
    // builds; getPixelForValue(label) is already used elsewhere in this file
    // (drawFinancialReportLines, periodStripesPlugin), so invert it by
    // nearest match instead — this is only run once per drag, at mouseup.
    function pixelToDay(c, px) {
      const scale = c.scales.x;
      const labels = c.data.labels;
      if (!scale || !labels || !labels.length) return null;
      let bestIdx = 0;
      let bestDist = Infinity;
      for (let i = 0; i < labels.length; i++) {
        const d = Math.abs(scale.getPixelForValue(labels[i]) - px);
        if (d < bestDist) { bestDist = d; bestIdx = i; }
      }
      return parseDay(labels[bestIdx]);
    }

    function paintOverlay(x0, x1) {
      overlay.style.left = Math.min(x0, x1) + 'px';
      overlay.style.width = Math.abs(x1 - x0) + 'px';
    }

    canvas.addEventListener('mousedown', function (e) {
      if (!e.shiftKey) return;
      const c = getChart();
      const area = c && c.chartArea;
      if (!c || !area) return;
      e.preventDefault();   // shift+drag must not trigger text/page selection
      dragging = true;
      startPx = pixelFromEvent(e, area);
      canvas.style.cursor = 'crosshair';
      overlay.style.top = area.top + 'px';
      overlay.style.height = (area.bottom - area.top) + 'px';
      paintOverlay(startPx, startPx);
      overlay.hidden = false;
    });

    document.addEventListener('mousemove', function (e) {
      if (!dragging) return;
      const c = getChart();
      const area = c && c.chartArea;
      if (!c || !area) return;
      paintOverlay(startPx, pixelFromEvent(e, area));
    });

    document.addEventListener('mouseup', function (e) {
      if (!dragging) return;
      dragging = false;
      canvas.style.cursor = '';
      overlay.hidden = true;
      const c = getChart();
      if (!c) return;
      const area = c.chartArea || { left: startPx, right: startPx };
      const endPx = pixelFromEvent(e, area);
      if (Math.abs(endPx - startPx) < DRAG_THRESHOLD_PX) return;   // shift-click, not a drag
      const d0 = pixelToDay(c, startPx);
      const d1 = pixelToDay(c, endPx);
      if (d0 == null || d1 == null) return;
      suppressNextClick = true;   // the mouseup that ends this drag also fires a native click
      clearPresetActive();
      preset = 'custom';
      setRangeDays(Math.min(d0, d1), Math.max(d0, d1));
      load();
    });
  }

  function wireControls() {
    Object.keys(PRESETS).forEach(function (p) {
      const b = el('tl-preset-' + p);
      if (!b) return;
      b.addEventListener('click', function () {
        const span = rangeForPreset(p);
        if (span) setRangeDays(span.fromDay, span.toDay);
        setPresetActive(p);
        load();
      });
    });

    ['from', 'to'].forEach(function (which) {
      const input = el('tl-range-' + which);
      if (!input) return;
      input.addEventListener('input', function () {
        if (rangeSyncing) return;
        const fromInput = el('tl-range-from');
        const toInput = el('tl-range-to');
        let fromDay = +fromInput.value;
        let toDay = +toInput.value;
        if (fromDay > toDay) {
          if (which === 'from') toDay = fromDay;
          else fromDay = toDay;
        }
        rangeSyncing = true;
        fromInput.value = String(fromDay);
        toInput.value = String(toDay);
        rangeSyncing = false;
        updateRangeLabels(fromDay, toDay);
        updateBridgePosition();
      });
      input.addEventListener('change', function () {
        if (rangeSyncing) return;
        clearPresetActive();
        preset = 'custom';
        load();
      });
    });

    // Bridge (sliding window) dragging logic
    const bridge = el('tl-range-bridge');
    const fromInput = el('tl-range-from');
    const toInput = el('tl-range-to');
    const slidersContainer = document.querySelector('.timeline-range-sliders');

    if (bridge && fromInput && toInput && slidersContainer) {
      let isDragging = false;
      let startX = 0;
      let startFromVal = 0;
      let startToVal = 0;

      function getEventX(e) {
        if (e.touches && e.touches.length) {
          return e.touches[0].clientX;
        }
        return e.clientX;
      }

      function onDragStart(e) {
        const bounds = coverageBounds();
        if (!bounds) return;

        isDragging = true;
        bridge.classList.add('dragging');
        startX = getEventX(e);
        startFromVal = +fromInput.value;
        startToVal = +toInput.value;

        // Prevent text selection/scrolling while dragging
        e.preventDefault();

        document.addEventListener('mousemove', onDragMove, { passive: false });
        document.addEventListener('mouseup', onDragEnd);
        document.addEventListener('touchmove', onDragMove, { passive: false });
        document.addEventListener('touchend', onDragEnd);
      }

      function onDragMove(e) {
        if (!isDragging) return;

        const bounds = coverageBounds();
        if (!bounds) return;

        const rect = slidersContainer.getBoundingClientRect();
        if (rect.width === 0) return;

        const deltaX = getEventX(e) - startX;
        const daysRange = bounds.maxDay - bounds.minDay;
        const daysPerPixel = daysRange / rect.width;
        const deltaDays = Math.round(deltaX * daysPerPixel);

        let newFrom = startFromVal + deltaDays;
        let newTo = startToVal + deltaDays;
        const span = startToVal - startFromVal;

        if (newFrom < bounds.minDay) {
          newFrom = bounds.minDay;
          newTo = bounds.minDay + span;
        } else if (newTo > bounds.maxDay) {
          newTo = bounds.maxDay;
          newFrom = bounds.maxDay - span;
        }

        rangeSyncing = true;
        fromInput.value = String(newFrom);
        toInput.value = String(newTo);
        rangeSyncing = false;

        updateRangeLabels(newFrom, newTo);
        updateBridgePosition();

        // Prevent default touch behavior (scrolling)
        e.preventDefault();
      }

      function onDragEnd() {
        if (!isDragging) return;

        isDragging = false;
        bridge.classList.remove('dragging');

        document.removeEventListener('mousemove', onDragMove);
        document.removeEventListener('mouseup', onDragEnd);
        document.removeEventListener('touchmove', onDragMove);
        document.removeEventListener('touchend', onDragEnd);

        clearPresetActive();
        preset = 'custom';
        load();
      }

      bridge.addEventListener('mousedown', onDragStart);
      bridge.addEventListener('touchstart', onDragStart, { passive: false });
    }

    // Event type filter: checkbox toggles are live (re-render on every
    // change, no Apply button); one delegated listener covers both the
    // per-form parent checkbox and per-category leaf checkboxes.
    const filterTreeHost = el('tl-filter-tree');
    if (filterTreeHost) {
      filterTreeHost.addEventListener('change', function (e) {
        const t = e.target;
        if (!filterChecked) return;
        if (t.dataset.key) {
          filterChecked[t.dataset.key] = t.checked;
        } else if (t.dataset.form) {
          Object.keys(filterChecked).forEach(function (k) {
            if (k.indexOf(t.dataset.form + '|') === 0) filterChecked[k] = t.checked;
          });
        } else {
          return;
        }
        saveFilter();
        render();   // client-side only; no request
      });
    }

    const filterBtn = el('tl-filter-btn');
    if (filterBtn) filterBtn.addEventListener('click', function () { setFilterOpen(!filterOpen); });

    const filterCloseBtn = el('tl-filter-close');
    if (filterCloseBtn) filterCloseBtn.addEventListener('click', function () { setFilterOpen(false); });

    const filterAllBtn = el('tl-filter-all');
    if (filterAllBtn) {
      filterAllBtn.addEventListener('click', function () {
        if (!filterChecked) return;
        Object.keys(filterChecked).forEach(function (k) { filterChecked[k] = true; });
        saveFilter();
        render();
      });
    }

    const filterNoneBtn = el('tl-filter-none');
    if (filterNoneBtn) {
      filterNoneBtn.addEventListener('click', function () {
        if (!filterChecked) return;
        Object.keys(filterChecked).forEach(function (k) { filterChecked[k] = false; });
        saveFilter();
        render();
      });
    }

    document.addEventListener('click', function (e) {
      if (!filterOpen) return;
      const panel = el('tl-filter-panel');
      const btn = el('tl-filter-btn');
      if ((panel && panel.contains(e.target)) || (btn && btn.contains(e.target))) return;
      setFilterOpen(false);
    });

    // Modal dismissal: backdrop click uses the same target check as
    // analyze.js, plus Escape. No focus-trap (no precedent in this codebase).
    const overlay = el('timeline-event-modal');
    if (overlay) {
      overlay.addEventListener('click', function (e) {
        if (e.target === e.currentTarget) closeEventModal();
      });
      const closeBtn = el('timeline-event-modal-close');
      if (closeBtn) closeBtn.addEventListener('click', closeEventModal);
    }
    document.addEventListener('keydown', function (e) {
      if (e.key !== 'Escape') return;
      closeEventModal();
      if (filterOpen) setFilterOpen(false);
    });

    const volLogInput = el('tl-volume-log');
    if (volLogInput) {
      volLogInput.addEventListener('change', function () {
        volumeLogScale = volLogInput.checked;
        saveVolumeScalePref();
        render();
      });
    }

    installDragSelect('timeline-chart', 'tl-drag-select', function () { return chart; });
    installDragSelect('timeline-chart-financial', 'tl-drag-select-fin', function () { return finChart; });
  }

  CompanyTabs.register('timeline', {
    init: function (c) {
      ctx = c;
      volumeLogScale = loadVolumeScalePref();
      initRangeSliders();
      wireControls();
      load();
    },
    show: function () {
      // Chart.js cannot size a canvas inside display:none.
      if (chart) chart.resize();
      if (finChart) finChart.resize();
    },
  });
})();
