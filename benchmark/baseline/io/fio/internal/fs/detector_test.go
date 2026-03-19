package fs

import (
	"testing"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1572864, "1.5 MB"},
		{1073741824, "1.0 GB"},
		{1073741824 * 2, "2.0 GB"},
		{1099511627776, "1.0 TB"},
		{1099511627776 * 2, "2.0 TB"},
		{512 * 1024 * 1024, "512.0 MB"},
		{100 * 1024 * 1024, "100.0 MB"},
	}

	for _, tt := range tests {
		result := FormatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("FormatBytes(%v) = %v, want %v", tt.bytes, result, tt.expected)
		}
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		value     float64
		precision int
		expected  string
	}{
		{1.5, 1, "1.5"},
		{1.0, 1, "1.0"},
		{100.0, 0, "100"},
		{0.5, 1, "0.5"},
		{99.99, 2, "99.99"},
		{0.0, 0, "0"},
	}

	for _, tt := range tests {
		result := formatFloat(tt.value, tt.precision)
		if result != tt.expected {
			t.Errorf("formatFloat(%v, %v) = %v, want %v", tt.value, tt.precision, result, tt.expected)
		}
	}
}

func TestNewDetector(t *testing.T) {
	detector := NewDetector()

	if detector == nil {
		t.Error("NewDetector() returned nil")
	}
}

func TestDetectorInterface(t *testing.T) {
	detector := NewDetector()

	_, ok := detector.(Detector)
	if !ok {
		t.Error("NewDetector() does not implement Detector interface")
	}
}
