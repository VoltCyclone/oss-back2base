package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// extractFS writes the embedded payload to destDir if the stored hash
// differs from currentHash. Returns (extracted, err) so callers can force
// a docker image rebuild when the payload actually changed.
func extractFS(assets fs.FS, destDir, currentHash string) (bool, error) {
	hashFile := filepath.Join(destDir, ".extract-hash")

	if existing, err := os.ReadFile(hashFile); err == nil {
		if string(existing) == currentHash {
			return false, nil
		}
	}

	walkErr := fs.WalkDir(assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(destDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		data, err := fs.ReadFile(assets, path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		// Preserve exec bit for shell scripts — Dockerfile COPY --chmod
		// works in BuildKit only, so the Dockerfile-side fix isn't
		// universal. Doing it at extract is belt + suspenders.
		mode := fs.FileMode(0644)
		if ext := filepath.Ext(path); ext == ".sh" || ext == ".bash" {
			mode = 0755
		}
		return os.WriteFile(target, data, mode)
	})
	if walkErr != nil {
		return false, fmt.Errorf("extract assets: %w", walkErr)
	}
	if err := os.WriteFile(hashFile, []byte(currentHash), 0644); err != nil {
		return false, err
	}
	return true, nil
}

func seedEnvFile(assets fs.FS, envPath string) (bool, error) {
	if _, err := os.Stat(envPath); err == nil {
		return false, nil
	}

	data, err := fs.ReadFile(assets, "defaults/env.example")
	if err != nil {
		return false, fmt.Errorf("read embedded env.example: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(envPath), 0755); err != nil {
		return false, err
	}
	return true, os.WriteFile(envPath, data, 0644)
}
