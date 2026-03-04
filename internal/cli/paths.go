package cli

import (
	"os"
	"path/filepath"
)

func defaultOutputDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Documents", "Ghostwriter")
}

func defaultModelPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ghostwriter", "models", "ggml-base.en.bin")
}
