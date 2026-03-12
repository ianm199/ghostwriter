package cli

import (
	"os"
	"path/filepath"
)

func defaultOutputDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "Ghostwriter")
}

func googleTokenPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ghostwriter", "google-token.json")
}

func defaultModelPath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "share", "ghostwriter", "models")

	preferred := []string{
		"ggml-large-v3-turbo-q5_0.bin",
		"ggml-large-v3-turbo.bin",
		"ggml-large-v3.bin",
		"ggml-medium.en.bin",
		"ggml-medium.bin",
		"ggml-small.en.bin",
		"ggml-small.bin",
		"ggml-base.en.bin",
		"ggml-base.bin",
		"ggml-tiny.en.bin",
		"ggml-tiny.bin",
	}
	for _, name := range preferred {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return filepath.Join(dir, "ggml-base.en.bin")
}
