package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultBaseURL = "http://127.0.0.1:8080"
	configDirMode  = 0o700
	configFileMode = 0o600
)

// config is the on-disk session. Only the token and server URL are stored.
type config struct {
	URL   string `json:"url,omitempty"`
	Token string `json:"token,omitempty"`
}

// configPath honours XDG_CONFIG_HOME and falls back to ~/.config.
func configPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "earnest", "config.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", fmt.Errorf("resolve config directory: %w", err)
	}
	return filepath.Join(home, ".config", "earnest", "config.json"), nil
}

func loadConfig() (config, error) {
	path, err := configPath()
	if err != nil {
		return config{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return config{}, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}

// saveConfig writes the config atomically with owner-only permissions, so a
// lost config directory cannot leak the token to other users.
func saveConfig(cfg config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), configDirMode); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(configFileMode); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

// resolveServer prefers EARNEST_URL, then the config file, then the loopback
// default the server itself uses.
func resolveServer(cfg config) string {
	if value := strings.TrimSpace(os.Getenv("EARNEST_URL")); value != "" {
		return strings.TrimRight(value, "/")
	}
	if cfg.URL != "" {
		return strings.TrimRight(cfg.URL, "/")
	}
	return defaultBaseURL
}

// resolveToken prefers EARNEST_TOKEN over the stored config token.
func resolveToken(cfg config) string {
	if value := os.Getenv("EARNEST_TOKEN"); value != "" {
		return value
	}
	return cfg.Token
}

// maskToken keeps a short prefix and suffix so the user can recognise the
// token without exposing it.
func maskToken(token string) string {
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "…" + token[len(token)-4:]
}
