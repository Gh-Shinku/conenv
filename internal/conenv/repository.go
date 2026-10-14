package conenv

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const RepositoryEnvironment = "CONENV_REPO"

type RepositorySelection struct {
	Path   string `json:"path"`
	Source string `json:"source"`
}

type userConfig struct {
	Repository string `json:"repository"`
}

func ResolveRepository(explicit string) (RepositorySelection, error) {
	if explicit != "" {
		path, err := validateRepository(explicit)
		return RepositorySelection{Path: path, Source: "flag"}, err
	}
	if path := os.Getenv(RepositoryEnvironment); path != "" {
		validated, err := validateRepository(path)
		return RepositorySelection{Path: validated, Source: RepositoryEnvironment}, err
	}
	config, err := loadUserConfig()
	if err != nil {
		return RepositorySelection{}, err
	}
	if config.Repository != "" {
		path, err := validateRepository(config.Repository)
		return RepositorySelection{Path: path, Source: "user config"}, err
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return RepositorySelection{}, err
	}
	path, err := validateRepository(workingDirectory)
	return RepositorySelection{Path: path, Source: "working directory"}, err
}

func SaveRepository(path string) (RepositorySelection, error) {
	validated, err := validateRepository(path)
	if err != nil {
		return RepositorySelection{}, err
	}
	configPath, err := userConfigPath()
	if err != nil {
		return RepositorySelection{}, err
	}
	data, err := json.MarshalIndent(userConfig{Repository: validated}, "", "  ")
	if err != nil {
		return RepositorySelection{}, err
	}
	if err := atomicWrite(configPath, append(data, '\n'), 0o600); err != nil {
		return RepositorySelection{}, err
	}
	return RepositorySelection{Path: validated, Source: "user config"}, nil
}

func ClearRepository() error {
	configPath, err := userConfigPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(userConfig{}, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(configPath, append(data, '\n'), 0o600)
}

func UserConfigPath() (string, error) {
	return userConfigPath()
}

func loadUserConfig() (userConfig, error) {
	path, err := userConfigPath()
	if err != nil {
		return userConfig{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return userConfig{}, nil
	}
	if err != nil {
		return userConfig{}, err
	}
	var config userConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return userConfig{}, fmt.Errorf("decode %s: %w", path, err)
	}
	return config, nil
}

func userConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".conenv", "config.json"), nil
}

func validateRepository(path string) (string, error) {
	expanded, err := expandUserPath(path)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("configuration repository %s: %w", absolute, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("configuration repository %s is not a directory", absolute)
	}
	manifest := filepath.Join(absolute, "conenv.yaml")
	if _, err := os.Stat(manifest); err != nil {
		return "", fmt.Errorf("configuration repository is missing conenv.yaml: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func expandUserPath(path string) (string, error) {
	if path == "~" || len(path) > 1 && path[0] == '~' && os.IsPathSeparator(path[1]) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			return home, nil
		}
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}
