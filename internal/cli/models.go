package cli

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var models = map[string]string{
	"tiny.en":    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin",
	"tiny":       "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.bin",
	"base.en":    "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin",
	"base":       "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin",
	"small.en":   "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin",
	"small":      "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.bin",
	"medium.en":  "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin",
	"medium":     "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.bin",
	"large-v3":             "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3.bin",
	"large-v3-turbo":       "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin",
	"large-v3-turbo-q5_0": "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo-q5_0.bin",
}

func modelsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ghostwriter", "models")
}

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage whisper models",
}

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available models",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := modelsDir()

		for name := range models {
			path := filepath.Join(dir, "ggml-"+name+".bin")
			status := "not downloaded"
			if info, err := os.Stat(path); err == nil {
				status = fmt.Sprintf("downloaded (%dMB)", info.Size()/1024/1024)
			}
			fmt.Printf("  %-12s %s\n", name, status)
		}
		return nil
	},
}

var modelsDownloadCmd = &cobra.Command{
	Use:   "download [model]",
	Short: "Download a whisper model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		url, ok := models[name]
		if !ok {
			available := make([]string, 0, len(models))
			for k := range models {
				available = append(available, k)
			}
			return fmt.Errorf("unknown model %q — available: %s", name, strings.Join(available, ", "))
		}

		return downloadModel(name, url)
	},
}

var modelsRemoveCmd = &cobra.Command{
	Use:   "remove [model]",
	Short: "Remove a downloaded model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		path := filepath.Join(modelsDir(), "ggml-"+name+".bin")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return fmt.Errorf("model %q is not downloaded", name)
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Printf("Removed %s\n", name)
		return nil
	},
}

func downloadModel(name, url string) error {
	dir := modelsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	dest := filepath.Join(dir, "ggml-"+name+".bin")
	if _, err := os.Stat(dest); err == nil {
		fmt.Printf("%s already downloaded\n", name)
		return nil
	}

	fmt.Printf("Downloading %s...\n", name)

	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmp := dest + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	written, err := io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("download failed: %w", err)
	}

	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}

	fmt.Printf("Downloaded %s (%dMB)\n", name, written/1024/1024)
	return nil
}

func init() {
	modelsCmd.AddCommand(modelsListCmd)
	modelsCmd.AddCommand(modelsDownloadCmd)
	modelsCmd.AddCommand(modelsRemoveCmd)
}
