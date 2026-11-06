package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Link is one short-link mapping, e.g. gh -> https://github.com.
type Link struct {
	Key    string `json:"key"`
	Target string `json:"target"`
}

// Store persists link mappings as a single JSON file. Writes are atomic
// (write to a temp file in the same directory, then rename) so a crash or
// power loss cannot leave a half-written file behind.
type Store struct {
	mu    sync.RWMutex
	path  string
	links map[string]string
}

type storeFile struct {
	Links map[string]string `json:"links"`
}

func loadStore(path string) (*Store, error) {
	s := &Store{path: path, links: map[string]string{}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse data file %s: %w", path, err)
	}
	if f.Links != nil {
		s.links = f.Links
	}
	return s, nil
}

func (s *Store) saveLocked() error {
	data, err := json.MarshalIndent(storeFile{Links: s.links}, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".links-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.path)
}

func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.links[key]
	return t, ok
}

// All returns every mapping sorted by key.
func (s *Store) All() []Link {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Link, 0, len(s.links))
	for k, v := range s.links {
		out = append(out, Link{Key: k, Target: v})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *Store) Set(key, target string) error {
	key, err := NormalizeKey(key)
	if err != nil {
		return err
	}
	if err := ValidateTarget(target); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.links[key] = target
	return s.saveLocked()
}

func (s *Store) Delete(key string) error {
	key, err := NormalizeKey(key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.links, key)
	return s.saveLocked()
}

var keyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// reservedKeys can never be used as link keys because they collide with the
// built-in routes.
var reservedKeys = map[string]bool{"_admin": true}

// NormalizeKey lowercases and validates a link key. Lookup is
// case-insensitive, so keys are stored in lowercase only.
func NormalizeKey(key string) (string, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return "", fmt.Errorf("key must not be empty")
	}
	if len(key) > 64 {
		return "", fmt.Errorf("key %q is too long (max 64 characters)", key)
	}
	if !keyRe.MatchString(key) {
		return "", fmt.Errorf("key %q is invalid: use lowercase letters, digits, '-' and '_', starting with a letter or digit", key)
	}
	if reservedKeys[key] {
		return "", fmt.Errorf("key %q is reserved", key)
	}
	return key, nil
}

// ValidateTarget requires a complete absolute http(s) URL.
func ValidateTarget(target string) error {
	target = strings.TrimSpace(target)
	if target == "" {
		return fmt.Errorf("target must not be empty")
	}
	if len(target) > 2048 {
		return fmt.Errorf("target is too long (max 2048 characters)")
	}
	u, err := url.Parse(target)
	if err != nil {
		return fmt.Errorf("target %q is not a valid URL: %w", target, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("target %q must start with http:// or https://", target)
	}
	if u.Host == "" {
		return fmt.Errorf("target %q has no host", target)
	}
	return nil
}
