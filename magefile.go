//go:build mage
// +build mage

package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/openimsdk/gomake/mageutil"
)

var Default = DefaultTarget

func DefaultTarget() {
	if shouldUseLauncherDefault() {
		Start()
		return
	}
	Build()
}

func shouldUseLauncherDefault() bool {
	if dirExists(filepath.Join(".", mageutil.SrcDir)) || dirExists(filepath.Join(".", mageutil.ToolsDir)) {
		return false
	}
	if !fileExists(filepath.Join(".", mageutil.StartConfigFile)) {
		return false
	}
	if dirExists(filepath.Join(".", mageutil.BinDir, mageutil.PlatformsDir)) {
		return true
	}
	return dirExists(filepath.Join(".", mageutil.OutputDir, mageutil.BinDir, mageutil.PlatformsDir))
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

var Aliases = map[string]any{
	"buildcc": BuildWithCustomConfig,
	"startcc": StartWithCustomConfig,
}

var (
	customRootDir = "."
	// customSrcDir  = "work_cmd"
	customSrcDir    = "cmd"
	customOutputDir = "_output"
	customConfigDir = "config"
	customToolsDir  = "tools"
)

// Build support specifical binary build.
//
// Example: `mage build openim-api openim-rpc-user seq`
func Build() {
	flag.Parse()
	bin := flag.Args()
	if len(bin) != 0 {
		bin = bin[1:]
	}

	mageutil.Build(bin, nil, nil)
}

func BuildWithCustomConfig() {
	flag.Parse()
	bin := flag.Args()
	if len(bin) != 0 {
		bin = bin[1:]
	}

	config := &mageutil.PathOptions{
		RootDir:   &customRootDir,   // default is "."(current directory)
		OutputDir: &customOutputDir, // default is "_output"
		SrcDir:    &customSrcDir,    // default is "cmd"
		ToolsDir:  &customToolsDir,  // default is "tools"
	}

	mageutil.Build(bin, config, nil)
}

func Start() {
	mageutil.InitForSSC()
	err := setMaxOpenFiles()
	if err != nil {
		mageutil.PrintRed("setMaxOpenFiles failed " + err.Error())
		os.Exit(1)
	}

	flag.Parse()
	bin := flag.Args()
	if len(bin) != 0 {
		bin = bin[1:]
	}

	mageutil.StartToolsAndServices(bin, nil)
}

func StartWithCustomConfig() {
	mageutil.InitForSSC()
	err := setMaxOpenFiles()
	if err != nil {
		mageutil.PrintRed("setMaxOpenFiles failed " + err.Error())
		os.Exit(1)
	}

	flag.Parse()
	bin := flag.Args()
	if len(bin) != 0 {
		bin = bin[1:]
	}

	config := &mageutil.PathOptions{
		RootDir:   &customRootDir,   // default is "."(current directory)
		OutputDir: &customOutputDir, // default is "_output"
		ConfigDir: &customConfigDir, // default is "config"
	}

	mageutil.StartToolsAndServices(bin, config)
}

func Stop() {
	mageutil.StopAndCheckBinaries()
}

func Check() {
	mageutil.CheckAndReportBinariesStatus()
}

func Protocol() {
	mageutil.Protocol()
}
