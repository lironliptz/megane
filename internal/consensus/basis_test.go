package consensus

import "testing"

func TestClassifyBasis(t *testing.T) {
	sec := 0.10
	if got, diverged := ClassifyBasis(0.104, &sec); got != "reported" || diverged {
		t.Fatalf("within 5%% => reported, got %q diverged=%v", got, diverged)
	}
	if got, diverged := ClassifyBasis(0.20, &sec); got != "street" || !diverged {
		t.Fatalf("outside 5%% => street, got %q diverged=%v", got, diverged)
	}
	if got, diverged := ClassifyBasis(0.20, nil); got != "unknown" || diverged {
		t.Fatalf("nil sidecar => unknown, got %q diverged=%v", got, diverged)
	}
}
