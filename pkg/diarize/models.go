package diarize

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	segmentationURL  = "https://github.com/k2-fsa/sherpa-onnx/releases/download/speaker-segmentation-models/sherpa-onnx-pyannote-segmentation-3-0.tar.bz2"
	embeddingURL     = "https://github.com/k2-fsa/sherpa-onnx/releases/download/speaker-recongition-models/3dspeaker_speech_eres2net_base_sv_zh-cn_3dspeaker_16k.onnx"
	segmentationFile = "segmentation.onnx"
	embeddingFile    = "embedding.onnx"
)

func ModelsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "ghostwriter", "models", "diarize")
}

func DefaultSegmentationModelPath() string {
	return filepath.Join(ModelsDir(), segmentationFile)
}

func DefaultEmbeddingModelPath() string {
	return filepath.Join(ModelsDir(), embeddingFile)
}

func ModelsDownloaded() bool {
	_, segErr := os.Stat(DefaultSegmentationModelPath())
	_, embErr := os.Stat(DefaultEmbeddingModelPath())
	return segErr == nil && embErr == nil
}

func EnsureModels() error {
	dir := ModelsDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create models directory: %w", err)
	}

	segPath := DefaultSegmentationModelPath()
	if _, err := os.Stat(segPath); os.IsNotExist(err) {
		fmt.Println("Downloading segmentation model...")
		if err := downloadSegmentationModel(segPath); err != nil {
			return fmt.Errorf("failed to download segmentation model: %w", err)
		}
	}

	embPath := DefaultEmbeddingModelPath()
	if _, err := os.Stat(embPath); os.IsNotExist(err) {
		fmt.Println("Downloading embedding model...")
		if err := downloadFile(embeddingURL, embPath); err != nil {
			return fmt.Errorf("failed to download embedding model: %w", err)
		}
	}

	return nil
}

func downloadSegmentationModel(dest string) error {
	resp, err := http.Get(segmentationURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	bzReader := bzip2.NewReader(resp.Body)
	tarReader := tar.NewReader(bzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}

		if strings.HasSuffix(header.Name, "model.onnx") && header.Typeflag == tar.TypeReg {
			tmp := dest + ".tmp"
			f, err := os.Create(tmp)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tarReader)
			f.Close()
			if err != nil {
				os.Remove(tmp)
				return err
			}
			if err := os.Rename(tmp, dest); err != nil {
				os.Remove(tmp)
				return err
			}
			fmt.Printf("Downloaded segmentation model (%dMB)\n", header.Size/1024/1024)
			return nil
		}
	}

	return fmt.Errorf("model.onnx not found in archive")
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
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
		return err
	}

	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}

	fmt.Printf("Downloaded embedding model (%dMB)\n", written/1024/1024)
	return nil
}
