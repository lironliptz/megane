requireAuth();

const POLL_INTERVAL = 3000;
const pollTimers = {};
let insightCharts = [];

function destroyInsightCharts() {
  insightCharts.forEach(ch => { try { ch.destroy(); } catch (e) {} });
  insightCharts = [];
}

const palette = ['#4f8ef7', '#4caf7d', '#f0a946', '#e05c5c', '#a78bfa', '#22d3ee', '#fb7185'];

(async () => {
  try {
    const me = await apiFetch('/auth/me');
    document.getElementById('header-email').textContent = me.email;
    if (me.role === 'admin') {
      document.getElementById('admin-link').style.display = '';
    }
  } catch (err) {
    logout();
    return;
  }
  await loadLLMModels();
  await loadFiles();
})();

async function loadLLMModels() {
  const row = document.getElementById('model-picker-row');
  const sel = document.getElementById('llm-model-select');
  try {
    const data = await apiFetch('/api/files/llm-models');
    if (data.provider !== 'gemini') {
      row.style.display = 'none';
      return;
    }
    row.style.display = 'flex';
    sel.innerHTML = '';
    const optDef = document.createElement('option');
    optDef.value = '';
    optDef.textContent = 'Server default' + (data.default_model ? ' (' + data.default_model + ')' : '');
    sel.appendChild(optDef);
    for (const id of (data.models || [])) {
      const o = document.createElement('option');
      o.value = id;
      o.textContent = id;
      sel.appendChild(o);
    }
    if (data.default_model) {
      for (let i = 0; i < sel.options.length; i++) {
        if (sel.options[i].value === data.default_model) {
          sel.selectedIndex = i;
          break;
        }
      }
    }
  } catch (e) {
    row.style.display = 'none';
  }
}

async function loadFiles() {
  try {
    const projects = await apiFetch('/api/files');
    renderFiles(projects);
    (projects || []).forEach(p => {
      if (isInProgress(p.Status)) startPolling(p.ID);
    });
  } catch (err) {
    showError(err.message);
  }
}

const IN_PROGRESS = new Set(['uploaded', 'converting', 'llm_pending', 'llm_done', 'post_processing']);

function isInProgress(status) {
  return IN_PROGRESS.has(status);
}

function renderFiles(projects) {
  const tbody = document.getElementById('files-tbody');
  if (!projects || projects.length === 0) {
    tbody.innerHTML = '<tr><td colspan="8" class="cell-muted">No files yet — upload above.</td></tr>';
    return;
  }
  tbody.innerHTML = '';
  projects.forEach(p => {
    const tr = document.createElement('tr');
    tr.id = 'row-' + p.ID;
    var uploadedIso = p.CreatedAt ? String(p.CreatedAt) : '';
    tr.setAttribute('data-uploaded-at', encodeURIComponent(uploadedIso));

    tr.appendChild(cellText(String(p.ID)));
    tr.appendChild(cellText(p.OriginalName, false));
    tr.appendChild(cellHtml(p.llm_model || 'default', 'cell-model'));
    tr.appendChild(makeStatusCell(p.ID, p.Status, null));
    tr.appendChild(cellText(formatFileSize(p.file_size)));
    tr.appendChild(cellText(formatDate(p.CreatedAt)));
    var completedIso = p.completed_at ? String(p.completed_at) : '';
    tr.appendChild(cellText(formatProcessingDisplay(uploadedIso, completedIso, p.Status), 'cell-duration'));
    tr.appendChild(makeActionCell(p));

    tbody.appendChild(tr);
  });
}

function cellText(text, esc) {
  if (esc === false) {
    var td = document.createElement('td');
    td.textContent = text == null ? '' : String(text);
    return td;
  }
  var td2 = document.createElement('td');
  td2.textContent = text == null ? '' : String(text);
  return td2;
}

function cellHtml(html, className) {
  var td = document.createElement('td');
  if (className) td.className = className;
  td.textContent = html == null ? '' : String(html);
  return td;
}

function makeStatusCell(projectID, status, statusData) {
  var td = document.createElement('td');
  var opts = {
    projectId: projectID,
    showCancel: true,
    onCancel: cancelProjectProcessing,
    pipelineProgress: statusData && statusData.pipeline_progress != null ? statusData.pipeline_progress : null,
    pipelineStatusLabel: (statusData && statusData.pipeline_status_label) || pipelineStatusLabelFallback(status),
    errorDetail: statusData && statusData.error_detail ? statusData.error_detail : ''
  };
  if (opts.pipelineProgress == null && isInProgress(status)) {
    opts.pipelineProgress = pipelineProgressFallback(status);
  }
  td.appendChild(makePipelineSpinner(status, opts));
  return td;
}

function pipelineProgressFallback(status) {
  var map = {
    uploaded: 8, converting: 28, llm_pending: 45, llm_done: 62, post_processing: 82
  };
  return map[status] != null ? map[status] : null;
}

function pipelineStatusLabelFallback(status) {
  var map = {
    uploaded: 'Queued',
    converting: 'Extracting text',
    llm_pending: 'Analyzing with LLM',
    llm_done: 'LLM complete',
    post_processing: 'Saving results',
    complete: 'Complete',
    error: 'Error'
  };
  return map[status] || String(status || '').replace(/_/g, ' ');
}

function makeActionCell(p) {
  var td = document.createElement('td');
  td.className = 'cell-actions';
  var wrap = document.createElement('div');
  wrap.className = 'row-actions';

  if (p.Status === 'complete') {
    var viewBtn = document.createElement('button');
    viewBtn.type = 'button';
    viewBtn.className = 'btn btn-ghost btn-row';
    viewBtn.textContent = 'Open analysis';
    viewBtn.addEventListener('click', function () { viewResult(p.ID, p.OriginalName); });
    wrap.appendChild(viewBtn);
  }

  var reBtn = document.createElement('button');
  reBtn.type = 'button';
  reBtn.className = 'btn btn-ghost btn-row';
  reBtn.textContent = 'Re-process';
  reBtn.disabled = isInProgress(p.Status);
  reBtn.addEventListener('click', function () { reprocessFile(p.ID); });
  wrap.appendChild(reBtn);

  var delBtn = document.createElement('button');
  delBtn.type = 'button';
  delBtn.className = 'btn btn-danger btn-row';
  delBtn.textContent = 'Delete';
  delBtn.addEventListener('click', function () { deleteFile(p.ID, p.OriginalName); });
  wrap.appendChild(delBtn);

  td.appendChild(wrap);
  return td;
}

async function reprocessFile(projectID) {
  if (!projectID) return;
  try {
    await apiFetch('/api/files/' + projectID + '/reprocess', { method: 'POST' });
    startPolling(projectID);
    await pollStatus(projectID);
  } catch (err) {
    showError(err.message);
  }
}

async function deleteFile(projectID, originalName) {
  if (!projectID) return;
  var label = originalName || ('#' + projectID);
  if (!window.confirm('Delete "' + label + '"? This cannot be undone.')) return;
  stopPolling(projectID);
  try {
    await apiFetch('/api/files/' + projectID, { method: 'DELETE' });
    await loadFiles();
  } catch (err) {
    showError(err.message);
  }
}

async function cancelProjectProcessing(projectID) {
  if (!projectID) return;
  try {
    await apiFetch('/api/files/' + projectID + '/cancel', { method: 'POST' });
    await pollStatus(projectID);
  } catch (err) {
    showError(err.message);
  }
}

function startPolling(projectID) {
  if (pollTimers[projectID]) return;
  pollTimers[projectID] = setInterval(() => pollStatus(projectID), POLL_INTERVAL);
}

function stopPolling(projectID) {
  clearInterval(pollTimers[projectID]);
  delete pollTimers[projectID];
}

async function pollStatus(projectID) {
  try {
    const data = await apiFetch('/api/files/' + projectID + '/status');
    updateRow(projectID, data);
    if (!isInProgress(data.status)) stopPolling(projectID);
  } catch (e) { /* ignore */ }
}

function updateRow(projectID, statusData) {
  const row = document.getElementById('row-' + projectID);
  if (!row) return;
  const newStatus = statusData.status;
  const completedAtIso = statusData.completed_at || '';
  const origName = row.children[1].textContent;
  var uploadedRaw = row.getAttribute('data-uploaded-at');
  var uploadedIso = uploadedRaw ? decodeURIComponent(uploadedRaw) : '';
  const cells = row.querySelectorAll('td');
  var newStatusCell = makeStatusCell(projectID, newStatus, statusData);
  row.replaceChild(newStatusCell, cells[3]);
  cells[6].textContent = formatProcessingDisplay(uploadedIso, completedAtIso, newStatus);
  var newActionCell = makeActionCell({ ID: projectID, Status: newStatus, OriginalName: origName });
  row.replaceChild(newActionCell, cells[7]);
}

async function viewResult(projectID, originalName) {
  destroyInsightCharts();
  try {
    const result = await apiFetch('/api/files/' + projectID + '/result');
    document.getElementById('modal-title').textContent = originalName || ('Analysis #' + projectID);
    renderAnalysis(document.getElementById('modal-body'), result);
    document.getElementById('result-modal').classList.add('open');
  } catch (err) {
    showError(err.message);
  }
}

function renderAnalysis(container, result) {
  if (result == null) {
    container.innerHTML = '<p class="muted">No data.</p>';
    return;
  }
  if (typeof result !== 'object') {
    container.innerHTML = '<pre class="result-pre">' + escHtml(String(result)) + '</pre>';
    return;
  }
  if (result.raw_text != null && result.metadata == null) {
    container.innerHTML = '<p class="muted">Raw model output (no structured schema).</p><pre class="result-pre">' + escHtml(String(result.raw_text)) + '</pre>';
    return;
  }

  const meta = result.metadata || {};
  const insights = Array.isArray(result.key_insights) ? result.key_insights : [];
  const charts = Array.isArray(result.charts) ? result.charts : [];
  const conf = typeof result.confidence === 'number' ? Math.round(result.confidence * 100) : null;

  let html = '';

  html += '<section class="result-section">';
  html += '<h4><span class="section-icon">&#9432;</span> Metadata</h4>';
  html += '<div class="meta-grid">';
  html += metaRow('Category', meta.file_role);
  html += metaRow('Type / purpose', meta.file_kind_summary);
  html += metaRow('Structure', meta.structure_overview);
  html += metaRow('Language', meta.primary_language);
  html += metaRow('Extent', meta.approx_extent);
  html += '</div></section>';

  html += '<section class="result-section">';
  html += '<h4><span class="section-icon">&#128196;</span> Summary</h4>';
  html += '<p class="result-summary">' + escHtml(result.summary || '—') + '</p>';
  if (conf != null) html += '<p class="confidence-bar">Model confidence: <strong>' + conf + '%</strong></p>';
  if (result.extra_notes) html += '<p class="muted">' + escHtml(result.extra_notes) + '</p>';
  html += '</section>';

  html += '<section class="result-section">';
  html += '<h4><span class="section-icon">&#128161;</span> Important information</h4>';
  if (insights.length === 0) {
    html += '<p class="muted">No bullet insights extracted.</p>';
  } else {
    html += '<ul class="insight-list">';
    insights.forEach(s => { html += '<li>' + escHtml(s) + '</li>'; });
    html += '</ul>';
  }
  html += '</section>';

  html += '<section class="result-section">';
  html += '<h4><span class="section-icon">&#128202;</span> Charts</h4>';
  html += '<div id="charts-host" class="charts-grid"></div>';
  html += '</section>';

  container.innerHTML = html;
  renderChartsInto(document.getElementById('charts-host'), charts);
}

function metaRow(label, val) {
  if (!val) return '';
  return '<div class="meta-item"><span class="meta-label">' + escHtml(label) + '</span><span class="meta-value">' + escHtml(val) + '</span></div>';
}

function renderChartsInto(host, charts) {
  if (!charts.length) {
    host.innerHTML = '<p class="muted">No charts — the model only adds these when it finds clear numeric or categorical breakdowns in the text.</p>';
    return;
  }
  if (typeof Chart === 'undefined') {
    host.innerHTML = '<p class="muted">Chart library failed to load.</p>';
    return;
  }

  charts.forEach((spec, idx) => {
    const type = String(spec.type || 'bar').toLowerCase();
    if (!['bar', 'line', 'pie', 'doughnut'].includes(type)) return;

    const labels = spec.labels || [];
    const rawDs = spec.datasets || [];
    if (!labels.length || !rawDs.length) return;

    const wrap = document.createElement('div');
    wrap.className = 'chart-card';
    const title = document.createElement('h5');
    title.className = 'chart-title';
    title.textContent = spec.title || ('Chart ' + (idx + 1));
    const canvas = document.createElement('canvas');
    canvas.id = 'chart-canvas-' + idx + '-' + Math.random().toString(36).slice(2);
    wrap.appendChild(title);
    wrap.appendChild(canvas);
    host.appendChild(wrap);

    const datasets = rawDs.map((ds, di) => {
      const data = (ds.data || []).map(n => {
        const x = Number(n);
        return Number.isFinite(x) ? x : 0;
      });
      const base = palette[di % palette.length];
      if (type === 'pie' || type === 'doughnut') {
        return {
          label: ds.label || 'Values',
          data,
          backgroundColor: labels.map((_, i) => palette[(di + i) % palette.length]),
          borderColor: '#1a1d23',
          borderWidth: 2,
        };
      }
      return {
        label: ds.label || ('Series ' + (di + 1)),
        data,
        backgroundColor: hexAlpha(base, 0.35),
        borderColor: base,
        borderWidth: 1,
        tension: 0.25,
        fill: type === 'line',
      };
    });

    const cfg = {
      type,
      data: { labels, datasets },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        plugins: {
          legend: { labels: { color: '#c9d4e8' } },
        },
        scales: (type === 'pie' || type === 'doughnut') ? {} : {
          x: { ticks: { color: '#8892a4' }, grid: { color: 'rgba(255,255,255,.06)' } },
          y: { ticks: { color: '#8892a4' }, grid: { color: 'rgba(255,255,255,.06)' }, beginAtZero: true },
        },
      },
    };

    insightCharts.push(new Chart(canvas, cfg));
  });

  if (!host.querySelector('.chart-card')) {
    host.innerHTML = '<p class="muted">Chart specs were present but could not be rendered (check labels vs data lengths).</p>';
  }
}

function hexAlpha(hex, a) {
  const h = hex.replace('#', '');
  const r = parseInt(h.slice(0, 2), 16);
  const g = parseInt(h.slice(2, 4), 16);
  const b = parseInt(h.slice(4, 6), 16);
  return 'rgba(' + r + ',' + g + ',' + b + ',' + a + ')';
}

function closeModal() {
  destroyInsightCharts();
  document.getElementById('result-modal').classList.remove('open');
}

document.getElementById('result-modal').addEventListener('click', e => {
  if (e.target === e.currentTarget) closeModal();
});

const dropZone = document.getElementById('drop-zone');
const fileInput = document.getElementById('file-input');
const uploadProg = document.getElementById('upload-progress');

dropZone.addEventListener('click', () => fileInput.click());
dropZone.addEventListener('dragover', e => { e.preventDefault(); dropZone.classList.add('drag-over'); });
dropZone.addEventListener('dragleave', () => dropZone.classList.remove('drag-over'));
dropZone.addEventListener('drop', e => {
  e.preventDefault();
  dropZone.classList.remove('drag-over');
  uploadFiles(e.dataTransfer.files);
});
fileInput.addEventListener('change', () => {
  uploadFiles(fileInput.files);
  fileInput.value = '';
});

async function uploadFiles(fileList) {
  if (!fileList || fileList.length === 0) return;
  const fd = new FormData();
  for (const f of fileList) fd.append('files', f);
  const picker = document.getElementById('model-picker-row');
  const sel = document.getElementById('llm-model-select');
  if (picker && picker.style.display !== 'none' && sel && sel.options.length > 0) {
    fd.append('model', sel.value);
  }
  const i18n = window.I18N || {};
  const uploadingMsg = (i18n.uploading || 'Uploading {n} file(s)…').replace('{n}', fileList.length);
  const queuedMsg   = (i18n.files_queued || '{n} file(s) queued — processing…').replace('{n}', fileList.length);
  setUploadProgress(0, uploadingMsg);
  try {
    const body = await xhrUpload('/api/files/upload', fd, function (pct) {
      setUploadProgress(pct, uploadingMsg);
    });
    if (body.error) throw new Error(body.error);
    setUploadProgress(100, queuedMsg);
    await loadFiles();
    setTimeout(function () { clearUploadProgress(); }, 5000);
  } catch (err) {
    showError(err.message);
    clearUploadProgress();
  }
}

function setUploadProgress(pct, label) {
  var host = document.getElementById('upload-progress');
  if (!host) return;
  host.classList.add('upload-progress-active');
  var bar = host.querySelector('progress');
  if (!bar) {
    host.innerHTML = '';
    bar = document.createElement('progress');
    bar.className = 'upload-progress-bar';
    bar.max = 100;
    host.appendChild(bar);
    var lbl = document.createElement('span');
    lbl.className = 'upload-progress-label';
    host.appendChild(lbl);
  }
  bar.value = Math.max(0, Math.min(100, pct || 0));
  var labelEl = host.querySelector('.upload-progress-label');
  if (labelEl) labelEl.textContent = label || '';
}

function clearUploadProgress() {
  var host = document.getElementById('upload-progress');
  if (!host) return;
  host.classList.remove('upload-progress-active');
  host.innerHTML = '';
}

function xhrUpload(url, formData, onProgress) {
  return new Promise(function (resolve, reject) {
    var xhr = new XMLHttpRequest();
    xhr.open('POST', url);
    var headers = authHeadersMultipart();
    Object.keys(headers).forEach(function (k) {
      if (headers[k]) xhr.setRequestHeader(k, headers[k]);
    });
    xhr.upload.addEventListener('progress', function (e) {
      if (e.lengthComputable && typeof onProgress === 'function') {
        onProgress(Math.round((e.loaded / e.total) * 100));
      }
    });
    xhr.addEventListener('load', function () {
      var body;
      try {
        body = JSON.parse(xhr.responseText || '{}');
      } catch (parseErr) {
        reject(new Error('Invalid server response'));
        return;
      }
      if (xhr.status >= 400) {
        reject(new Error(body.error || ('Upload failed (' + xhr.status + ')')));
        return;
      }
      resolve(body);
    });
    xhr.addEventListener('error', function () { reject(new Error('Upload failed')); });
    xhr.addEventListener('abort', function () { reject(new Error('Upload cancelled')); });
    xhr.send(formData);
  });
}
