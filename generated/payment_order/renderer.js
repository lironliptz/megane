function renderPaymentOrderAnalysis(data) {
  // Auto-generated — edit freely, re-generate with POST .../build/apply
  let html = '';

  // Other fields
  html += '<table class="dm-analysis-table"><tbody>';
  html += `<tr><th>account_number</th><td>${escHtml(data.account_number ?? '')}</td></tr>`;
  html += `<tr><th>bank_branch_number</th><td>${escHtml(data.bank_branch_number ?? '')}</td></tr>`;
  html += `<tr><th>bank_name</th><td>${escHtml(data.bank_name ?? '')}</td></tr>`;
  html += `<tr><th>currency</th><td>${escHtml(data.currency ?? '')}</td></tr>`;
  html += `<tr><th>document_date</th><td>${escHtml(data.document_date ?? '')}</td></tr>`;
  html += `<tr><th>iban_list</th><td>${escHtml(data.iban_list ?? '')}</td></tr>`;
  html += `<tr><th>is_signed</th><td>${escHtml(data.is_signed ?? '')}</td></tr>`;
  html += `<tr><th>payee_name</th><td>${escHtml(data.payee_name ?? '')}</td></tr>`;
  html += `<tr><th>payer_bank_branch</th><td>${escHtml(data.payer_bank_branch ?? '')}</td></tr>`;
  html += `<tr><th>payer_bank_name</th><td>${escHtml(data.payer_bank_name ?? '')}</td></tr>`;
  html += `<tr><th>payer_name</th><td>${escHtml(data.payer_name ?? '')}</td></tr>`;
  html += `<tr><th>payer_tax_id</th><td>${escHtml(data.payer_tax_id ?? '')}</td></tr>`;
  html += `<tr><th>payment_order_number</th><td>${escHtml(data.payment_order_number ?? '')}</td></tr>`;
  html += `<tr><th>payments_list</th><td>${escHtml(data.payments_list ?? '')}</td></tr>`;
  html += `<tr><th>project_name</th><td>${escHtml(data.project_name ?? '')}</td></tr>`;
  html += `<tr><th>source_account_id</th><td>${escHtml(data.source_account_id ?? '')}</td></tr>`;
  html += `<tr><th>total_amount</th><td>${escHtml(data.total_amount ?? '')}</td></tr>`;
  html += '</tbody></table>';

  return html;
}
