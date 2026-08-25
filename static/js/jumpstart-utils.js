/**
 * jumpstart-utils.js — declarative Jump-Start form helpers.
 *
 * To add a new LLM provider: add one entry to LLM_PROVIDERS.
 * To add a new general field:  add one entry to GENERAL_FIELDS in the relevant section
 * and call collectGeneralPayload() in doJumpStart().
 *
 * No other file needs to change for provider additions.
 */

// ---------------------------------------------------------------------------
// LLM provider registry
// Each entry: { value, label, fields: [{ id, label, type, placeholder, payload }] }
// ---------------------------------------------------------------------------
const LLM_PROVIDERS = [
  {
    value: 'gemini',
    label: 'Google Gemini',
    fields: [
      { id: 'js-gemini-key', label: 'Gemini API key', type: 'text', placeholder: 'AIza…', payload: 'gemini_api_key', required: true },
    ],
  },
  {
    value: 'openai',
    label: 'OpenAI',
    fields: [
      { id: 'js-openai-key', label: 'OpenAI API key', type: 'text', placeholder: 'sk-…', payload: 'openai_api_key', required: true },
    ],
  },
  {
    value: 'local',
    label: 'Local (Ollama)',
    fields: [
      { id: 'js-local-url', label: 'Local LLM URL', type: 'url', placeholder: 'http://localhost:11434', payload: 'local_llm_url', required: true },
    ],
  },
];

const SLUG_RE = /^[a-z][a-z0-9-]{1,62}$/;
const HEX_COLOR_RE = /^#[0-9a-fA-F]{3}([0-9a-fA-F]{3})?$/;

/**
 * Populates the provider <select> and appends per-provider field groups.
 * Call once on page load.
 * @param {string} selectId  — id of the <select> element
 * @param {string} containerId — id of the container that receives the field groups
 */
function renderLLMProviders(selectId, containerId) {
  const sel = document.getElementById(selectId);
  const box = document.getElementById(containerId);

  // Build <option> list
  sel.innerHTML = LLM_PROVIDERS.map(p =>
    `<option value="${p.value}">${p.label}</option>`
  ).join('');

  // Build one field-group div per provider (hidden except the first)
  box.innerHTML = LLM_PROVIDERS.map((p, i) =>
    `<div id="llm-group-${p.value}" style="${i === 0 ? '' : 'display:none'}">` +
    p.fields.map(f =>
      `<div class="form-group" style="margin:0">` +
        `<label>${f.label}${f.required ? ' <span class="req" aria-hidden="true">*</span>' : ''}</label>` +
        `<input type="${f.type}" id="${f.id}" placeholder="${f.placeholder || ''}">` +
      `</div>`
    ).join('') +
    `</div>`
  ).join('');

  updateLLMFields(selectId);
}

/**
 * Shows only the field group for the currently-selected provider.
 * Wire to the <select>'s onchange.
 * @param {string} selectId
 */
function updateLLMFields(selectId) {
  const val = document.getElementById(selectId).value;
  LLM_PROVIDERS.forEach(p => {
    const el = document.getElementById('llm-group-' + p.value);
    if (el) el.style.display = p.value === val ? '' : 'none';
    p.fields.forEach(f => {
      const input = document.getElementById(f.id);
      if (input) input.required = p.value === val && !!f.required;
    });
  });
}

/**
 * Returns a payload object containing all provider field values.
 * Keys match the `payload` property in each field descriptor.
 * @param {string} selectId
 * @returns {Object}
 */
function collectLLMPayload(selectId) {
  const out = { llm_provider: document.getElementById(selectId).value };
  LLM_PROVIDERS.forEach(p =>
    p.fields.forEach(f => {
      const el = document.getElementById(f.id);
      out[f.payload] = el ? el.value.trim() : '';
    })
  );
  return out;
}

/**
 * Validates Jump-Start payload before POST. Returns null if OK, or an error message.
 * Mirrors internal/admin/jumpstart.go validateConfig.
 * @param {Object} p
 * @returns {string|null}
 */
function validateJumpStartPayload(p) {
  if (!p.app_name) return 'App name is required.';
  if (!p.app_slug || !SLUG_RE.test(p.app_slug)) {
    return 'App slug must be lowercase letters, digits, and hyphens (2–63 chars, start with a letter).';
  }
  if (!p.target_path) return 'Target path is required.';
  if (!p.description) return 'Description is required.';
  if (!p.language || (p.language !== 'en' && p.language !== 'he')) {
    return 'Interface language is required.';
  }
  if (p.primary_color && !HEX_COLOR_RE.test(p.primary_color)) {
    return 'Primary accent color must be a hex value like #4f8ef7.';
  }
  if (!p.admin_email) return 'Admin email is required.';
  if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(p.admin_email)) {
    return 'Admin email must be a valid email address.';
  }
  if (!p.admin_password) return 'Admin password is required.';
  if (p.admin_password.length < 4) return 'Admin password must be at least 4 characters.';
  if (!Number.isFinite(p.session_ttl) || p.session_ttl < 60) {
    return 'Session TTL must be at least 60 seconds.';
  }
  if (!Number.isFinite(p.port) || p.port < 1 || p.port > 65535) {
    return 'Port must be between 1 and 65535.';
  }

  const provider = p.llm_provider || 'gemini';
  if (provider === 'gemini' && !p.gemini_api_key) {
    return 'Gemini API key is required when Google Gemini is selected.';
  }
  if (provider === 'openai' && !p.openai_api_key) {
    return 'OpenAI API key is required when OpenAI is selected.';
  }
  if (provider === 'local' && !p.local_llm_url) {
    return 'Local LLM URL is required when Local (Ollama) is selected.';
  }
  if (!['gemini', 'openai', 'local'].includes(provider)) {
    return 'LLM provider must be Gemini, OpenAI, or Local.';
  }
  return null;
}

/**
 * Reads all Jump-Start form fields into a payload object.
 */
function collectJumpStartPayload() {
  const ttlRaw = document.getElementById('js-ttl').value;
  const portRaw = document.getElementById('js-port').value;
  return {
    app_name:       document.getElementById('js-app-name').value.trim(),
    app_slug:       document.getElementById('js-app-slug').value.trim(),
    target_path:    document.getElementById('js-path').value.trim(),
    description:    document.getElementById('js-description').value.trim(),
    language:       document.getElementById('js-language').value,
    primary_color:  document.getElementById('js-color-text').value.trim(),
    session_ttl:    ttlRaw === '' ? NaN : parseInt(ttlRaw, 10),
    admin_email:    document.getElementById('js-admin-email').value.trim(),
    admin_password: document.getElementById('js-admin-password').value.trim(),
    port:           portRaw === '' ? NaN : parseInt(portRaw, 10),
    ...collectLLMPayload('js-llm'),
  };
}

function autoSlug(name) {
  const slug = name.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-|-$/g, '');
  const el = document.getElementById('js-app-slug');
  if (el) el.value = slug;
}

function syncColor(hex) {
  if (/^#[0-9a-fA-F]{3,8}$/.test(hex)) {
    const picker = document.getElementById('js-color-picker');
    if (picker) picker.value = hex;
  }
}

function jumpStartEscHtml(str) {
  return (str || '').replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);
}

function showJumpStartResult(type, html) {
  const el = document.getElementById('js-result');
  if (!el) return;
  el.className = 'js-result ' + type;
  el.innerHTML = html;
  el.style.display = 'block';
  el.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
}

async function doJumpStart() {
  const btn = document.querySelector('.jump-btn');
  const form = document.getElementById('js-form');
  const result = document.getElementById('js-result');
  if (result) result.style.display = 'none';

  if (form && !form.reportValidity()) {
    showJumpStartResult('error', 'Please fill in all required fields.');
    return;
  }

  const payload = collectJumpStartPayload();
  const validationError = validateJumpStartPayload(payload);
  if (validationError) {
    showJumpStartResult('error', jumpStartEscHtml(validationError));
    return;
  }

  if (btn) {
    btn.disabled = true;
    btn.textContent = '⚡ Creating…';
  }

  try {
    const data = await apiFetch('/api/admin/jumpstart', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
    showJumpStartResult('ok',
      `✅ <strong>${jumpStartEscHtml(payload.app_name)}</strong> created at <code>${jumpStartEscHtml(data.path)}</code>` +
      `<code>cd ${jumpStartEscHtml(data.path)} &amp;&amp; go mod tidy &amp;&amp; make run</code>`);
  } catch (err) {
    showJumpStartResult('error', '❌ ' + jumpStartEscHtml(err.message));
  } finally {
    if (btn) {
      btn.disabled = false;
      btn.textContent = '⚡ Jump-Start';
    }
  }
}

// Globals for inline handlers in admin.html (form onsubmit, oninput, etc.)
window.autoSlug = autoSlug;
window.syncColor = syncColor;
window.doJumpStart = doJumpStart;
window.collectJumpStartPayload = collectJumpStartPayload;
window.validateJumpStartPayload = validateJumpStartPayload;
