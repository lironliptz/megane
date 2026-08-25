/**
 * app.js — shared utilities for jump-starter frontend pages.
 */

const TOKEN_KEY = 'token';
const ROLE_KEY  = 'role';

/**
 * Returns the stored JWT token, or null if not present.
 */
function getToken() {
  return localStorage.getItem(TOKEN_KEY);
}

/**
 * Returns an object suitable for use as fetch() headers, including the Bearer token.
 */
function authHeaders() {
  const token = getToken();
  return {
    'Authorization': token ? `Bearer ${token}` : '',
    'Content-Type': 'application/json',
  };
}

/**
 * Returns headers for multipart/form-data requests (no Content-Type override — browser sets boundary).
 */
function authHeadersMultipart() {
  const token = getToken();
  return {
    'Authorization': token ? `Bearer ${token}` : '',
  };
}

/**
 * Formats an ISO date string into a human-friendly local date/time.
 */
function formatDate(isoString) {
  if (!isoString) return '—';
  const d = new Date(isoString);
  if (isNaN(d.getTime())) return isoString;
  return d.toLocaleString();
}

/** Human-readable file size (bytes → KB/MB). */
function formatFileSize(bytes) {
  if (bytes == null || bytes <= 0) return '—';
  var n = Number(bytes);
  if (!isFinite(n) || n <= 0) return '—';
  if (n < 1024) return n + ' B';
  if (n < 1024 * 1024) return (n / 1024).toFixed(1) + ' KB';
  return (n / (1024 * 1024)).toFixed(1) + ' MB';
}

/**
 * Escapes HTML-significant characters so untrusted text is safe inside innerHTML.
 * Shared by every page — do not redefine it locally.
 */
function escHtml(str) {
  var s = str == null ? '' : String(str);
  return s.replace(/[&<>"']/g, function (c) {
    return ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c];
  });
}

/**
 * Creates a <progress> bar for pipeline stage % (0–100). Returns { el, setValue }.
 */
function createLLMProgressBar(initialPct) {
  var bar = document.createElement('progress');
  bar.className = 'pipeline-progress-bar';
  bar.max = 100;
  bar.value = Math.max(0, Math.min(100, initialPct || 0));
  return {
    el: bar,
    setValue: function (pct, isError) {
      bar.value = Math.max(0, Math.min(100, pct || 0));
      bar.classList.toggle('pipeline-progress-error', !!isError);
    }
  };
}

/** Status column: badge + optional progress bar + label row. */
function makePipelineSpinner(status, opts) {
  opts = opts || {};
  var wrap = document.createElement('div');
  wrap.className = 'files-status-cell pipeline-llm-cell';

  var badgeRow = document.createElement('div');
  badgeRow.innerHTML = statusBadgeHtml(status);
  wrap.appendChild(badgeRow);

  var pct = opts.pipelineProgress;
  if (isInProgressStatus(status) && pct != null && pct >= 0) {
    var pb = createLLMProgressBar(pct);
    wrap.appendChild(pb.el);
    var labelRow = document.createElement('div');
    labelRow.className = 'pipeline-llm-label-row';
    if (isInProgressStatus(status)) {
      var sp = document.createElement('span');
      sp.className = 'spinner';
      labelRow.appendChild(sp);
    }
    var lbl = document.createElement('span');
    lbl.className = 'pipeline-llm-label';
    lbl.textContent = opts.pipelineStatusLabel || status.replace(/_/g, ' ');
    labelRow.appendChild(lbl);
    wrap.appendChild(labelRow);
    wrap._progressBar = pb;
  }

  if (opts.errorDetail) {
    var err = document.createElement('div');
    err.className = 'cell-muted';
    err.style.fontSize = '0.78rem';
    err.textContent = opts.errorDetail;
    wrap.appendChild(err);
  }

  if (opts.showCancel && isInProgressStatus(status)) {
    var btn = document.createElement('button');
    btn.type = 'button';
    btn.className = 'btn btn-ghost btn-cancel-row';
    btn.textContent = 'Cancel';
    btn.addEventListener('click', function () {
      if (typeof opts.onCancel === 'function') opts.onCancel(opts.projectId);
    });
    wrap.appendChild(btn);
  }

  return wrap;
}

function statusBadgeHtml(status) {
  var spinner = isInProgressStatus(status) ? '<span class="spinner"></span>' : '';
  return '<span class="badge ' + status + '">' + spinner + status.replace(/_/g, ' ') + '</span>';
}

function isInProgressStatus(status) {
  return status === 'uploaded' || status === 'converting' || status === 'llm_pending' ||
    status === 'llm_done' || status === 'post_processing';
}

function formatDurationSeconds(totalSec) {
  if (totalSec == null || totalSec < 0 || !isFinite(totalSec)) return '—';
  var s = Math.floor(totalSec);
  if (s < 60) return s + ' s';
  var m = Math.floor(s / 60);
  var rs = s % 60;
  if (m < 60) return m + ' m ' + rs + ' s';
  var h = Math.floor(m / 60);
  var rm = m % 60;
  return h + ' h ' + rm + ' m';
}

/** Wall time from upload to completion; in-flight rows show elapsed + " …". */
function formatProcessingDisplay(uploadedIso, completedIso, status) {
  var up = uploadedIso ? new Date(uploadedIso).getTime() : NaN;
  if (isNaN(up)) return '—';
  if (completedIso) {
    var done = new Date(completedIso).getTime();
    if (!isNaN(done) && done >= up) {
      return formatDurationSeconds(Math.round((done - up) / 1000));
    }
  }
  var inProg = status === 'uploaded' || status === 'converting' || status === 'llm_pending' ||
    status === 'llm_done' || status === 'post_processing';
  if (inProg) {
    var elapsed = Math.round((Date.now() - up) / 1000);
    return formatDurationSeconds(elapsed) + ' …';
  }
  return '—';
}

/**
 * Displays an error message in the element with id="error-msg".
 * Creates the element if it doesn't exist.
 */
function showError(message) {
  let el = document.getElementById('error-msg');
  if (!el) {
    el = document.createElement('div');
    el.id = 'error-msg';
    el.className = 'error-msg';
    document.body.prepend(el);
  }
  el.textContent = message;
  el.style.display = 'block';
  setTimeout(() => { el.style.display = 'none'; }, 6000);
}

/**
 * Redirects to /login if no token is present.
 */
function requireAuth() {
  if (!getToken()) {
    window.location.href = '/login';
  }
}

/**
 * Clears stored credentials and redirects to /login.
 */
function logout() {
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(ROLE_KEY);
  window.location.href = '/login';
}

/**
 * Returns true if the stored role is "admin".
 */
function isAdmin() {
  return localStorage.getItem(ROLE_KEY) === 'admin';
}

/**
 * Performs a JSON API call. Returns the parsed `data` field or throws on error.
 */
async function apiFetch(url, options = {}) {
  const defaults = {
    headers: authHeaders(),
  };
  const merged = Object.assign({}, defaults, options, {
    headers: Object.assign({}, defaults.headers, options.headers || {}),
  });
  const resp = await fetch(url, merged);
  const body = await resp.json();
  if (body.error) throw new Error(body.error);
  return body.data;
}
