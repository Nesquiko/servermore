package server

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/Nesquiko/servermore/pkg/assert"
)

type AbsolutePath = string

func CreateFile(filePath AbsolutePath, bytes []byte) error {
	if err := os.WriteFile(filePath, bytes, 0o755); err != nil {
		DeleteFile(filePath)
		return err
	}

	return nil
}

func DeleteFile(fPath AbsolutePath) {
	if err := os.Remove(fPath); err != nil {
		slog.Error(
			"there was an error deleting file",
			"fPath", fPath,
			"error", err,
		)
	}
}

func CreateDirIfNotExists(path AbsolutePath) error {
	assert.That(path != "", "path can't be empty")

	rootInfo, err := os.Stat(path)
	if err != nil && errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("failed to create dir %q: %w", path, err)
		}
	} else if err != nil {
		return err
	} else if !rootInfo.IsDir() {
		return fmt.Errorf("path '%s' exists, but is not directory", path)
	}
	return nil
}
