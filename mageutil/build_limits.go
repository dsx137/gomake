package mageutil

import (
	"fmt"
	"runtime"

	"github.com/shirou/gopsutil/mem"
)

const (
	defaultBuildTaskMemBytes   = 1 * 1024 * 1024 * 1024 // 1GiB per concurrent build task
	defaultBuildThreadMemBytes = 500 * 1024 * 1024      // 500MiB per build thread (GOMAXPROCS)
)

type buildMemOptions struct {
	buildTaskMemBytes   uint64
	buildThreadMemBytes uint64
}

func resolveBuildMemOptions(opts *PathOptions) buildMemOptions {
	memOpts := buildMemOptions{
		buildTaskMemBytes:   defaultBuildTaskMemBytes,
		buildThreadMemBytes: defaultBuildThreadMemBytes,
	}
	if opts == nil {
		return memOpts
	}
	if opts.BuildTaskMemBytes != nil && *opts.BuildTaskMemBytes > 0 {
		memOpts.buildTaskMemBytes = *opts.BuildTaskMemBytes
	}
	if opts.BuildThreadMemBytes != nil && *opts.BuildThreadMemBytes > 0 {
		memOpts.buildThreadMemBytes = *opts.BuildThreadMemBytes
	}
	return memOpts
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	value := float64(bytes) / float64(div)
	suffixes := []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	if exp >= len(suffixes) {
		exp = len(suffixes) - 1
	}
	return fmt.Sprintf("%.2f%s", value, suffixes[exp])
}

type buildLimits struct {
	concurrency int
	goMaxProcs  int
	available   uint64
	memOpts     buildMemOptions
}

func calculateBuildLimits(compileCount int, memOpts buildMemOptions) (buildLimits, error) {
	cpuNum := runtime.GOMAXPROCS(0)
	if cpuNum <= 0 {
		cpuNum = runtime.NumCPU()
	} else if cpuNum > runtime.NumCPU() {
		cpuNum = runtime.NumCPU()
	}

	if cpuNum < 1 {
		cpuNum = 1
	}

	cpuConcurrency := cpuNum
	if compileCount < cpuNum {
		cpuConcurrency = compileCount
	}
	if cpuConcurrency <= 0 {
		cpuConcurrency = 1
	}

	if memOpts.buildTaskMemBytes == 0 || memOpts.buildThreadMemBytes == 0 {
		return buildLimits{memOpts: memOpts}, fmt.Errorf("invalid memory thresholds: buildTaskMem=%d, buildThreadMem=%d", memOpts.buildTaskMemBytes, memOpts.buildThreadMemBytes)
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return buildLimits{memOpts: memOpts}, fmt.Errorf("failed to read system memory: %w", err)
	}

	availableMem := vm.Available
	minRequired := memOpts.buildTaskMemBytes + memOpts.buildThreadMemBytes
	if availableMem < minRequired {
		return buildLimits{available: availableMem, memOpts: memOpts}, insufficientMemoryErr(availableMem, memOpts)
	}

	maxConcurrency := cpuConcurrency
	maxByTask := int(availableMem / memOpts.buildTaskMemBytes)
	if maxByTask < maxConcurrency {
		maxConcurrency = maxByTask
	}

	for t := maxConcurrency; t >= 1; t-- {
		pCPU := cpuNum / t
		if pCPU < 1 {
			pCPU = 1
		}
		perTask := availableMem / uint64(t)
		if perTask <= memOpts.buildTaskMemBytes {
			continue
		}
		threadBudget := perTask - memOpts.buildTaskMemBytes
		pMem := int(threadBudget / memOpts.buildThreadMemBytes)
		if pMem < 1 {
			continue
		}

		p := pMem
		if p > pCPU {
			p = pCPU
		}

		return buildLimits{
			concurrency: t,
			goMaxProcs:  p,
			available:   availableMem,
			memOpts:     memOpts,
		}, nil
	}

	return buildLimits{available: availableMem, memOpts: memOpts}, insufficientMemoryErr(availableMem, memOpts)
}

func insufficientMemoryErr(availableBytes uint64, memOpts buildMemOptions) error {
	return fmt.Errorf(
		"insufficient available memory: available=%s, buildTaskMem=%s, buildThreadMem=%s",
		formatBytes(availableBytes),
		formatBytes(memOpts.buildTaskMemBytes),
		formatBytes(memOpts.buildThreadMemBytes),
	)
}
