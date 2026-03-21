package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFuncFileExists(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T) string
		wantExists   bool
		wantErr      bool
		wantErrMatch string
	}{
		{
			name: "returns true for existing file",
			setup: func(t *testing.T) string {
				t.Helper()
				path := filepath.Join(t.TempDir(), "function.bin")
				require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))
				return path
			},
			wantExists: true,
		},
		{
			name: "returns false for missing file",
			setup: func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "missing.bin")
			},
			wantExists: false,
		},
		{
			name: "returns error for directory",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			wantExists:   false,
			wantErr:      true,
			wantErrMatch: "path is a directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := tt.setup(t)

			exists, err := funcFileExists(path)

			assert.Equal(t, tt.wantExists, exists)
			if tt.wantErr {
				require.Error(t, err)
				assert.EqualError(t, err, tt.wantErrMatch)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
