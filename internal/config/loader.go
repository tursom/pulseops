package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

func LoadGlobal(baseDir, path string) (Config, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read global config: %w", err)
	}
	var cfg Config
	if err := toml.Unmarshal(content, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode global config: %w", err)
	}
	cfg.BaseDir = baseDir
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func LoadTaskSpec(global Config, path string) (TaskSpec, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return TaskSpec{}, fmt.Errorf("read task config: %w", err)
	}
	var spec TaskSpec
	if err := toml.Unmarshal(content, &spec); err != nil {
		return TaskSpec{}, fmt.Errorf("decode task config: %w", err)
	}
	spec.SourcePath = filepath.Clean(path)
	spec.SourceHash = hashContent(content)
	spec.Normalize(global)
	if err := spec.ValidateBasic(); err != nil {
		return TaskSpec{}, err
	}
	return spec, nil
}

func LoadTaskSpecs(global Config) ([]TaskSpec, error) {
	entries, err := os.ReadDir(global.Task.ConfigDir)
	if err != nil {
		return nil, fmt.Errorf("read task config dir: %w", err)
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		paths = append(paths, filepath.Join(global.Task.ConfigDir, entry.Name()))
	}
	sort.Strings(paths)
	specs := make([]TaskSpec, 0, len(paths))
	for _, path := range paths {
		spec, err := LoadTaskSpec(global, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func hashContent(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
