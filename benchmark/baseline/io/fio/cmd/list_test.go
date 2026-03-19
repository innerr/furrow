package cmd

import (
	"testing"
)

func TestFormatBytesCmd(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
		{512 * 1024 * 1024, "512.0 MB"},
		{2 * 1024 * 1024 * 1024, "2.0 GB"},
		{150 * 1024 * 1024 * 1024, "150.0 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		if result != tt.expected {
			t.Errorf("formatBytes(%v) = %v, want %v", tt.bytes, result, tt.expected)
		}
	}
}
