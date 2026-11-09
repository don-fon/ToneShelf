package store

import (
	"errors"
	"path/filepath"
	"strings"
)

type Option func(*Store) error

func WithDataRoot(root string) Option {
	return func(s *Store) error { var err error; s.root, err = filepath.Abs(root); return err }
}

// The database stores portable paths; application services receive absolute paths.
func (s *Store) storedPath(path string) (string, error) {
	if s.root == "" || path == "" {
		return path, nil
	}
	if filepath.IsAbs(path) {
		relative, err := filepath.Rel(s.root, path)
		if err != nil {
			return "", err
		}
		path = filepath.ToSlash(relative)
	} else {
		path = strings.ReplaceAll(path, "\\", "/")
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	if !strings.HasPrefix(clean, "media/") || strings.Contains(clean, ":") || !filepath.IsLocal(filepath.FromSlash(clean)) {
		return "", errors.New("media path must stay inside the data root's media directory")
	}
	return clean, nil
}

func (s *Store) resolvedPath(path string) (string, error) {
	path, err := s.storedPath(path)
	if err != nil || path == "" || s.root == "" {
		return path, err
	}
	return filepath.Join(s.root, filepath.FromSlash(path)), nil
}

func (s *Store) paths(resolve bool, fields ...*string) error {
	for _, field := range fields {
		var err error
		if resolve {
			*field, err = s.resolvedPath(*field)
		} else {
			*field, err = s.storedPath(*field)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) newTrackPaths(n NewTrack, resolve bool) (NewTrack, error) {
	err := s.paths(resolve, &n.OriginalPath, &n.PlayablePath, &n.CoverPath)
	return n, err
}
func (s *Store) resolveTrack(t Track) (Track, error) {
	err := s.paths(true, &t.OriginalPath, &t.PlayablePath, &t.CoverPath)
	return t, err
}
func (s *Store) filePaths(files []PendingFile) ([]PendingFile, error) {
	result := append([]PendingFile(nil), files...)
	for i := range result {
		var err error
		result[i].Path, err = s.storedPath(result[i].Path)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
