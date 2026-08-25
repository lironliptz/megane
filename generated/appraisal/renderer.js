function renderAppraisalAnalysis(data) {
  // Auto-generated — edit freely, re-generate with POST .../build/apply
  let html = '';

  // Other fields
  html += '<table class="dm-analysis-table"><tbody>';
  html += `<tr><th>contractor_name</th><td>${escHtml(data.contractor_name ?? '')}</td></tr>`;
  html += `<tr><th>developer_company</th><td>${escHtml(data.developer_company ?? '')}</td></tr>`;
  html += `<tr><th>estimated_completion_date</th><td>${escHtml(data.estimated_completion_date ?? '')}</td></tr>`;
  html += `<tr><th>financial_execution_percentage</th><td>${escHtml(data.financial_execution_percentage ?? '')}</td></tr>`;
  html += `<tr><th>inspector_name</th><td>${escHtml(data.inspector_name ?? '')}</td></tr>`;
  html += `<tr><th>payments_data</th><td>${escHtml(data.payments_data ?? '')}</td></tr>`;
  html += `<tr><th>physical_execution_percentage</th><td>${escHtml(data.physical_execution_percentage ?? '')}</td></tr>`;
  html += `<tr><th>project_name</th><td>${escHtml(data.project_name ?? '')}</td></tr>`;
  html += `<tr><th>report_date</th><td>${escHtml(data.report_date ?? '')}</td></tr>`;
  html += `<tr><th>report_number</th><td>${escHtml(data.report_number ?? '')}</td></tr>`;
  html += `<tr><th>report_period</th><td>${escHtml(data.report_period ?? '')}</td></tr>`;
  html += '</tbody></table>';

  return html;
}
