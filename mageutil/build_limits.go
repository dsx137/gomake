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

type buildLimits struct {
	concurrency   int
	goMaxProcs    int
	availableMem  uint64
	availableDisk uint64
	tempInMemory  bool
	memOpts       buildMemOptions
}

func calculateBuildLimits(compileCount int, memOpts buildMemOptions) (buildLimits, error) {
	if memOpts.buildTaskMemBytes == 0 || memOpts.buildThreadMemBytes == 0 {
		return buildLimits{memOpts: memOpts},
			fmt.Errorf("invalid memory thresholds: task=%d, thread=%d",
				memOpts.buildTaskMemBytes, memOpts.buildThreadMemBytes)
	}

	cpuNum := clamp(runtime.GOMAXPROCS(0), 1, runtime.NumCPU())
	cpuConcurrency := clamp(compileCount, 1, cpuNum)

	vm, err := mem.VirtualMemory()
	if err != nil {
		return buildLimits{memOpts: memOpts}, fmt.Errorf("read system memory: %w", err)
	}

	tempRoot := resolveGoBuildTempRoot()
	tempInfo := resolveTempStorageInfo(tempRoot)

	taskBudget := vm.Available
	if !tempInfo.inMemory {
		taskBudget = tempInfo.availableDisk
	}
	threadBudget := vm.Available

	limits := buildLimits{
		availableMem:  vm.Available,
		availableDisk: tempInfo.availableDisk,
		tempInMemory:  tempInfo.inMemory,
		memOpts:       memOpts,
	}

	// 基础可行性检查
	if !hasMinimumResources(taskBudget, threadBudget, memOpts, tempInfo.inMemory) {
		return limits, insufficientResourcesErr(
			vm.Available, tempInfo.availableDisk, memOpts, tempInfo.inMemory)
	}

	maxTasks := min(cpuConcurrency, int(taskBudget/memOpts.buildTaskMemBytes))
	if maxTasks < 1 {
		maxTasks = 1
	}

	bestUse := 0
	bestFound := false

	for t := maxTasks; t >= 1; t-- {
		pCPU := max(1, cpuNum/t)
		pMem := maxThreadsPerTask(
			t, taskBudget, threadBudget, memOpts, tempInfo.inMemory,
		)

		p := min(pCPU, pMem)
		if p < 1 {
			continue
		}

		total := t * p
		if total == cpuNum {
			limits.concurrency = t
			limits.goMaxProcs = p
			return limits, nil
		}

		if !bestFound || total > bestUse || (total == bestUse && t > limits.concurrency) {
			bestFound = true
			bestUse = total
			limits.concurrency = t
			limits.goMaxProcs = p
		}
	}

	if bestFound {
		return limits, nil
	}
	return limits, insufficientResourcesErr(
		vm.Available, tempInfo.availableDisk, memOpts, tempInfo.inMemory)
}

func insufficientResourcesErr(availableMem, availableDisk uint64, memOpts buildMemOptions, tempInMemory bool) error {
	return fmt.Errorf(
		"insufficient available resources: tempInMemory=%t,diskAvailable=%s, memAvailable=%s, buildTaskMem=%s, buildThreadMem=%s",
		tempInMemory,
		formatBytes(availableDisk),
		formatBytes(availableMem),
		formatBytes(memOpts.buildTaskMemBytes),
		formatBytes(memOpts.buildThreadMemBytes),
	)
}

func hasMinimumResources(taskBudget uint64, threadBudget uint64, memOpts buildMemOptions, inMemory bool) bool {
	if inMemory {
		return taskBudget >= memOpts.buildTaskMemBytes+memOpts.buildThreadMemBytes
	}

	return taskBudget >= memOpts.buildTaskMemBytes && threadBudget >= memOpts.buildThreadMemBytes
}

func maxThreadsPerTask(t int, taskBudget, threadBudget uint64, memOpts buildMemOptions, inMemory bool) int {
	if inMemory {
		perTask := taskBudget / uint64(t)
		if perTask <= memOpts.buildTaskMemBytes {
			return 0
		}
		return int((perTask - memOpts.buildTaskMemBytes) / memOpts.buildThreadMemBytes)
	}

	perTaskDisk := taskBudget / uint64(t)
	if perTaskDisk < memOpts.buildTaskMemBytes {
		return 0
	}
	perTaskMem := threadBudget / uint64(t)
	return int(perTaskMem / memOpts.buildThreadMemBytes)
}
