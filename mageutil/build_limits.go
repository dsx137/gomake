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

func memoryLimits(memOpts buildMemOptions) (concurrency int, goMaxProcs int, availableBytes uint64, err error) {
	if memOpts.buildTaskMemBytes == 0 || memOpts.buildThreadMemBytes == 0 {
		return 0, 0, 0, fmt.Errorf("invalid memory thresholds: buildTaskMem=%d, buildThreadMem=%d", memOpts.buildTaskMemBytes, memOpts.buildThreadMemBytes)
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return 0, 0, 0, fmt.Errorf("failed to read system memory: %w", err)
	}

	availableBytes = vm.Available
	concurrency = int(availableBytes / memOpts.buildTaskMemBytes)
	goMaxProcs = int(availableBytes / memOpts.buildThreadMemBytes)

	if concurrency < 1 || goMaxProcs < 1 {
		return concurrency, goMaxProcs, availableBytes, fmt.Errorf(
			"insufficient available memory: available=%s, buildTaskMem=%s, buildThreadMem=%s",
			formatBytes(availableBytes),
			formatBytes(memOpts.buildTaskMemBytes),
			formatBytes(memOpts.buildThreadMemBytes),
		)
	}

	return concurrency, goMaxProcs, availableBytes, nil
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

	cpuConcurrency := cpuNum
	if compileCount < cpuNum {
		cpuConcurrency = compileCount
	}
	if cpuNum < cpuConcurrency {
		cpuConcurrency = cpuNum
	}
	if cpuConcurrency <= 0 {
		cpuConcurrency = 1
	}

	cpuGoMaxProcs := cpuNum / cpuConcurrency
	if cpuGoMaxProcs <= 0 {
		cpuGoMaxProcs = 1
	}

	memConcurrency, memGoMaxProcs, availableMem, err := memoryLimits(memOpts)
	if err != nil {
		return buildLimits{
			available: availableMem,
			memOpts:   memOpts,
		}, err
	}

	concurrency := cpuConcurrency
	if memConcurrency < concurrency {
		concurrency = memConcurrency
	}

	goMaxProcs := cpuGoMaxProcs
	if memGoMaxProcs < goMaxProcs {
		goMaxProcs = memGoMaxProcs
	}

	if concurrency < 1 || goMaxProcs < 1 {
		return buildLimits{}, fmt.Errorf(
			"insufficient memory for compilation: available=%s, buildTaskMem=%s, buildThreadMem=%s",
			formatBytes(availableMem),
			formatBytes(memOpts.buildTaskMemBytes),
			formatBytes(memOpts.buildThreadMemBytes),
		)
	}

	return buildLimits{
		concurrency: concurrency,
		goMaxProcs:  goMaxProcs,
		available:   availableMem,
		memOpts:     memOpts,
	}, nil
}
