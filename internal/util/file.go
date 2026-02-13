package util

import (
	"fmt"
	"os"
)

func CheckExist(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return nil
	}
	if os.IsNotExist(err) {
		return fmt.Errorf("file or directory does not exist: %s", path)
	}
	return fmt.Errorf("failed to stat %s: %v", path, err)
}
