package fio

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/innerr/furrow/benchmark/baseline/io/fio/internal/types"
)

type FioOutput struct {
	GlobalOptions FioGlobalOptions `json:"global options"`
	FioVersion    string           `json:"fio version"`
	Timestamp     int64            `json:"timestamp"`
	Time          string           `json:"time"`
	Jobs          []FioJob         `json:"jobs"`
	DiskUtil      []FioDiskUtil    `json:"disk_util"`
}

type FioGlobalOptions struct {
	IOEngine string `json:"ioengine"`
	Direct   string `json:"direct"`
}

type FioJob struct {
	JobName    string      `json:"jobname"`
	GroupID    int         `json:"groupid"`
	Error      int         `json:"error"`
	ETA        int         `json:"eta"`
	Elapsed    int         `json:"elapsed"`
	JobOptions interface{} `json:"job options"`
	Read       FioRWStats  `json:"read"`
	Write      FioRWStats  `json:"write"`
	Trim       FioRWStats  `json:"trim"`
	Sync       FioRWStats  `json:"sync"`
	JobRuntime int         `json:"job_runtime"`
	UsrCPU     float64     `json:"usr_cpu"`
	SysCPU     float64     `json:"sys_cpu"`
	Ctx        int         `json:"ctx"`
	MajF       int         `json:"majf"`
	MinF       int         `json:"minf"`
}

type FioRWStats struct {
	IOBytes         int64        `json:"io_bytes"`
	Bandwidth       int64        `json:"bw"`
	BandwidthMin    int64        `json:"bw_min"`
	BandwidthMax    int64        `json:"bw_max"`
	BandwidthAgg    int64        `json:"bw_agg"`
	MeanBandwidth   float64      `json:"bw_mean"`
	StdDevBandwidth float64      `json:"bw_dev"`
	Samples         int64        `json:"bw_samples"`
	IOPS            float64      `json:"iops"`
	IOPSMin         float64      `json:"iops_min"`
	IOPSMax         float64      `json:"iops_max"`
	IOPSMean        float64      `json:"iops_mean"`
	IOPSStdDev      float64      `json:"iops_stddev"`
	IOPSSamples     int          `json:"iops_samples"`
	Latency         FioLatency   `json:"latency"`
	LatencyMS       FioLatencyMS `json:"latency_ms"`
	LatencyUS       FioLatencyUS `json:"latency_us"`
	LatencyNS       FioLatencyNS `json:"latency_ns"`
	TotalLat        FioLatency   `json:"total_latency"`
	TotalLatMS      FioLatencyMS `json:"total_latency_ms"`
	TotalLatUS      FioLatencyUS `json:"total_latency_us"`
	TotalLatNS      FioLatencyNS `json:"total_latency_ns"`
}

type FioLatency struct {
	Min    int64   `json:"min"`
	Max    int64   `json:"max"`
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
	N      int     `json:"n"`
}

type FioLatencyMS struct {
	Min    int64              `json:"min"`
	Max    int64              `json:"max"`
	Mean   float64            `json:"mean"`
	StdDev float64            `json:"stddev"`
	N      int                `json:"n"`
	Pct    map[string]float64 `json:"percentile"`
}

type FioLatencyUS struct {
	Min    int64              `json:"min"`
	Max    int64              `json:"max"`
	Mean   float64            `json:"mean"`
	StdDev float64            `json:"stddev"`
	N      int                `json:"n"`
	Pct    map[string]float64 `json:"percentile"`
}

type FioLatencyNS struct {
	Min    int64              `json:"min"`
	Max    int64              `json:"max"`
	Mean   float64            `json:"mean"`
	StdDev float64            `json:"stddev"`
	N      int                `json:"n"`
	Pct    map[string]float64 `json:"percentile"`
}

type FioDiskUtil struct {
	Name       string  `json:"name"`
	ReadIOPS   float64 `json:"r_iops"`
	WriteIOPS  float64 `json:"w_iops"`
	ReadMerge  int64   `json:"r_merges"`
	WriteMerge int64   `json:"w_merges"`
	ReadBytes  int64   `json:"r_bytes"`
	WriteBytes int64   `json:"w_bytes"`
	ReadTicks  int64   `json:"r_ticks"`
	WriteTicks int64   `json:"w_ticks"`
	Queue      int64   `json:"in_queue"`
	Util       float64 `json:"util"`
}

func ParseFioOutput(output []byte) (*FioOutput, error) {
	var fioOutput FioOutput
	if err := json.Unmarshal(output, &fioOutput); err != nil {
		return nil, fmt.Errorf("failed to parse fio output: %w", err)
	}
	return &fioOutput, nil
}

func ExtractMetrics(fioOutput *FioOutput, jobName string) (*types.TestMetrics, error) {
	for _, job := range fioOutput.Jobs {
		if job.JobName == jobName {
			return extractMetricsFromJob(&job)
		}
	}
	return nil, fmt.Errorf("job %s not found in fio output", jobName)
}

func extractMetricsFromJob(job *FioJob) (*types.TestMetrics, error) {
	metrics := &types.TestMetrics{
		LatencyPercentiles: make(map[string]float64),
	}

	rw := determineRW(job)
	if rw == "read" || rw == "randread" {
		metrics.BandwidthBytes = uint64(job.Read.IOBytes)
		metrics.BandwidthMBps = float64(job.Read.BandwidthAgg) / 1024
		metrics.IOPS = job.Read.IOPS
		extractLatency(&job.Read, metrics)
	} else if rw == "write" || rw == "randwrite" {
		metrics.BandwidthBytes = uint64(job.Write.IOBytes)
		metrics.BandwidthMBps = float64(job.Write.BandwidthAgg) / 1024
		metrics.IOPS = job.Write.IOPS
		extractLatency(&job.Write, metrics)
	} else if rw == "randrw" {
		metrics.BandwidthBytes = uint64(job.Read.IOBytes + job.Write.IOBytes)
		metrics.BandwidthMBps = float64(job.Read.BandwidthAgg+job.Write.BandwidthAgg) / 1024
		metrics.IOPS = job.Read.IOPS + job.Write.IOPS
		extractLatency(&job.Read, metrics)
	} else {
		metrics.BandwidthBytes = uint64(job.Read.IOBytes + job.Write.IOBytes)
		metrics.BandwidthMBps = float64(job.Read.BandwidthAgg+job.Write.BandwidthAgg) / 1024
		metrics.IOPS = job.Read.IOPS + job.Write.IOPS
	}

	metrics.CPUUser = job.UsrCPU
	metrics.CPUSystem = job.SysCPU

	return metrics, nil
}

func determineRW(job *FioJob) string {
	if job.Read.IOBytes > 0 && job.Write.IOBytes > 0 {
		return "randrw"
	} else if job.Read.IOBytes > 0 {
		return "read"
	} else if job.Write.IOBytes > 0 {
		return "write"
	}
	return "unknown"
}

func extractLatency(rw *FioRWStats, metrics *types.TestMetrics) {
	if rw.LatencyUS.N > 0 {
		metrics.LatencyMin = float64(rw.LatencyUS.Min)
		metrics.LatencyMax = float64(rw.LatencyUS.Max)
		metrics.LatencyMean = rw.LatencyUS.Mean
		metrics.LatencyStddev = rw.LatencyUS.StdDev
		for k, v := range rw.LatencyUS.Pct {
			key := "p" + strings.ReplaceAll(k, ".", "_")
			metrics.LatencyPercentiles[key] = v
		}
	} else if rw.LatencyNS.N > 0 {
		metrics.LatencyMin = float64(rw.LatencyNS.Min) / 1000
		metrics.LatencyMax = float64(rw.LatencyNS.Max) / 1000
		metrics.LatencyMean = rw.LatencyNS.Mean / 1000
		metrics.LatencyStddev = rw.LatencyNS.StdDev / 1000
		for k, v := range rw.LatencyNS.Pct {
			key := "p" + strings.ReplaceAll(k, ".", "_")
			metrics.LatencyPercentiles[key] = v / 1000
		}
	} else if rw.LatencyMS.N > 0 {
		metrics.LatencyMin = float64(rw.LatencyMS.Min) * 1000
		metrics.LatencyMax = float64(rw.LatencyMS.Max) * 1000
		metrics.LatencyMean = rw.LatencyMS.Mean * 1000
		metrics.LatencyStddev = rw.LatencyMS.StdDev * 1000
		for k, v := range rw.LatencyMS.Pct {
			key := "p" + strings.ReplaceAll(k, ".", "_")
			metrics.LatencyPercentiles[key] = v * 1000
		}
	} else {
		metrics.LatencyMin = float64(rw.Latency.Min)
		metrics.LatencyMax = float64(rw.Latency.Max)
		metrics.LatencyMean = rw.Latency.Mean
		metrics.LatencyStddev = rw.Latency.StdDev
	}
}

func ParseFioVersion(output string) (string, []int) {
	output = strings.TrimSpace(output)
	if strings.HasPrefix(output, "fio-") {
		version := strings.TrimPrefix(output, "fio-")
		parts := strings.Split(version, ".")
		var numeric []int
		for _, p := range parts {
			var n int
			fmt.Sscanf(p, "%d", &n)
			numeric = append(numeric, n)
		}
		return "fio-" + version, numeric
	}
	return output, nil
}
