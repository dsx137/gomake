package mageutil

import (
	"os"
	"runtime"

	"gopkg.in/yaml.v3"
)

const (
	StartConfigFile = "start-config.yml"
)

var (
	serviceBinaries    map[string]int
	toolBinaries       []string
	MaxFileDescriptors int
)

type Config struct {
	ServiceBinaries    map[string]int `yaml:"serviceBinaries"`
	ToolBinaries       []string       `yaml:"toolBinaries"`
	MaxFileDescriptors int            `yaml:"maxFileDescriptors"`
}

func InitForSSC() {
	yamlFile, err := os.ReadFile(StartConfigFile)
	if err != nil {
		PrintRed("error reading YAML file: " + err.Error())
		os.Exit(1)
	}

	var config Config
	err = yaml.Unmarshal(yamlFile, &config)
	if err != nil {
		PrintRed("error unmarshalling YAML: " + err.Error())
		os.Exit(1)
	}

	adjustedBinaries := make(map[string]int)
	for binary, count := range config.ServiceBinaries {
		if runtime.GOOS == "windows" {
			binary += ".exe"
		}
		adjustedBinaries[binary] = count
	}

	var adjustedToolsBinaries []string
	for _, tool := range config.ToolBinaries {
		if runtime.GOOS == "windows" {
			tool += ".exe"
		}
		adjustedToolsBinaries = append(adjustedToolsBinaries, tool)
	}
	serviceBinaries = adjustedBinaries
	toolBinaries = adjustedToolsBinaries
	MaxFileDescriptors = config.MaxFileDescriptors
}
