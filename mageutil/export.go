package mageutil

import (
	"archive/tar"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/openimsdk/gomake/internal/util"
)

func ExportMageLauncherArchived(overrideMappingPaths map[string]string) error {
	PrintBlue("Preparing launcher archive export...")
	PrintBlue("Building binaries before export...")
	restoreEnv, err := util.SetEnvs(map[string]string{
		"RELEASE":  "true",
		"COMPRESS": "true",
	})
	if err != nil {
		return err
	}
	defer restoreEnv()
	Build(nil, nil, nil)

	tmpDir := Paths.OutputTmp
	PrintBlue(fmt.Sprintf("Using tmp directory: %s", tmpDir))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory %s: %v", tmpDir, err)
	}

	platforms := os.Getenv("PLATFORMS")
	if platforms == "" {
		platforms = DetectPlatform()
	}

	platformList := strings.Fields(platforms)
	if len(platformList) == 0 {
		return fmt.Errorf("no platforms specified for export")
	}

	for _, platform := range platformList {
		PrintBlue(fmt.Sprintf("Target platform: %s", platform))
		platformParts := strings.SplitN(platform, "_", 2)
		if len(platformParts) != 2 {
			return fmt.Errorf("invalid platform format: %s", platform)
		}
		targetOS, targetArch := platformParts[0], platformParts[1]

		mageBinaryPath := filepath.Join(tmpDir, fmt.Sprintf("mage_%s", platform))
		if targetOS == "windows" {
			mageBinaryPath += ".exe"
		}
		PrintBlue(fmt.Sprintf("Compiling mage binary for %s: mage -compile %s", platform, mageBinaryPath))
		cmd := exec.Command("mage", "-compile", mageBinaryPath, "-goos", targetOS, "-goarch", targetArch, "-ldflags", "-s -w")
		cmd.Dir = Paths.Root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to compile mage for %s: %v", platform, err)
		}
		PrintGreen(fmt.Sprintf("Mage binary compiled: %s", mageBinaryPath))

		mappingPaths, err := EnsureRootRelPaths(
			filepath.Join(Paths.OutputBinPath, targetOS, targetArch),
			filepath.Join(Paths.OutputBinToolPath, targetOS, targetArch),
			filepath.Join(Paths.Root, StartConfigFile),
		)
		if err != nil {
			return err
		}

		mageInPath := mageBinaryPath
		mageOutPath := "mage"
		if targetOS == "windows" {
			mageOutPath = "mage.exe"
		}

		mappingPaths[mageInPath] = mageOutPath
		for k, v := range overrideMappingPaths {
			mappingPaths[k] = v
		}

		err = archive(filepath.Join(tmpDir, fmt.Sprintf("launcher_%s.tar.zst", platform)), mappingPaths)
		if err != nil {
			return err
		}
	}
	return nil
}

func archive(archivePath string, mappingPaths map[string]string) error {
	PrintBlue(fmt.Sprintf("Creating archive: %s", archivePath))
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive file %s: %v", archivePath, err)
	}
	defer archiveFile.Close()
	//gzipWriter, err := gzip.NewWriterLevel(archiveFile, gzip.BestCompression)
	//if err != nil {
	//	return fmt.Errorf("failed to create gzip writer: %v", err)
	//}
	//defer gzipWriter.Close()
	zstdWriter, err := zstd.NewWriter(archiveFile, zstd.WithEncoderLevel(zstd.SpeedBestCompression))
	if err != nil {
		return fmt.Errorf("failed to create zstd writer: %v", err)
	}
	defer zstdWriter.Close()
	tarWriter := tar.NewWriter(zstdWriter)
	defer tarWriter.Close()

	for in, out := range mappingPaths {
		err := util.CheckExist(in)
		if err != nil {
			return err
		}

		PrintBlue(fmt.Sprintf("Adding %s to archive", in))
		if err := util.AddToTar(tarWriter, in, out); err != nil {
			return fmt.Errorf("failed to add %s to archive: %v", in, err)
		}
	}

	PrintGreen(fmt.Sprintf("Archive created successfully: %s", archivePath))
	return nil
}

func EnsureRootRelPaths(paths ...string) (map[string]string, error) {
	relPathMap := make(map[string]string)
	for _, path := range paths {
		relPath, err := filepath.Rel(Paths.Root, path)
		if err != nil {
			return nil, fmt.Errorf("failed to get relative path for %s: %v", path, err)
		}
		relPathMap[path] = relPath
	}

	return relPathMap, nil
}
