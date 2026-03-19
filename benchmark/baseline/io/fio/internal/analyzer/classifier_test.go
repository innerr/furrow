package analyzer

import (
	"testing"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name              string
		sample            *types.SampleResult
		expectedDiskClass types.DiskClass
	}{
		{
			name: "NVMe SSD - high performance",
			sample: &types.SampleResult{
				SeqReadBWMBps:  3500,
				SeqWriteBWMBps: 3000,
				RandReadIOPS:   600000,
				RandWriteIOPS:  500000,
				FsyncIOPS:      50000,
			},
			expectedDiskClass: types.DiskClassNVMeSSD,
		},
		{
			name: "NVMe SSD - borderline",
			sample: &types.SampleResult{
				SeqReadBWMBps:  2100,
				SeqWriteBWMBps: 1800,
				RandReadIOPS:   550000,
				RandWriteIOPS:  400000,
			},
			expectedDiskClass: types.DiskClassNVMeSSD,
		},
		{
			name: "SATA SSD - high end",
			sample: &types.SampleResult{
				SeqReadBWMBps:  550,
				SeqWriteBWMBps: 500,
				RandReadIOPS:   90000,
				RandWriteIOPS:  80000,
			},
			expectedDiskClass: types.DiskClassSATASSD,
		},
		{
			name: "SATA SSD - borderline",
			sample: &types.SampleResult{
				SeqReadBWMBps:  450,
				SeqWriteBWMBps: 400,
				RandReadIOPS:   55000,
				RandWriteIOPS:  50000,
			},
			expectedDiskClass: types.DiskClassSATASSD,
		},
		{
			name: "SATA SSD - low end (IOPS just above threshold)",
			sample: &types.SampleResult{
				SeqReadBWMBps:  420,
				SeqWriteBWMBps: 380,
				RandReadIOPS:   51000,
				RandWriteIOPS:  45000,
			},
			expectedDiskClass: types.DiskClassSATASSD,
		},
		{
			name: "Fast HDD - typical",
			sample: &types.SampleResult{
				SeqReadBWMBps:  200,
				SeqWriteBWMBps: 180,
				RandReadIOPS:   200,
				RandWriteIOPS:  180,
			},
			expectedDiskClass: types.DiskClassFastHDD,
		},
		{
			name: "Fast HDD - borderline",
			sample: &types.SampleResult{
				SeqReadBWMBps:  160,
				SeqWriteBWMBps: 150,
				RandReadIOPS:   150,
				RandWriteIOPS:  140,
			},
			expectedDiskClass: types.DiskClassFastHDD,
		},
		{
			name: "Slow HDD",
			sample: &types.SampleResult{
				SeqReadBWMBps:  100,
				SeqWriteBWMBps: 90,
				RandReadIOPS:   80,
				RandWriteIOPS:  70,
			},
			expectedDiskClass: types.DiskClassSlowHDD,
		},
		{
			name: "Very slow storage",
			sample: &types.SampleResult{
				SeqReadBWMBps:  50,
				SeqWriteBWMBps: 40,
				RandReadIOPS:   30,
				RandWriteIOPS:  25,
			},
			expectedDiskClass: types.DiskClassSlowHDD,
		},
		{
			name: "Edge case - just below NVMe threshold",
			sample: &types.SampleResult{
				SeqReadBWMBps:  2000,
				SeqWriteBWMBps: 1800,
				RandReadIOPS:   499999,
				RandWriteIOPS:  400000,
			},
			expectedDiskClass: types.DiskClassSATASSD,
		},
		{
			name: "Edge case - just below SATA SSD threshold",
			sample: &types.SampleResult{
				SeqReadBWMBps:  400,
				SeqWriteBWMBps: 350,
				RandReadIOPS:   49999,
				RandWriteIOPS:  45000,
			},
			expectedDiskClass: types.DiskClassFastHDD,
		},
		{
			name: "Edge case - just below Fast HDD threshold",
			sample: &types.SampleResult{
				SeqReadBWMBps:  149,
				SeqWriteBWMBps: 140,
				RandReadIOPS:   100,
				RandWriteIOPS:  90,
			},
			expectedDiskClass: types.DiskClassSlowHDD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Classify(tt.sample)
			if result != tt.expectedDiskClass {
				t.Errorf("Classify() = %v, want %v", result, tt.expectedDiskClass)
			}
		})
	}
}

func TestClassifyFromDiskType(t *testing.T) {
	tests := []struct {
		name              string
		diskType          string
		expectedDiskClass types.DiskClass
	}{
		{
			name:              "NVMe disk",
			diskType:          "nvme",
			expectedDiskClass: types.DiskClassNVMeSSD,
		},
		{
			name:              "SSD disk",
			diskType:          "ssd",
			expectedDiskClass: types.DiskClassSATASSD,
		},
		{
			name:              "HDD disk",
			diskType:          "hdd",
			expectedDiskClass: types.DiskClassFastHDD,
		},
		{
			name:              "Unknown type defaults to SlowHDD",
			diskType:          "unknown",
			expectedDiskClass: types.DiskClassSlowHDD,
		},
		{
			name:              "Empty type defaults to SlowHDD",
			diskType:          "",
			expectedDiskClass: types.DiskClassSlowHDD,
		},
		{
			name:              "USB type defaults to SlowHDD",
			diskType:          "usb",
			expectedDiskClass: types.DiskClassSlowHDD,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ClassifyFromDiskType(tt.diskType)
			if result != tt.expectedDiskClass {
				t.Errorf("ClassifyFromDiskType() = %v, want %v", result, tt.expectedDiskClass)
			}
		})
	}
}

func TestClassifyPriority(t *testing.T) {
	t.Run("NVMe threshold prioritizes IOPS over bandwidth", func(t *testing.T) {
		sample := &types.SampleResult{
			SeqReadBWMBps: 3000,
			RandReadIOPS:  600000,
		}
		if result := Classify(sample); result != types.DiskClassNVMeSSD {
			t.Errorf("Expected NVMe SSD for high BW and IOPS, got %v", result)
		}
	})

	t.Run("High IOPS with sufficient bandwidth is SATA SSD", func(t *testing.T) {
		sample := &types.SampleResult{
			SeqReadBWMBps: 450,
			RandReadIOPS:  100000,
		}
		if result := Classify(sample); result != types.DiskClassSATASSD {
			t.Errorf("Expected SATA SSD for sufficient BW and high IOPS, got %v", result)
		}
	})

	t.Run("High bandwidth with low IOPS is Fast HDD", func(t *testing.T) {
		sample := &types.SampleResult{
			SeqReadBWMBps: 200,
			RandReadIOPS:  200,
		}
		if result := Classify(sample); result != types.DiskClassFastHDD {
			t.Errorf("Expected Fast HDD for high BW and low IOPS, got %v", result)
		}
	})
}
