function renderBankAccountConfirmationAnalysis(data) {
  // Auto-generated — edit freely, re-generate with POST .../build/apply
  let html = '';

  // Other fields
  html += '<table class="dm-analysis-table"><tbody>';
  html += `<tr><th>account_management_start_date</th><td>${escHtml(data.account_management_start_date ?? '')}</td></tr>`;
  html += `<tr><th>account_number</th><td>${escHtml(data.account_number ?? '')}</td></tr>`;
  html += `<tr><th>account_owners</th><td>${escHtml(data.account_owners ?? '')}</td></tr>`;
  html += `<tr><th>authorized_signatories</th><td>${escHtml(data.authorized_signatories ?? '')}</td></tr>`;
  html += `<tr><th>bank_name</th><td>${escHtml(data.bank_name ?? '')}</td></tr>`;
  html += `<tr><th>bank_number</th><td>${escHtml(data.bank_number ?? '')}</td></tr>`;
  html += `<tr><th>branch_number</th><td>${escHtml(data.branch_number ?? '')}</td></tr>`;
  html += `<tr><th>document_date</th><td>${escHtml(data.document_date ?? '')}</td></tr>`;
  html += `<tr><th>iban</th><td>${escHtml(data.iban ?? '')}</td></tr>`;
  html += `<tr><th>swift_code</th><td>${escHtml(data.swift_code ?? '')}</td></tr>`;
  html += '</tbody></table>';

  return html;
}
