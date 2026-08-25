package datamodeling

import "testing"

func TestFieldNamesSimilar(t *testing.T) {
	cases := []struct{ a, b string; want bool }{
		{"invoice_number", "invoice_number", true},
		{"invoice_number", "InvoiceNumber", true},
		{"invoice_number", "invoice_no", true},
		{"payment_amount", "amount", true},
		{"vendor", "customer", false},
	}
	for _, tc := range cases {
		if got := fieldNamesSimilar(tc.a, tc.b); got != tc.want {
			t.Errorf("fieldNamesSimilar(%q,%q)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestMatchDiscoveredField_priority(t *testing.T) {
	discovered := map[string]string{
		"invoice_number": "exact",
		"invoice_no":     "alias",
	}
	used := map[string]bool{}

	matched, ok := matchDiscoveredField("invoice_number", discovered, used)
	if !ok || matched != "invoice_number" {
		t.Fatalf("exact match: got %q ok=%v", matched, ok)
	}
	used[matched] = true

	matched, ok = matchDiscoveredField("invoice_number", discovered, used)
	if !ok || matched != "invoice_no" {
		t.Fatalf("similar fallback: got %q ok=%v", matched, ok)
	}
}
