package launchd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ServiceConfig defines a launchd user agent.
type ServiceConfig struct {
	Label        string
	ProgramArgs  []string
	Environment  map[string]string
	LogPath      string
	RunAtLoad    bool
	KeepAlive    bool
}

func plistPath(label string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// Install writes a launchd plist and loads it via launchctl.
func Install(cfg ServiceConfig) (string, error) {
	path, err := plistPath(cfg.Label)
	if err != nil {
		return "", err
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
`, cfg.Label)

	for _, arg := range cfg.ProgramArgs {
		plist += fmt.Sprintf("        <string>%s</string>\n", arg)
	}

	plist += "    </array>\n"

	if len(cfg.Environment) > 0 {
		plist += "    <key>EnvironmentVariables</key>\n    <dict>\n"
		for k, v := range cfg.Environment {
			plist += fmt.Sprintf("        <key>%s</key>\n        <string>%s</string>\n", k, v)
		}
		plist += "    </dict>\n"
	}

	if cfg.RunAtLoad {
		plist += "    <key>RunAtLoad</key>\n    <true/>\n"
	}
	if cfg.KeepAlive {
		plist += "    <key>KeepAlive</key>\n    <true/>\n"
	}
	if cfg.LogPath != "" {
		plist += fmt.Sprintf("    <key>StandardOutPath</key>\n    <string>%s</string>\n", cfg.LogPath)
		plist += fmt.Sprintf("    <key>StandardErrorPath</key>\n    <string>%s</string>\n", cfg.LogPath)
	}

	plist += "</dict>\n</plist>\n"

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return "", fmt.Errorf("failed to write plist: %w", err)
	}

	if err := exec.Command("launchctl", "load", path).Run(); err != nil {
		return path, fmt.Errorf("failed to load launch agent: %w", err)
	}

	return path, nil
}

// Uninstall unloads and removes a launchd plist by label.
func Uninstall(label string) error {
	path, err := plistPath(label)
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("service %q is not installed", label)
	}

	exec.Command("launchctl", "unload", path).Run()

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to remove plist: %w", err)
	}

	return nil
}
