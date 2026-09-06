package consensus

import "math"

func ClassifyBasis(vendorActual float64, sidecarEPS *float64) (basis string, diverged bool) {
	if sidecarEPS == nil {
		return "unknown", false
	}
	den := math.Abs(*sidecarEPS)
	if den < 1e-9 {
		if math.Abs(vendorActual-*sidecarEPS) < 1e-9 {
			return "reported", false
		}
		return "street", true
	}
	diff := math.Abs(vendorActual-*sidecarEPS) / den
	if diff <= 0.05 {
		return "reported", false
	}
	return "street", true
}
