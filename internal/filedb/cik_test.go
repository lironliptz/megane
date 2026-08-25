package filedb

import (
	"errors"
	"testing"
)

func TestNormalizeCIK(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"1567529", "0001567529", false},
		{"0001567529", "0001567529", false},
		{"CIK0001567529", "0001567529", false},
		{"cik1567529", "0001567529", false},
		{"  1567529  ", "0001567529", false},
		{"0", "0000000000", false},
		{"1234567890", "1234567890", false},
		// rejected
		{"", "", true},
		{"abc", "", true},
		{"12345678901", "", true}, // 11 digits
		{"../../etc/passwd", "", true},
		{"0001567529/..", "", true},
		{"15 67529", "", true},
		{"-1567529", "", true},
		{"CIK", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizeCIK(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("NormalizeCIK(%q) = %q, want error", tt.in, got)
			} else if !errors.Is(err, ErrInvalidCIK) {
				t.Errorf("NormalizeCIK(%q) err = %v, want ErrInvalidCIK", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("NormalizeCIK(%q) unexpected err: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeCIK(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTrimCIKZeros(t *testing.T) {
	tests := map[string]string{
		"0001567529": "1567529",
		"1567529":    "1567529",
		"0000000000": "0",
	}
	for in, want := range tests {
		if got := trimCIKZeros(in); got != want {
			t.Errorf("trimCIKZeros(%q) = %q, want %q", in, got, want)
		}
	}
}
