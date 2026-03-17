package report

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

func GenerateJSON(report *types.Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func GenerateReportID(hostname, deviceName string, t time.Time) string {
	return fmt.Sprintf("%s_%s_%s",
		t.Format("20060102_150405"),
		hostname,
		deviceName)
}

func CalculateScores(results map[string]types.TestConfigResult, diskClass types.DiskClass) map[string]int {
	scores := make(map[string]int)
	refs := getReferenceValues(diskClass)

	if r, ok := results["seq_read_async_direct"]; ok {
		scores["seq_read"] = calculateScore(r.Metrics.BandwidthMBps, refs.SeqReadBW)
	}
	if r, ok := results["seq_write_async_direct"]; ok {
		scores["seq_write"] = calculateScore(r.Metrics.BandwidthMBps, refs.SeqWriteBW)
	}
	if r, ok := results["rand_read_4k_async_direct"]; ok {
		scores["rand_read"] = calculateScore(r.Metrics.IOPS, refs.RandReadIOPS)
	}
	if r, ok := results["rand_write_4k_async_direct"]; ok {
		scores["rand_write"] = calculateScore(r.Metrics.IOPS, refs.RandWriteIOPS)
	}
	if r, ok := results["mixed_70_30"]; ok {
		scores["mixed"] = calculateScore(r.Metrics.IOPS, refs.MixedIOPS)
	}
	if r, ok := results["fsync_limit"]; ok {
		scores["fsync"] = calculateScore(r.Metrics.IOPS, refs.FsyncIOPS)
	}

	return scores
}

func CalculateOverallScore(scores map[string]int) int {
	weights := map[string]float64{
		"seq_read":   0.15,
		"seq_write":  0.15,
		"rand_read":  0.25,
		"rand_write": 0.25,
		"mixed":      0.10,
		"fsync":      0.10,
	}

	var total float64
	var weightSum float64
	for name, score := range scores {
		if w, ok := weights[name]; ok {
			total += float64(score) * w
			weightSum += w
		}
	}

	if weightSum == 0 {
		return 0
	}
	return int(total / weightSum * 100)
}

func calculateScore(value float64, refs types.ReferenceValues) int {
	if value >= refs.Excellent {
		return 100
	} else if value >= refs.Good {
		return 80 + int((value-refs.Good)/(refs.Excellent-refs.Good)*20)
	} else if value >= refs.Fair {
		return 60 + int((value-refs.Fair)/(refs.Good-refs.Fair)*20)
	} else if value >= refs.Poor {
		return 40 + int((value-refs.Poor)/(refs.Fair-refs.Poor)*20)
	}
	return int((value / refs.Poor) * 40)
}

func getReferenceValues(diskClass types.DiskClass) types.ScoreReferences {
	switch diskClass {
	case types.DiskClassNVMeSSD:
		return types.ScoreReferences{
			SeqReadBW:     types.ReferenceValues{Excellent: 3500, Good: 2500, Fair: 1500, Poor: 500},
			SeqWriteBW:    types.ReferenceValues{Excellent: 3000, Good: 2000, Fair: 1200, Poor: 400},
			RandReadIOPS:  types.ReferenceValues{Excellent: 800000, Good: 500000, Fair: 200000, Poor: 50000},
			RandWriteIOPS: types.ReferenceValues{Excellent: 700000, Good: 400000, Fair: 150000, Poor: 30000},
			FsyncIOPS:     types.ReferenceValues{Excellent: 50000, Good: 30000, Fair: 15000, Poor: 5000},
			MixedIOPS:     types.ReferenceValues{Excellent: 500000, Good: 300000, Fair: 150000, Poor: 50000},
		}
	case types.DiskClassSATASSD:
		return types.ScoreReferences{
			SeqReadBW:     types.ReferenceValues{Excellent: 550, Good: 400, Fair: 250, Poor: 100},
			SeqWriteBW:    types.ReferenceValues{Excellent: 520, Good: 380, Fair: 200, Poor: 80},
			RandReadIOPS:  types.ReferenceValues{Excellent: 100000, Good: 70000, Fair: 40000, Poor: 10000},
			RandWriteIOPS: types.ReferenceValues{Excellent: 90000, Good: 60000, Fair: 30000, Poor: 8000},
			FsyncIOPS:     types.ReferenceValues{Excellent: 30000, Good: 15000, Fair: 8000, Poor: 2000},
			MixedIOPS:     types.ReferenceValues{Excellent: 70000, Good: 50000, Fair: 30000, Poor: 10000},
		}
	default:
		return types.ScoreReferences{
			SeqReadBW:     types.ReferenceValues{Excellent: 200, Good: 150, Fair: 100, Poor: 50},
			SeqWriteBW:    types.ReferenceValues{Excellent: 200, Good: 150, Fair: 100, Poor: 50},
			RandReadIOPS:  types.ReferenceValues{Excellent: 300, Good: 200, Fair: 100, Poor: 50},
			RandWriteIOPS: types.ReferenceValues{Excellent: 300, Good: 200, Fair: 100, Poor: 50},
			FsyncIOPS:     types.ReferenceValues{Excellent: 1000, Good: 500, Fair: 200, Poor: 50},
			MixedIOPS:     types.ReferenceValues{Excellent: 200, Good: 150, Fair: 80, Poor: 30},
		}
	}
}

func GenerateRecommendations(scores map[string]int, diskClass types.DiskClass) []string {
	var recs []string

	overall := CalculateOverallScore(scores)
	if overall >= 85 {
		recs = append(recs, "Well-suited for high-performance workloads")
	} else if overall >= 70 {
		recs = append(recs, "Suitable for general-purpose workloads")
	}

	if scores["fsync"] < 70 {
		recs = append(recs, "Consider enabling write-back cache if data integrity allows")
	}

	if scores["rand_write"] < 60 && diskClass != types.DiskClassSlowHDD {
		recs = append(recs, "Random write performance may benefit from larger queue depth")
	}

	if len(recs) == 0 {
		recs = append(recs, "Performance is within expected range")
	}

	return recs
}

func IdentifyBottleneck(results map[string]types.TestConfigResult, scores map[string]int) (string, string) {
	minScore := 100
	minName := ""

	for name, score := range scores {
		if score < minScore {
			minScore = score
			minName = name
		}
	}

	if minScore >= 80 {
		return "", ""
	}

	detail := fmt.Sprintf("%s score is %d/100", minName, minScore)

	if minName == "fsync" {
		if r, ok := results["fsync_limit"]; ok {
			detail = fmt.Sprintf("fsync latency is high (IOPS: %.0f)", r.Metrics.IOPS)
		}
	}

	return minName, detail
}

func ScoreToStars(score int) string {
	if score >= 95 {
		return "★★★★★"
	} else if score >= 80 {
		return "★★★★☆"
	} else if score >= 60 {
		return "★★★☆☆"
	} else if score >= 40 {
		return "★★☆☆☆"
	}
	return "★☆☆☆☆"
}

func FormatBandwidth(mbps float64) string {
	if mbps >= 1000 {
		return fmt.Sprintf("%.1f GB/s", mbps/1000)
	}
	return fmt.Sprintf("%.0f MB/s", mbps)
}

func FormatIOPS(iops float64) string {
	if iops >= 1000000 {
		return fmt.Sprintf("%.1fM", iops/1000000)
	} else if iops >= 1000 {
		return fmt.Sprintf("%.0fK", iops/1000)
	}
	return fmt.Sprintf("%.0f", iops)
}

func FormatLatency(us float64) string {
	if us >= 1000 {
		return fmt.Sprintf("%.1f ms", us/1000)
	}
	return fmt.Sprintf("%.1f μs", us)
}
