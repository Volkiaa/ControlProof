package workspace

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var (
	ErrOutsideRoot = errors.New("path outside workspace root")
	ErrSymlink     = errors.New("symlink not allowed")
	ErrNotRegular  = errors.New("not a regular file")
	ErrTooLarge    = errors.New("file too large")
	ErrBinary      = errors.New("binary file")
)

const workflowsDir = ".github/workflows"

type Limits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
}

var DefaultLimits = Limits{MaxFileBytes: 1 << 20, MaxTotalBytes: 16 << 20}

type File struct {
	Path    string
	Content []byte
}

type Workspace struct {
	root   *os.Root
	limits Limits
	total  int64
}

func Open(root string, limits Limits) (*Workspace, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace root %q: %w", root, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, fmt.Errorf("stat workspace root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("workspace root %q is not a directory", root)
	}
	handle, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, fmt.Errorf("open workspace root %q: %w", root, err)
	}
	return &Workspace{root: handle, limits: limits}, nil
}

func (w *Workspace) Close() error {
	if err := w.root.Close(); err != nil {
		return fmt.Errorf("close workspace: %w", err)
	}
	return nil
}

func (w *Workspace) Workflows() ([]File, error) {
	info, err := w.lstatChain(workflowsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []File{}, nil
		}
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory: %w", workflowsDir, ErrNotRegular)
	}

	dir, err := w.root.Open(workflowsDir)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", workflowsDir, err)
	}
	defer dir.Close()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("read directory %s: %w", workflowsDir, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		rel := workflowsDir + "/" + entry.Name()
		info, err := w.root.Lstat(rel)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", rel, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink: %w", rel, ErrSymlink)
		}
		if info.IsDir() || !isWorkflowName(entry.Name()) {
			continue
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s is not a regular file: %w", rel, ErrNotRegular)
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)

	files := make([]File, 0, len(paths))
	for _, rel := range paths {
		file, err := w.readFile(rel)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

func (w *Workspace) ReadFile(rel string) (File, error) {
	cleaned, err := cleanRel(rel)
	if err != nil {
		return File{}, err
	}
	return w.readFile(cleaned)
}

func (w *Workspace) readFile(rel string) (File, error) {
	info, err := w.lstatChain(rel)
	if err != nil {
		return File{}, err
	}
	if !info.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s is not a regular file: %w", rel, ErrNotRegular)
	}
	if info.Size() > w.limits.MaxFileBytes {
		return File{}, fmt.Errorf("%s: size %d exceeds limit %d: %w", rel, info.Size(), w.limits.MaxFileBytes, ErrTooLarge)
	}

	handle, err := w.root.Open(rel)
	if err != nil {
		return File{}, fmt.Errorf("open %s: %w", rel, err)
	}
	defer handle.Close()

	openedInfo, err := handle.Stat()
	if err != nil {
		return File{}, fmt.Errorf("stat %s: %w", rel, err)
	}
	if !openedInfo.Mode().IsRegular() {
		return File{}, fmt.Errorf("%s is not a regular file: %w", rel, ErrNotRegular)
	}
	if openedInfo.Size() > w.limits.MaxFileBytes {
		return File{}, fmt.Errorf("%s: size %d exceeds limit %d: %w", rel, openedInfo.Size(), w.limits.MaxFileBytes, ErrTooLarge)
	}

	content, err := io.ReadAll(io.LimitReader(handle, w.limits.MaxFileBytes+1))
	if err != nil {
		return File{}, fmt.Errorf("read %s: %w", rel, err)
	}
	if int64(len(content)) > w.limits.MaxFileBytes {
		return File{}, fmt.Errorf("%s: size exceeds limit %d: %w", rel, w.limits.MaxFileBytes, ErrTooLarge)
	}
	if bytes.IndexByte(content, 0) >= 0 {
		return File{}, fmt.Errorf("%s contains a NUL byte: %w", rel, ErrBinary)
	}

	total := w.total + int64(len(content))
	if total > w.limits.MaxTotalBytes {
		return File{}, fmt.Errorf("%s: total size %d exceeds limit %d: %w", rel, total, w.limits.MaxTotalBytes, ErrTooLarge)
	}
	w.total = total

	return File{Path: rel, Content: content}, nil
}

// os.Root follows symlinks that stay inside the root, so each component is checked.
func (w *Workspace) lstatChain(rel string) (fs.FileInfo, error) {
	parts := strings.Split(rel, "/")
	var final fs.FileInfo
	for i := range parts {
		current := strings.Join(parts[:i+1], "/")
		info, err := w.root.Lstat(current)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", current, err)
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("%s is a symlink: %w", current, ErrSymlink)
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory: %w", current, ErrNotRegular)
		}
		final = info
	}
	return final, nil
}

func cleanRel(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path: %w", ErrOutsideRoot)
	}
	if filepath.VolumeName(rel) != "" || strings.ContainsRune(rel, '\\') {
		return "", fmt.Errorf("%s: %w", rel, ErrOutsideRoot)
	}
	if path.IsAbs(rel) {
		return "", fmt.Errorf("%s: %w", rel, ErrOutsideRoot)
	}
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("%s: %w", rel, ErrOutsideRoot)
	}
	return cleaned, nil
}

func isWorkflowName(name string) bool {
	ext := path.Ext(name)
	return ext == ".yml" || ext == ".yaml"
}
