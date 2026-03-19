package fio

import (
	"testing"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func TestCalculateTestFileSize(t *testing.T) {
	tests := []struct {
		name          string
		partitionSize uint64
		freeSpace     uint64
		diskClass     types.DiskClass
		expectedMin   uint64
		expectedMax   uint64
	}{
		{
			name:          "small partition - tier 1",
			partitionSize: 32 * GB,
			freeSpace:     20 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   1 * GB,
			expectedMax:   5 * GB,
		},
		{
			name:          "medium partition - tier 2",
			partitionSize: 128 * GB,
			freeSpace:     100 * GB,
			diskClass:     types.DiskClassSATASSD,
			expectedMin:   2 * GB,
			expectedMax:   25 * GB,
		},
		{
			name:          "large partition - tier 3",
			partitionSize: 512 * GB,
			freeSpace:     400 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   4 * GB,
			expectedMax:   100 * GB,
		},
		{
			name:          "very large partition - tier 4",
			partitionSize: 2 * TB,
			freeSpace:     1 * TB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   8 * GB,
			expectedMax:   256 * GB,
		},
		{
			name:          "HDD reduces size by 60%",
			partitionSize: 256 * GB,
			freeSpace:     200 * GB,
			diskClass:     types.DiskClassFastHDD,
			expectedMin:   1 * GB,
			expectedMax:   50 * GB,
		},
		{
			name:          "slow HDD reduces size by 60%",
			partitionSize: 512 * GB,
			freeSpace:     400 * GB,
			diskClass:     types.DiskClassSlowHDD,
			expectedMin:   1 * GB,
			expectedMax:   100 * GB,
		},
		{
			name:          "limited free space",
			partitionSize: 1 * TB,
			freeSpace:     4 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   1 * GB,
			expectedMax:   1 * GB,
		},
		{
			name:          "very limited free space",
			partitionSize: 1 * TB,
			freeSpace:     1 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   0,
			expectedMax:   1 * GB,
		},
		{
			name:          "zero free space",
			partitionSize: 1 * TB,
			freeSpace:     0,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   0,
			expectedMax:   0,
		},
		{
			name:          "boundary tier 1-2",
			partitionSize: 64 * GB,
			freeSpace:     50 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   1 * GB,
			expectedMax:   12 * GB,
		},
		{
			name:          "boundary tier 2-3",
			partitionSize: 256 * GB,
			freeSpace:     200 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   4 * GB,
			expectedMax:   50 * GB,
		},
		{
			name:          "boundary tier 3-4",
			partitionSize: 1 * TB,
			freeSpace:     800 * GB,
			diskClass:     types.DiskClassNVMeSSD,
			expectedMin:   8 * GB,
			expectedMax:   200 * GB,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateTestFileSize(tt.partitionSize, tt.freeSpace, tt.diskClass)

			if result < tt.expectedMin {
				t.Errorf("CalculateTestFileSize() = %v, want at least %v", result, tt.expectedMin)
			}
			if result > tt.expectedMax {
				t.Errorf("CalculateTestFileSize() = %v, want at most %v", result, tt.expectedMax)
			}
		})
	}
}

func TestCalculateTestFileSizeHDDReduction(t *testing.T) {
	var partitionSize uint64 = 256 * GB
	var freeSpace uint64 = 200 * GB

	ssdSize := CalculateTestFileSize(partitionSize, freeSpace, types.DiskClassNVMeSSD)
	hddSize := CalculateTestFileSize(partitionSize, freeSpace, types.DiskClassFastHDD)
	slowHDDSize := CalculateTestFileSize(partitionSize, freeSpace, types.DiskClassSlowHDD)

	if hddSize >= ssdSize {
		t.Errorf("HDD size (%v) should be less than SSD size (%v)", hddSize, ssdSize)
	}

	if slowHDDSize >= ssdSize {
		t.Errorf("Slow HDD size (%v) should be less than SSD size (%v)", slowHDDSize, ssdSize)
	}

	expectedHDDSize := uint64(float64(ssdSize) * 0.6)
	if hddSize != expectedHDDSize {
		t.Errorf("HDD size = %v, want %v (60%% of SSD size)", hddSize, expectedHDDSize)
	}
}

func TestCalculateTestFileSizeFreeSpaceLimit(t *testing.T) {
	var partitionSize uint64 = 1 * TB

	var freeSpace25Percent uint64 = 256 * GB
	result := CalculateTestFileSize(partitionSize, freeSpace25Percent, types.DiskClassNVMeSSD)
	maxAllowed := uint64(float64(freeSpace25Percent) * 0.25)

	if result > maxAllowed {
		t.Errorf("Test file size (%v) should not exceed 25%% of free space (%v)", result, maxAllowed)
	}

	var freeSpaceLimited uint64 = 2 * GB
	resultLimited := CalculateTestFileSize(partitionSize, freeSpaceLimited, types.DiskClassNVMeSSD)
	maxAllowedLimited := uint64(float64(freeSpaceLimited) * 0.25)

	if resultLimited > maxAllowedLimited {
		t.Errorf("Test file size (%v) should not exceed 25%% of limited free space (%v)", resultLimited, maxAllowedLimited)
	}
}

func TestGetTestConfig(t *testing.T) {
	tests := []struct {
		name        string
		testName    string
		expectFound bool
		checkConfig func(types.TestConfig) bool
	}{
		{
			name:        "seq_read_async_direct",
			testName:    "seq_read_async_direct",
			expectFound: true,
			checkConfig: func(cfg types.TestConfig) bool {
				return cfg.RW == "read" && cfg.BS == "1M" && cfg.Direct
			},
		},
		{
			name:        "rand_read_4k_async_direct",
			testName:    "rand_read_4k_async_direct",
			expectFound: true,
			checkConfig: func(cfg types.TestConfig) bool {
				return cfg.RW == "randread" && cfg.BS == "4k" && cfg.Direct
			},
		},
		{
			name:        "mixed_70_30",
			testName:    "mixed_70_30",
			expectFound: true,
			checkConfig: func(cfg types.TestConfig) bool {
				return cfg.RW == "randrw" && cfg.RWMixRead == 70
			},
		},
		{
			name:        "fsync_limit",
			testName:    "fsync_limit",
			expectFound: true,
			checkConfig: func(cfg types.TestConfig) bool {
				return cfg.Fsync && cfg.BS == "4k"
			},
		},
		{
			name:        "nonexistent test",
			testName:    "nonexistent_test",
			expectFound: false,
			checkConfig: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, found := GetTestConfig(tt.testName)

			if found != tt.expectFound {
				t.Errorf("GetTestConfig() found = %v, want %v", found, tt.expectFound)
			}

			if found && tt.checkConfig != nil {
				if !tt.checkConfig(cfg) {
					t.Errorf("GetTestConfig() config check failed for %v", tt.testName)
				}
			}
		})
	}
}

func TestAdjustConfigForDiskClass(t *testing.T) {
	tests := []struct {
		name            string
		diskClass       types.DiskClass
		expectedIODepth int
		expectedNumJobs int
	}{
		{
			name:            "NVMe SSD",
			diskClass:       types.DiskClassNVMeSSD,
			expectedIODepth: 32,
			expectedNumJobs: 4,
		},
		{
			name:            "SATA SSD",
			diskClass:       types.DiskClassSATASSD,
			expectedIODepth: 32,
			expectedNumJobs: 4,
		},
		{
			name:            "Fast HDD",
			diskClass:       types.DiskClassFastHDD,
			expectedIODepth: 8,
			expectedNumJobs: 2,
		},
		{
			name:            "Slow HDD",
			diskClass:       types.DiskClassSlowHDD,
			expectedIODepth: 8,
			expectedNumJobs: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := types.TestConfig{
				Name:    "test",
				IODepth: 1,
				NumJobs: 1,
			}

			adjusted := AdjustConfigForDiskClass(cfg, tt.diskClass)

			if adjusted.IODepth != tt.expectedIODepth {
				t.Errorf("AdjustConfigForDiskClass() IODepth = %v, want %v",
					adjusted.IODepth, tt.expectedIODepth)
			}

			if adjusted.NumJobs != tt.expectedNumJobs {
				t.Errorf("AdjustConfigForDiskClass() NumJobs = %v, want %v",
					adjusted.NumJobs, tt.expectedNumJobs)
			}
		})
	}
}

func TestAdjustConfigForUnknownDiskClass(t *testing.T) {
	cfg := types.TestConfig{
		Name:    "test",
		IODepth: 64,
		NumJobs: 8,
	}

	adjusted := AdjustConfigForDiskClass(cfg, "UnknownClass")

	if adjusted.IODepth != 64 {
		t.Errorf("AdjustConfigForDiskClass() should preserve original IODepth for unknown class")
	}

	if adjusted.NumJobs != 8 {
		t.Errorf("AdjustConfigForDiskClass() should preserve original NumJobs for unknown class")
	}
}

func TestTestCatalogCompleteness(t *testing.T) {
	requiredTests := []string{
		"seq_read_async_direct",
		"seq_write_async_direct",
		"rand_read_4k_async_direct",
		"rand_write_4k_async_direct",
		"mixed_70_30",
		"fsync_limit",
	}

	for _, test := range requiredTests {
		if _, ok := TestCatalog[test]; !ok {
			t.Errorf("Required test %v not found in TestCatalog", test)
		}
	}
}

func TestSamplingTestsList(t *testing.T) {
	if len(SamplingTests) == 0 {
		t.Error("SamplingTests should not be empty")
	}

	for _, test := range SamplingTests {
		if _, ok := TestCatalog[test]; !ok {
			t.Errorf("Sampling test %v not found in TestCatalog", test)
		}
	}
}

func TestTestSelectionMatrixCompleteness(t *testing.T) {
	classes := []types.DiskClass{
		types.DiskClassNVMeSSD,
		types.DiskClassSATASSD,
		types.DiskClassFastHDD,
		types.DiskClassSlowHDD,
	}

	for _, class := range classes {
		selection, ok := TestSelectionMatrix[class]
		if !ok {
			t.Errorf("TestSelectionMatrix missing entry for %v", class)
			continue
		}

		if len(selection.Run) == 0 {
			t.Errorf("TestSelectionMatrix[%v] has no tests to run", class)
		}

		if selection.IODepth <= 0 {
			t.Errorf("TestSelectionMatrix[%v] has invalid IODepth", class)
		}

		if selection.NumJobs <= 0 {
			t.Errorf("TestSelectionMatrix[%v] has invalid NumJobs", class)
		}
	}
}

func TestSizeTiers(t *testing.T) {
	if len(SizeTiers) < 4 {
		t.Errorf("SizeTiers should have at least 4 tiers, got %v", len(SizeTiers))
	}

	for i, tier := range SizeTiers {
		if tier.DefaultSizeGB == 0 {
			t.Errorf("SizeTiers[%v] has zero DefaultSizeGB", i)
		}

		if tier.MinFreeRatio <= 0 || tier.MinFreeRatio > 1 {
			t.Errorf("SizeTiers[%v] has invalid MinFreeRatio: %v", i, tier.MinFreeRatio)
		}
	}
}
