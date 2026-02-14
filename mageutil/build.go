package mageutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/openimsdk/gomake/internal/util"
)

type BuildOptions struct {
	CgoEnabled          *string
	Release             *bool
	Compress            *bool
	Platforms           *[]string
	GoBuildTempRoot     *string
	TaskConcurrency     *int
	TaskGoMaxProcs      *int
	BuildTaskMemBytes   *uint64
	BuildThreadMemBytes *uint64
}

// CompileForPlatform Main compile function
func CompileForPlatform(buildOpt BuildOptions, platform string, compileBinaries []string, memOpts buildMemOptions) {
	var cmdBinaries, toolsBinaries []string

	toolsPrefix := Paths.ToolsDir
	cmdPrefix := Paths.SrcDir

	if Paths.SrcDir == "." {
		cmdPrefix = ""
	}

	if toolsPrefix != "" {
		toolsPrefix += string(filepath.Separator)
	}
	if cmdPrefix != "" {
		cmdPrefix += string(filepath.Separator)
	}

	// PrintBlue(fmt.Sprintf("Using cmd prefix: '%s'", cmdPrefix))
	// PrintBlue(fmt.Sprintf("Using tools prefix: '%s'", toolsPrefix))

	for _, binary := range compileBinaries {
		// PrintBlue(fmt.Sprintf("Processing binary: %s", binary))

		if toolsPrefix != "" && strings.HasPrefix(binary, toolsPrefix) {
			toolsBinary := strings.TrimPrefix(binary, toolsPrefix)
			toolsBinaries = append(toolsBinaries, toolsBinary)
		} else if cmdPrefix == "" || strings.HasPrefix(binary, cmdPrefix) {
			var cmdBinary string
			if cmdPrefix == "" {
				cmdBinary = binary
			} else {
				cmdBinary = strings.TrimPrefix(binary, cmdPrefix)
			}
			cmdBinaries = append(cmdBinaries, cmdBinary)
			// PrintBlue(fmt.Sprintf("Added to cmd binaries: %s", cmdBinary))
		} else {
			PrintYellow(fmt.Sprintf("Binary %s does not have a valid prefix. Skipping...", binary))
		}
	}

	PrintBlue(fmt.Sprintf("Cmd binaries: %v", cmdBinaries))
	PrintBlue(fmt.Sprintf("Tools binaries: %v", toolsBinaries))

	var cmdCompiledDirs []string
	var toolsCompiledDirs []string

	if len(cmdBinaries) > 0 {
		PrintBlue(fmt.Sprintf("Compiling cmd binaries for %s...", platform))
		// PrintBlue(fmt.Sprintf("Source directory: %s", filepath.Join(Paths.Root, Paths.SrcDir)))
		// PrintBlue(fmt.Sprintf("Output directory: %s", Paths.OutputBinPath))
		cmdCompiledDirs = compileDir(buildOpt, filepath.Join(Paths.Root, Paths.SrcDir), Paths.OutputBinPath, platform, cmdBinaries, memOpts)
	}

	if len(toolsBinaries) > 0 {
		PrintBlue(fmt.Sprintf("Compiling tools binaries for %s...", platform))
		toolsCompiledDirs = compileDir(buildOpt, filepath.Join(Paths.Root, Paths.ToolsDir), Paths.OutputBinToolPath, platform, toolsBinaries, memOpts)
	}

	createStartConfigYML(cmdCompiledDirs, toolsCompiledDirs)
}

func compileDir(buildOpt BuildOptions, sourceDir, outputBase, platform string, compileBinaries []string, memOpts buildMemOptions) []string {
	releaseEnabled := boolOption(buildOpt.Release)
	compressEnabled := boolOption(buildOpt.Compress)
	tempRoot := stringOption(buildOpt.GoBuildTempRoot)
	cgoEnabled := strings.TrimSpace(stringOption(buildOpt.CgoEnabled))

	PrintBlue(fmt.Sprintf("Build flags: RELEASE=%t, COMPRESS=%t", releaseEnabled, compressEnabled))

	if info, err := os.Stat(sourceDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		fmt.Printf("Failed read directory %s: %v\n", sourceDir, err)
		os.Exit(1)
	} else if !info.IsDir() {
		fmt.Printf("Failed %s is not dir\n", sourceDir)
		os.Exit(1)
	}

	targetOS, targetArch := strings.Split(platform, "_")[0], strings.Split(platform, "_")[1]
	outputDir := filepath.Join(outputBase, targetOS, targetArch)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", outputDir, err)
		os.Exit(1)
	}

	limits, err := calculateBuildLimits(len(compileBinaries), memOpts, tempRoot)
	if err != nil {
		PrintRed(err.Error())
		os.Exit(1)
	}

	concurrency := limits.concurrency
	goMaxProcs := limits.goMaxProcs

	applyBuildOverride("task concurrency", buildOpt.TaskConcurrency, &concurrency)
	applyBuildOverride("task GOMAXPROCS", buildOpt.TaskGoMaxProcs, &goMaxProcs)

	PrintGreen(fmt.Sprintf("Concurrent compilations: %d, GOMAXPROCS per build: %d", concurrency, goMaxProcs))
	if limits.tempInMemory {
		PrintGreen(fmt.Sprintf(
			"Resource limit: tmpfs=true, memAvailable=%s, buildTaskMem=%s, buildThreadMem=%s",
			util.FormatBytes(limits.availableMem),
			util.FormatBytes(limits.memOpts.buildTaskMemBytes),
			util.FormatBytes(limits.memOpts.buildThreadMemBytes),
		))
	} else {
		PrintGreen(fmt.Sprintf(
			"Resource limit: tmpfs=false, diskAvailable=%s, memAvailable=%s, buildTaskMem=%s, buildThreadMem=%s",
			util.FormatBytes(limits.availableDisk),
			util.FormatBytes(limits.availableMem),
			util.FormatBytes(limits.memOpts.buildTaskMemBytes),
			util.FormatBytes(limits.memOpts.buildThreadMemBytes),
		))
	}

	task := make(chan int, concurrency)
	go func() {
		for i := range compileBinaries {
			task <- i
		}
		close(task)
	}()

	res := make(chan string, 1)
	running := int64(concurrency)

	env := map[string]string{
		"GOMAXPROCS": strconv.Itoa(goMaxProcs),
		"GOOS":       targetOS,
		"GOARCH":     targetArch,
	}
	if cgoEnabled != "" {
		env["CGO_ENABLED"] = cgoEnabled
	}

	baseDirAbs, err := filepath.Abs(Paths.Root)
	if err != nil {
		PrintRed(fmt.Sprintf("Failed to get absolute path for root: %v", err))
		os.Exit(1)
	}

	for i := 0; i < concurrency; i++ {
		go func() {
			defer func() {
				if atomic.AddInt64(&running, -1) == 0 {
					close(res)
				}
			}()

			for index := range task {
				originalDir := baseDirAbs

				binaryPath := filepath.Join(sourceDir, compileBinaries[index])
				path, err := util.FindMainGoFile(binaryPath)
				if err != nil {
					PrintYellow(fmt.Sprintf("Failed to walk through binary path %s: %v", binaryPath, err))
					os.Exit(1)
				}
				if path == "" {
					continue
				}

				dir := filepath.Dir(path)
				dirName := filepath.Base(dir)
				outputFileName := dirName
				if targetOS == "windows" {
					outputFileName += ".exe"
				}

				// Find Go module directory
				goModDir := util.FindGoModDir(dir)
				if goModDir == "" {
					goModDir = "."
				} else {
					PrintBlue(fmt.Sprintf("Found go.mod at: %s", goModDir))
				}

				// checkout to the Go module directory
				if err := os.Chdir(goModDir); err != nil {
					PrintRed(fmt.Sprintf("Failed to change directory to %s: %v", goModDir, err))
					os.Chdir(originalDir)
					continue
				}

				outputPath := filepath.Join(outputDir, outputFileName)

				// get relative path from the build directory to the Go module directory
				relPath, err := filepath.Rel(goModDir, path)
				if err != nil {
					PrintRed(fmt.Sprintf("Failed to get relative path: %v", err))
					os.Exit(1)
				}

				// Use the relative path as the build target
				buildTarget := relPath

				PrintBlue(fmt.Sprintf("Compiling dir: %s for platform: %s binary: %s ...", dirName, platform, outputFileName))

				// PrintBlue(fmt.Sprintf("DEBUG: buildTarget = '%s'", buildTarget))
				// PrintBlue(fmt.Sprintf("DEBUG: goModDir = '%s'", goModDir))
				// PrintBlue(fmt.Sprintf("DEBUG: path = '%s'", path))

				buildArgs := []string{"build", "-o", outputPath}
				if releaseEnabled {
					PrintBlue("Building in release mode with optimizations...")
					buildArgs = append(buildArgs, "-trimpath", "-ldflags", "-s -w")
				}
				buildArgs = append(buildArgs, buildTarget)

				err = RunWithPriority(PriorityLow, env, "go", buildArgs...)

				os.Chdir(originalDir)

				if err != nil {
					PrintRed("Compilation aborted. " + fmt.Sprintf("failed to compile %s for %s: %v", dirName, platform, err))
					os.Exit(1)
				}

				PrintGreen(fmt.Sprintf("Successfully compiled. dir: %s for platform: %s binary: %s", dirName, platform, outputFileName))

				if compressEnabled {
					PrintBlue(fmt.Sprintf("Compressing %s with UPX...", outputFileName))
					if err := RunWithPriority(PriorityLow, nil, "upx", "--lzma", outputPath); err != nil {
						PrintYellow(fmt.Sprintf("UPX compression failed for %s (non-fatal): %v", outputFileName, err))
					} else {
						PrintGreen(fmt.Sprintf("Successfully compressed with UPX: %s", outputFileName))
					}
				}

				res <- dirName
			}
		}()
	}

	compiledDirs := make([]string, 0, len(compileBinaries))
	for str := range res {
		compiledDirs = append(compiledDirs, str)
	}
	return compiledDirs
}

func createStartConfigYML(cmdDirs, toolsDirs []string) {
	configPath := filepath.Join(Paths.Root, StartConfigFile)

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		PrintBlue("start-config.yml already exists, skipping creation.")
		return
	}

	var content strings.Builder
	content.WriteString("serviceBinaries:\n")
	for _, dir := range cmdDirs {
		content.WriteString(fmt.Sprintf("  %s: 1\n", dir))
	}
	content.WriteString("toolBinaries:\n")
	for _, dir := range toolsDirs {
		content.WriteString(fmt.Sprintf("  - %s\n", dir))
	}
	content.WriteString("maxFileDescriptors: 10000\n")

	err := os.WriteFile(configPath, []byte(content.String()), 0644)
	if err != nil {
		PrintRed("Failed to create start-config.yml: " + err.Error())
		return
	}
	PrintGreen("start-config.yml created successfully.")
}

func applyBuildOverride(desc string, override *int, target *int) {
	if override == nil {
		return
	}
	*target = *override
	PrintBlue(fmt.Sprintf("Using %s override value=%d", desc, *override))
}

func resolveBuildOptionsFromEnv() BuildOptions {
	return BuildOptions{
		CgoEnabled:      resolveEnvOption[string]("CGO_ENABLED"),
		Release:         resolveEnvOption[bool]("RELEASE"),
		Compress:        resolveEnvOption[bool]("COMPRESS"),
		Platforms:       resolveEnvOption[[]string]("PLATFORMS"),
		GoBuildTempRoot: resolveEnvOption[string]("GOTMPDIR"),
	}
}

func ResolveBuildOptions(codeOpt *BuildOptions, envOpt *BuildOptions) BuildOptions {
	fromCode := BuildOptions{}
	if codeOpt != nil {
		fromCode = *codeOpt
	}

	fromEnv := BuildOptions{}
	if envOpt != nil {
		fromEnv = *envOpt
	}

	return BuildOptions{
		CgoEnabled:          util.CoalescePtr(fromCode.CgoEnabled, fromEnv.CgoEnabled),
		Release:             util.CoalescePtr(fromCode.Release, fromEnv.Release),
		Compress:            util.CoalescePtr(fromCode.Compress, fromEnv.Compress),
		Platforms:           util.CoalescePtr(fromCode.Platforms, fromEnv.Platforms),
		GoBuildTempRoot:     util.CoalescePtr(fromCode.GoBuildTempRoot, fromEnv.GoBuildTempRoot),
		TaskConcurrency:     util.CoalescePtr(fromCode.TaskConcurrency, fromEnv.TaskConcurrency),
		TaskGoMaxProcs:      util.CoalescePtr(fromCode.TaskGoMaxProcs, fromEnv.TaskGoMaxProcs),
		BuildTaskMemBytes:   fromCode.BuildTaskMemBytes,
		BuildThreadMemBytes: fromCode.BuildThreadMemBytes,
	}
}

func boolOption(opt *bool) bool {
	return opt != nil && *opt
}

func resolveEnvOption[T any](key string) *T {
	value, err := util.GetEnv[T](key)
	if err == nil {
		return &value
	}
	if errors.Is(err, util.ErrEnvNotSet) {
		return nil
	}
	PrintYellow(fmt.Sprintf("Invalid env %s: %v", key, err))
	return nil
}

func stringOption(opt *string) string {
	if opt == nil {
		return ""
	}
	return *opt
}

func platformsOption(opt *[]string) []string {
	if opt == nil {
		return nil
	}
	return *opt
}

func getBinaries(binaries []string) []string {
	if len(binaries) > 0 {
		return resolveRequestedBinaries(binaries)
	}

	type binarySource struct {
		baseDir string
		prefix  string
	}

	sources := []binarySource{
		{baseDir: filepath.Join(Paths.Root, Paths.SrcDir), prefix: normalizedSourcePrefix(Paths.SrcDir)},
		{baseDir: filepath.Join(Paths.Root, Paths.ToolsDir), prefix: normalizedSourcePrefix(Paths.ToolsDir)},
	}

	var allBinaries []string
	for _, source := range sources {
		dirs, err := getSubDirectoriesBFS(source.baseDir)
		if err != nil {
			PrintYellow(fmt.Sprintf("Failed to glob pattern %s: %v", source.baseDir, err))
			continue
		}

		for _, dir := range dirs {
			allBinaries = append(allBinaries, withSourcePrefix(source.prefix, dir))
		}
	}

	return allBinaries
}

func getSubDirectoriesBFS(baseDir string) ([]string, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}

	queue := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || util.IsExcludedBinaryDir(entry.Name()) {
			continue
		}
		queue = append(queue, filepath.Join(baseDir, entry.Name()))
	}

	var subDirs []string
	for i := 0; i < len(queue); i++ {
		currentDir := queue[i]

		if util.ContainsMainGo(currentDir) {
			relPath, err := filepath.Rel(baseDir, currentDir)
			if err == nil {
				subDirs = append(subDirs, relPath)
			}
			continue
		}

		children, err := os.ReadDir(currentDir)
		if err != nil {
			PrintYellow(fmt.Sprintf("Failed to read directory %s: %v", currentDir, err))
			continue
		}

		for _, child := range children {
			if !child.IsDir() {
				continue
			}
			name := child.Name()
			if util.IsExcludedBinaryDir(name) {
				PrintYellow(fmt.Sprintf("Skipping excluded directory: %s", name))
				continue
			}
			queue = append(queue, filepath.Join(currentDir, name))
		}
	}

	return subDirs, nil
}

func resolveRequestedBinaries(binaries []string) []string {
	var resolved []string
	for _, binary := range binaries {
		if path, found := isCmdBinary(binary); found {
			resolved = append(resolved, path)
			continue
		}
		if path, found := isToolBinary(binary); found {
			resolved = append(resolved, path)
			continue
		}
		PrintYellow(fmt.Sprintf("Binary %s not found in cmd (%s) or tools (%s) directories. Skipping...", binary, Paths.SrcDir, Paths.ToolsDir))
	}
	fmt.Println("Resolved binaries:", resolved)
	return resolved
}

func normalizedSourcePrefix(prefix string) string {
	if prefix == "." {
		return ""
	}
	return prefix
}

func withSourcePrefix(prefix, relPath string) string {
	if prefix == "" {
		return relPath
	}
	return filepath.Join(prefix, relPath)
}

func findBinaryPath(baseDir, binaryName string) (string, bool) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		PrintYellow(fmt.Sprintf("Failed to read directory %s: %v", baseDir, err))
		return "", false
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		subDirPath := filepath.Join(baseDir, entry.Name())
		if entry.Name() == binaryName {
			relativePath, err := filepath.Rel(baseDir, subDirPath)
			if err != nil {
				PrintYellow(fmt.Sprintf("Failed to get relative path for %s: %v", subDirPath, err))
				continue
			}
			return relativePath, true
		}
		if path, found := findBinaryPath(subDirPath, binaryName); found {
			return filepath.Join(entry.Name(), path), true
		}
	}
	return "", false
}

func isCmdBinary(binary string) (string, bool) {
	path, found := findBinaryPath(filepath.Join(Paths.Root, Paths.SrcDir), binary)
	if found {
		if Paths.SrcDir == "." {
			return path, true
		}

		return filepath.Join(Paths.SrcDir, path), true
	}
	return "", false
}

func isToolBinary(binary string) (string, bool) {
	path, found := findBinaryPath(filepath.Join(Paths.Root, Paths.ToolsDir), binary)
	if found {
		return filepath.Join(Paths.ToolsDir, path), true
	}
	return "", false
}
