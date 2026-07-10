package contextwindow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	storeDirPerm  = 0o700
	storeFilePerm = 0o600
)

// FileStore persists each endpoint/model observation in a separate JSON file.
type FileStore struct {
	root string
}

// NewFileStore returns a file-backed observation store rooted at root.
func NewFileStore(root string) *FileStore {
	return &FileStore{root: filepath.Clean(root)}
}

// Path returns the cache file path for key.
func (s *FileStore) Path(key Key) string {
	if s == nil {
		return ""
	}
	key = NewKey(key.Provider, key.Endpoint, key.Model)
	raw, _ := json.Marshal(key)
	sum := sha256.Sum256(raw)
	return filepath.Join(s.root, hex.EncodeToString(sum[:16])+".json")
}

// Load reads one observation. A missing file is not an error.
func (s *FileStore) Load(key Key) (Observation, bool, error) {
	if s == nil || s.root == "" || s.root == "." {
		return Observation{}, false, fmt.Errorf("context window store root is empty")
	}
	key = NewKey(key.Provider, key.Endpoint, key.Model)
	path := s.Path(key)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Observation{}, false, nil
		}
		return Observation{}, false, fmt.Errorf("read context window cache %s: %w", path, err)
	}
	var observation Observation
	if err := json.Unmarshal(raw, &observation); err != nil {
		return Observation{}, false, fmt.Errorf("decode context window cache %s: %w", path, err)
	}
	if observation.Version != 1 {
		return Observation{}, false, fmt.Errorf("decode context window cache %s: unsupported version %d", path, observation.Version)
	}
	storedKey := NewKey(observation.Key.Provider, observation.Key.Endpoint, observation.Key.Model)
	if storedKey != key {
		return Observation{}, false, fmt.Errorf("decode context window cache %s: cache key mismatch", path)
	}
	return normalizeObservation(key, observation), true, nil
}

// Save atomically replaces one observation file.
func (s *FileStore) Save(key Key, observation Observation) error {
	if s == nil || s.root == "" || s.root == "." {
		return fmt.Errorf("context window store root is empty")
	}
	key = NewKey(key.Provider, key.Endpoint, key.Model)
	observation = normalizeObservation(key, observation)
	if err := os.MkdirAll(s.root, storeDirPerm); err != nil {
		return fmt.Errorf("create context window cache directory: %w", err)
	}
	raw, err := json.MarshalIndent(observation, "", "  ")
	if err != nil {
		return fmt.Errorf("encode context window cache: %w", err)
	}
	raw = append(raw, '\n')
	tmp, err := os.CreateTemp(s.root, ".context-window-*.tmp")
	if err != nil {
		return fmt.Errorf("create context window cache temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write context window cache temp file: %w", err)
	}
	if err := tmp.Chmod(storeFilePerm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod context window cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close context window cache temp file: %w", err)
	}
	path := s.Path(key)
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace context window cache %s: %w", path, err)
	}
	tmpName = ""
	return nil
}
