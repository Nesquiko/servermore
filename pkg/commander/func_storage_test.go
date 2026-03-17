package commander

import "testing"

func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "keeps valid filename with extension",
			input:    "my function.wasm",
			expected: "my function.wasm",
		},
		{
			name:     "keeps valid filename without extension",
			input:    "my_function-1",
			expected: "my_function-1",
		},
		{
			name:     "uses base path segment only",
			input:    "../../func.wasm",
			expected: "func.wasm",
		},
		{
			name:     "rejects nested path after base extraction",
			input:    "bad/name?.wasm",
			expected: UncleanFuncFilename,
		},
		{
			name:     "rejects multiple extensions",
			input:    "archive.tar.gz",
			expected: UncleanFuncFilename,
		},
		{
			name:     "rejects invalid punctuation",
			input:    "hello?.wasm",
			expected: UncleanFuncFilename,
		},
		{
			name:     "rejects empty filename",
			input:    "",
			expected: UncleanFuncFilename,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeFilename(tt.input)
			if got != tt.expected {
				t.Fatalf("sanitizeFilename(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}
