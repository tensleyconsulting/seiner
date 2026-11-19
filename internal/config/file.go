package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// File is the config file's contents. Each key is the matching flag's
// name; the flag overrides it, and so do Google's project and
// impersonation environment variables.
type File struct {
	// InitialResource is the project, folder, or organization to start on,
	// by resource name: projects/my-proj, folders/123, organizations/456.
	InitialResource string `yaml:"initial-resource"`
	// Project was the key before initial-resource. It is read only to say
	// so.
	Project     string `yaml:"project"`
	Impersonate string `yaml:"impersonate"`
	Icons       string `yaml:"icons"`
	// IconsDir may start with ~/.
	IconsDir string `yaml:"icons-dir"`
	// LogFile may start with ~/.
	LogFile     string   `yaml:"log-file"`
	AWLocations []string `yaml:"aw-locations"`
	NoIntro     bool     `yaml:"no-intro"`
	// GoogleServiceAccounts starts the explorer with Google's service
	// accounts shown. It has no flag.
	GoogleServiceAccounts bool `yaml:"google-service-accounts"`
}

// Path is the default config file: $XDG_CONFIG_HOME/seiner/config.yaml,
// which is ~/.config/seiner/config.yaml when XDG_CONFIG_HOME is unset or
// relative, as the XDG base directory spec says. It is the same on macOS;
// os.UserConfigDir is not used because it ignores XDG_CONFIG_HOME there.
// It is "" when neither directory is known.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "seiner", "config.yaml")
}

// maxConfigFile caps the config file size so a device or stray large file
// passed via --config cannot stall or exhaust memory at startup.
const maxConfigFile = 1 << 20

// ReadFile reads the config file at path. A missing file fails with an
// error matching fs.ErrNotExist; an empty path is one. Unknown keys fail,
// so a misspelled key is not silently ignored.
func ReadFile(path string) (File, error) {
	if path == "" {
		return File{}, os.ErrNotExist
	}
	info, err := os.Stat(path)
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s: not a regular file", path)
	}
	if info.Size() > maxConfigFile {
		return File{}, fmt.Errorf("%s: %d bytes is over the %d byte limit", path, info.Size(), maxConfigFile)
	}
	fh, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer fh.Close()
	data, err := io.ReadAll(io.LimitReader(fh, maxConfigFile+1))
	if err != nil {
		return File{}, err
	}
	if len(data) > maxConfigFile {
		return File{}, fmt.Errorf("%s: exceeds %d byte limit", path, maxConfigFile)
	}
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	return f, nil
}
