package mageutil

import (
	"fmt"
	"runtime"

	"github.com/openimsdk/gomake/internal/util"
	"github.com/shirou/gopsutil/mem"
)

const (
	defaultBuildTaskMemBytes   = 1 * 1024 * 1024 * 1024 // 1GiB per concurrent build task
	defaultBuildThreadMemBytes = 500 * 1024 * 1024      // 500MiB per build thread (GOMAXPROCS)
)

type BuildMemOptions struct {
	BuildTaskMemBytes   uint64
	BuildThreadMemBytes uint64
}

func resolveBuildMemOptions(opts *BuildOptions) *BuildMemOptions {
	memOpts := &BuildMemOptions{
		BuildTaskMemBytes:   defaultBuildTaskMemBytes,
		BuildThreadMemBytes: defaultBuildThreadMemBytes,
	}
	memOpt := opts.GetMemOpt()
	if memOpt == nil {
		return memOpts
	}
	if memOpt.BuildTaskMemBytes > 0 {
		memOpts.BuildTaskMemBytes = memOpt.BuildTaskMemBytes
	}
	if memOpt.BuildThreadMemBytes > 0 {
		memOpts.BuildThreadMemBytes = memOpt.BuildThreadMemBytes
	}
	return memOpts
}

type buildLimits struct {
	concurrency   int
	goMaxProcs    int
	availableMem  uint64
	availableDisk uint64
	tempInMemory  bool
}

func calculateBuildLimits(compileCount int, memOpts *BuildMemOptions, tempRoot string) (buildLimits, error) {
	if memOpts == nil {
		return buildLimits{}, fmt.Errorf("invalid memory thresholds: mem options are nil")
	}

	resolvedMemOpts := *memOpts

	if resolvedMemOpts.BuildTaskMemBytes == 0 || resolvedMemOpts.BuildThreadMemBytes == 0 {
		return buildLimits{},
			fmt.Errorf("invalid memory thresholds: task=%d, thread=%d",
				resolvedMemOpts.BuildTaskMemBytes, resolvedMemOpts.BuildThreadMemBytes)
	}

	cpuNum := util.Clamp(runtime.GOMAXPROCS(0), 1, runtime.NumCPU())
	cpuConcurrency := util.Clamp(compileCount, 1, cpuNum)

	vm, err := mem.VirtualMemory()
	if err != nil {
		return buildLimits{}, fmt.Errorf("read system memory: %w", err)
	}

	tempInfo := util.ResolveTempStorageInfo(tempRoot)

	taskBudget := vm.Available
	if !tempInfo.InMemory {
		taskBudget = tempInfo.AvailableDisk
	}
	threadBudget := vm.Available

	limits := buildLimits{
		availableMem:  vm.Available,
		availableDisk: tempInfo.AvailableDisk,
		tempInMemory:  tempInfo.InMemory,
	}

	// 基础可行性检查
	if !hasMinimumResources(taskBudget, threadBudget, resolvedMemOpts, tempInfo.InMemory) {
		return limits, insufficientResourcesErr(
			vm.Available, tempInfo.AvailableDisk, resolvedMemOpts, tempInfo.InMemory)
	}

	maxTasks := min(cpuConcurrency, int(taskBudget/resolvedMemOpts.BuildTaskMemBytes))
	if maxTasks < 1 {
		maxTasks = 1
	}

	bestUse := 0
	bestFound := false

	for t := maxTasks; t >= 1; t-- {
		pCPU := max(1, cpuNum/t)
		pMem := maxThreadsPerTask(
			t, taskBudget, threadBudget, resolvedMemOpts, tempInfo.InMemory,
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
		vm.Available, tempInfo.AvailableDisk, resolvedMemOpts, tempInfo.InMemory)
}

func insufficientResourcesErr(availableMem, availableDisk uint64, memOpts BuildMemOptions, tempInMemory bool) error {
	return fmt.Errorf(
		"insufficient available resources: tempInMemory=%t,diskAvailable=%s, memAvailable=%s, buildTaskMem=%s, buildThreadMem=%s",
		tempInMemory,
		util.FormatBytes(availableDisk),
		util.FormatBytes(availableMem),
		util.FormatBytes(memOpts.BuildTaskMemBytes),
		util.FormatBytes(memOpts.BuildThreadMemBytes),
	)
}

func hasMinimumResources(taskBudget uint64, threadBudget uint64, memOpts BuildMemOptions, inMemory bool) bool {
	if inMemory {
		return taskBudget >= memOpts.BuildTaskMemBytes+memOpts.BuildThreadMemBytes
	}

	return taskBudget >= memOpts.BuildTaskMemBytes && threadBudget >= memOpts.BuildThreadMemBytes
}

func maxThreadsPerTask(t int, taskBudget, threadBudget uint64, memOpts BuildMemOptions, inMemory bool) int {
	if inMemory {
		perTask := taskBudget / uint64(t)
		if perTask <= memOpts.BuildTaskMemBytes {
			return 0
		}
		return int((perTask - memOpts.BuildTaskMemBytes) / memOpts.BuildThreadMemBytes)
	}

	perTaskDisk := taskBudget / uint64(t)
	if perTaskDisk < memOpts.BuildTaskMemBytes {
		return 0
	}
	perTaskMem := threadBudget / uint64(t)
	return int(perTaskMem / memOpts.BuildThreadMemBytes)
}
