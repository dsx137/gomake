package mageutil

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func ExportMageLauncherArchived(otherPaths ...string) error {
	// 在导出前先确保构建完成
	PrintBlue("Preparing launcher archive export...")
	PrintBlue("Building binaries before export...")
	Build(nil, nil)

	PrintBlue(fmt.Sprintf("Using bin root directory: %s", Paths.OutputBin))

	platforms := os.Getenv("PLATFORMS")
	if platforms == "" {
		platforms = DetectPlatform()
	}

	platformList := strings.Fields(platforms)
	if len(platformList) == 0 {
		return fmt.Errorf("no platforms specified for export")
	}

	for _, platform := range platformList {
		platformParts := strings.Split(platform, "_")
		if len(platformParts) != 2 {
			return fmt.Errorf("invalid platform format: %s", platform)
		}
		targetOS, targetArch := platformParts[0], platformParts[1]
		if err := exportMageLauncherArchivedForPlatform(targetOS, targetArch, otherPaths); err != nil {
			return err
		}
	}
	return nil
}

func exportMageLauncherArchivedForPlatform(targetOS, targetArch string, otherPaths []string) error {
	platform := fmt.Sprintf("%s_%s", targetOS, targetArch)
	tmpDir := Paths.OutputTmp
	PrintBlue(fmt.Sprintf("Using tmp directory: %s", tmpDir))

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("failed to create tmp directory %s: %v", tmpDir, err)
	}
	mageBinaryPath := filepath.Join(tmpDir, fmt.Sprintf("mage_%s", platform))
	if targetOS == "windows" {
		mageBinaryPath += ".exe"
	}

	PrintBlue(fmt.Sprintf("Compiling mage binary for %s: mage -compile %s", platform, mageBinaryPath))
	cmd := exec.Command("mage", "-compile", mageBinaryPath, "-goos", targetOS, "-goarch", targetArch)
	cmd.Dir = Paths.Root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("GOOS=%s", targetOS),
		fmt.Sprintf("GOARCH=%s", targetArch),
	)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to compile mage for %s: %v", platform, err)
	}
	PrintGreen(fmt.Sprintf("Mage binary compiled: %s", mageBinaryPath))
	PrintBlue(fmt.Sprintf("Target platform: %s", platform))
	archiveName := fmt.Sprintf("launcher_%s.tar.gz", platform)
	archivePath := filepath.Join(tmpDir, archiveName)

	PrintBlue(fmt.Sprintf("Creating archive: %s", archivePath))
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive file %s: %v", archivePath, err)
	}
	defer archiveFile.Close()

	gzipWriter, err := gzip.NewWriterLevel(archiveFile, gzip.BestCompression)
	if err != nil {
		return fmt.Errorf("failed to create gzip writer: %v", err)
	}
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	platformBinInputPath, platformBinOutputPath, err := ensureIOPath(filepath.Join(Paths.OutputBinPath, targetOS, targetArch))
	if err != nil {
		return err
	}
	PrintBlue(fmt.Sprintf("Adding bin directory to archive for %s: %s", platform, platformBinInputPath))
	if err := addDirToTar(tarWriter, platformBinInputPath, platformBinOutputPath); err != nil {
		return fmt.Errorf("failed to add bin directory for %s: %v", platform, err)
	}
	PrintGreen("Bin directory added successfully.")

	platformToolsInputPath, platformToolsOutputPath, err := ensureIOPath(filepath.Join(Paths.OutputBinToolPath, targetOS, targetArch))
	if err != nil {
		return err
	}
	PrintBlue(fmt.Sprintf("Adding tools directory to archive for %s: %s", platform, platformToolsInputPath))
	if err := addDirToTar(tarWriter, platformToolsInputPath, platformToolsOutputPath); err != nil {
		return fmt.Errorf("failed to add tools directory for %s: %v", platform, err)
	}
	PrintGreen("Tools directory added successfully.")

	startConfigInputPath, startConfigOutputPath, err := ensureIOPath(filepath.Join(Paths.Root, StartConfigFile))
	if err != nil {
		return err
	}
	PrintBlue(fmt.Sprintf("Adding %s to archive", startConfigInputPath))
	if err := addFileToTar(tarWriter, startConfigInputPath, startConfigOutputPath); err != nil {
		return fmt.Errorf("failed to add %s to archive: %v", startConfigInputPath, err)
	}
	PrintGreen(fmt.Sprintf("%s added successfully.", startConfigInputPath))

	for _, otherPath := range otherPaths {
		inputPath, archivePath, err := ensureIOPath(otherPath)
		if err != nil {
			return err
		}
		PrintBlue(fmt.Sprintf("Adding %s to archive", inputPath))
		if err := addToTar(tarWriter, inputPath, archivePath); err != nil {
			return fmt.Errorf("failed to add %s to archive: %v", inputPath, err)
		}
	}

	if err := checkExist(mageBinaryPath); err != nil {
		return err
	}
	mageFileName := "mage"
	if targetOS == "windows" {
		mageFileName = "mage.exe"
	}
	PrintBlue(fmt.Sprintf("Adding mage binary to archive: %s", mageBinaryPath))
	if err := addFileToTar(tarWriter, mageBinaryPath, mageFileName); err != nil {
		return fmt.Errorf("failed to add mage binary to archive: %v", err)
	}
	PrintGreen(fmt.Sprintf("Mage binary added: %s", mageFileName))

	PrintGreen(fmt.Sprintf("Archive created successfully: %s", archivePath))
	return nil
}

func ensureIOPath(path string) (string, string, error) {
	if err := checkExist(path); err != nil {
		return "", "", err
	}
	outputPath, err := filepath.Rel(Paths.Root, path)
	if err != nil {
		return "", "", fmt.Errorf("failed to get relative path for %s: %v", path, err)
	}
	return path, outputPath, nil
}

func checkExist(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("file or directory does not exist: %s", path)
	}
	return fmt.Errorf("failed to stat %s: %v", path, err)
}

func addToTar(tarWriter *tar.Writer, filePath, archivePath string) error {
	info, err := os.Stat(filePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return addDirToTar(tarWriter, filePath, archivePath)
	}
	return addFileToTar(tarWriter, filePath, archivePath)
}

func addFileToTar(tarWriter *tar.Writer, filePath, archivePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    archivePath,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tarWriter.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tarWriter, file)
	return err
}

func addDirToTar(tarWriter *tar.Writer, dirPath, archiveDirName string) error {
	return filepath.Walk(dirPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 计算在归档中的相对路径
		relPath, err := filepath.Rel(dirPath, filePath)
		if err != nil {
			return err
		}

		// 跳过根目录本身
		if relPath == "." {
			return nil
		}

		archivePath := filepath.Join(archiveDirName, relPath)

		if info.IsDir() {
			// 添加目录
			header := &tar.Header{
				Name:     archivePath + "/",
				Mode:     int64(info.Mode()),
				Typeflag: tar.TypeDir,
			}
			return tarWriter.WriteHeader(header)
		}

		// 添加文件
		return addFileToTar(tarWriter, filePath, archivePath)
	})
}
