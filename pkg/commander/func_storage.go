package commander

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Nesquiko/servermore/pkg/server"
)

type AbsolutePath = string

type FileSystemFunctionStorage struct {
	storageRoot AbsolutePath
}

func NewFSFunctionStorage(storageRoot AbsolutePath) (*FileSystemFunctionStorage, error) {
	rootInfo, err := os.Stat(storageRoot)
	if err != nil && os.IsNotExist(err) {
		if err := os.MkdirAll(storageRoot, 0o755); err != nil {
			return nil, fmt.Errorf("failed to create storage root %q: %w", storageRoot, err)
		}
	} else if err != nil {
		return nil, err
	} else if !rootInfo.IsDir() {
		return nil, fmt.Errorf("storage root '%s' exists, but is not directory", storageRoot)
	}

	return &FileSystemFunctionStorage{storageRoot: storageRoot}, nil
}

func BytesSha256(bytes []byte) []byte {
	h := sha256.New()
	h.Write(bytes)
	return h.Sum(nil)
}

func (s *FileSystemFunctionStorage) Save(
	funcName string,
	hash []byte,
	funcBytes []byte,
) (string, error) {
	funcPath := filepath.Join(s.storageRoot, functionFilename(funcName, hash))

	err := server.CreateFile(funcPath, funcBytes)
	if err != nil {
		return "", err
	}

	return funcPath, nil
}

func functionFilename(funcName string, hash []byte) string {
	sanitizedFilename := sanitizeFilename(funcName)
	ext := filepath.Ext(sanitizedFilename)
	base := strings.TrimSuffix(sanitizedFilename, ext)
	fname := fmt.Sprintf("%s-%X", base, hash)
	if ext == "" {
		return fname
	}
	return fmt.Sprintf("%s%s", fname, ext)
}

// filenames should look something like this "my _function-filename1.my-extention_8"
var validFilename = regexp.MustCompile(`^[a-zA-Z0-9_ -]+(\.[a-zA-Z0-9_ -]+)?$`)

const UncleanFuncFilename = "func"

func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	if !validFilename.MatchString(name) {
		return UncleanFuncFilename
	}

	return name
}
