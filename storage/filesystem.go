/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// FilesystemBackend writes one .blob file per chunk under Root.
type FilesystemBackend struct {
	root string
}

// NewFilesystemBackend creates (if needed) the root directory and
// returns a Backend.
func NewFilesystemBackend(root string) (*FilesystemBackend, error) {
	if root == "" {
		return nil, errors.New("storage: filesystem root required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("storage: mkdir %q: %w", root, err)
	}
	return &FilesystemBackend{root: root}, nil
}

// Root returns the filesystem root directory.
func (f *FilesystemBackend) Root() string { return f.root }

func (f *FilesystemBackend) path(cid string) string {
	return filepath.Join(f.root, cid+".blob")
}

// Put implements Backend.
func (f *FilesystemBackend) Put(ctx context.Context, cid string, ct []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return writeFileAtomic(f.path(cid), ct)
}

// Get implements Backend.
func (f *FilesystemBackend) Get(ctx context.Context, cid string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(f.path(cid))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("storage: read %q: %w", cid, err)
	}
	return b, nil
}

// Has implements Backend.
func (f *FilesystemBackend) Has(ctx context.Context, cid string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := os.Stat(f.path(cid))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Delete implements Backend.
func (f *FilesystemBackend) Delete(ctx context.Context, cid string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(f.path(cid)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("storage: remove %q: %w", cid, err)
	}
	return nil
}

// Iter implements Backend by walking the root directory.
func (f *FilesystemBackend) Iter(ctx context.Context, fn func(Entry) error) error {
	entries, err := os.ReadDir(f.root)
	if err != nil {
		return fmt.Errorf("storage: readdir %q: %w", f.root, err)
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".blob" {
			continue
		}
		cid := name[:len(name)-len(".blob")]
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		if err := fn(Entry{CID: cid, Size: int(info.Size())}); err != nil {
			return err
		}
	}
	return nil
}

// writeFileAtomic writes b to path via a temp file + rename.
func writeFileAtomic(path string, b []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".blob-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(b); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Sync(); err != nil {
		return errors.Join(err, tmp.Close())
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
