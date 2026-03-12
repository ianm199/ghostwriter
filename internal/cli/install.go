package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ianmclaughlin/ghostwriter/pkg/launchd"
	"github.com/spf13/cobra"
)

const serviceLabel = "com.ghostwriter.agent"

var appBundlePath string

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ghostwriter as a login service",
	Long:  `Install ghostwriter as a macOS login service. With --app-bundle, copies the .app to ~/Applications, creates a CLI symlink, and registers a launchd agent.`,
	RunE:  runInstall,
}

func init() {
	installCmd.Flags().StringVar(&appBundlePath, "app-bundle", "", "path to .app bundle to install")
}

func runInstall(cmd *cobra.Command, args []string) error {
	if appBundlePath != "" {
		return installAppBundle(appBundlePath)
	}
	return installBinaryOnly()
}

func installBinaryOnly() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	path, err := launchd.Install(launchd.ServiceConfig{
		Label:       serviceLabel,
		ProgramArgs: []string{exePath, "tray"},
		Environment: map[string]string{"PATH": os.Getenv("PATH")},
		LogPath:     "/tmp/ghostwriter.log",
		RunAtLoad:   true,
		KeepAlive:   true,
	})
	if err != nil {
		return err
	}

	fmt.Printf("Installed: %s\n", path)
	fmt.Printf("Binary:    %s\n", exePath)
	fmt.Println("Ghostwriter will start on login and restart on crash.")
	return nil
}

func installAppBundle(srcBundle string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	destApp := filepath.Join(home, "Applications", "Ghostwriter.app")
	binInApp := filepath.Join(destApp, "Contents", "MacOS", "ghostwriter")
	symlinkPath := filepath.Join(home, ".local", "bin", "ghostwriter")

	if err := os.RemoveAll(destApp); err != nil {
		return fmt.Errorf("failed to remove old app bundle: %w", err)
	}

	if err := copyDir(srcBundle, destApp); err != nil {
		return fmt.Errorf("failed to copy app bundle: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(symlinkPath), 0755); err != nil {
		return fmt.Errorf("failed to create symlink directory: %w", err)
	}
	os.Remove(symlinkPath)
	if err := os.Symlink(binInApp, symlinkPath); err != nil {
		return fmt.Errorf("failed to create symlink: %w", err)
	}

	path, err := launchd.Install(launchd.ServiceConfig{
		Label:       serviceLabel,
		ProgramArgs: []string{binInApp, "tray"},
		Environment: map[string]string{"PATH": os.Getenv("PATH")},
		LogPath:     "/tmp/ghostwriter.log",
		RunAtLoad:   true,
		KeepAlive:   true,
	})
	if err != nil {
		return err
	}

	fmt.Printf("App:       %s\n", destApp)
	fmt.Printf("Symlink:   %s → %s\n", symlinkPath, binInApp)
	fmt.Printf("Launchd:   %s\n", path)
	fmt.Println()
	fmt.Println("Ghostwriter will start on login and restart on crash.")
	fmt.Printf("Ensure %s is in your PATH.\n", filepath.Dir(symlinkPath))
	return nil
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ghostwriter login service",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := launchd.Uninstall(serviceLabel); err != nil {
			return err
		}

		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}

		appPath := filepath.Join(home, "Applications", "Ghostwriter.app")
		if _, statErr := os.Stat(appPath); statErr == nil {
			if err := os.RemoveAll(appPath); err != nil {
				return fmt.Errorf("failed to remove app bundle: %w", err)
			}
			fmt.Printf("Removed:   %s\n", appPath)
		}

		symlinkPath := filepath.Join(home, ".local", "bin", "ghostwriter")
		if _, statErr := os.Lstat(symlinkPath); statErr == nil {
			if err := os.Remove(symlinkPath); err != nil {
				return fmt.Errorf("failed to remove symlink: %w", err)
			}
			fmt.Printf("Removed:   %s\n", symlinkPath)
		}

		fmt.Println("Ghostwriter service uninstalled.")
		return nil
	},
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
