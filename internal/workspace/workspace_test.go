package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("create directory for %q: %v", rel, err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		t.Fatalf("write %q: %v", rel, err)
	}
}

func openWorkspace(t *testing.T, root string, limits Limits) *Workspace {
	t.Helper()
	ws, err := Open(root, limits)
	if err != nil {
		t.Fatalf("Open(%q) error: %v", root, err)
	}
	t.Cleanup(func() {
		if err := ws.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	})
	return ws
}

func workflowPaths(files []File) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func TestWorkflowsSortedAndFiltered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".github/workflows/b.yml", "b")
	writeFile(t, root, ".github/workflows/a.yaml", "a")
	writeFile(t, root, ".github/workflows/C.yml", "C")
	writeFile(t, root, ".github/workflows/notes.txt", "ignored")
	writeFile(t, root, ".github/workflows/subdir/x.yml", "ignored")

	files, err := openWorkspace(t, root, DefaultLimits).Workflows()
	if err != nil {
		t.Fatalf("Workflows() error: %v", err)
	}
	want := []string{".github/workflows/C.yml", ".github/workflows/a.yaml", ".github/workflows/b.yml"}
	got := workflowPaths(files)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Workflows() paths = %v, want %v", got, want)
	}
}

func TestWorkflowsMissingDir(t *testing.T) {
	t.Run("no .github", func(t *testing.T) {
		files, err := openWorkspace(t, t.TempDir(), DefaultLimits).Workflows()
		if err != nil {
			t.Fatalf("Workflows() error: %v", err)
		}
		if len(files) != 0 {
			t.Fatalf("Workflows() = %v, want empty", workflowPaths(files))
		}
	})

	t.Run("no .github/workflows", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".github/dependabot.yml", "version: 2")
		files, err := openWorkspace(t, root, DefaultLimits).Workflows()
		if err != nil {
			t.Fatalf("Workflows() error: %v", err)
		}
		if len(files) != 0 {
			t.Fatalf("Workflows() = %v, want empty", workflowPaths(files))
		}
	})
}

func TestReadFileRejectsEscapes(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "secret", "outside")
	root := filepath.Join(base, "depth", "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("create root: %v", err)
	}
	ws := openWorkspace(t, root, DefaultLimits)

	for _, rel := range []string{"../../secret", "/etc/passwd", "", "a/../../x", `.github\workflows\x.yml`} {
		t.Run(rel, func(t *testing.T) {
			file, err := ws.ReadFile(rel)
			if !errors.Is(err, ErrOutsideRoot) {
				t.Fatalf("ReadFile(%q) error = %v, want ErrOutsideRoot", rel, err)
			}
			if file.Path != "" || file.Content != nil {
				t.Fatalf("ReadFile(%q) = %+v, want zero File", rel, file)
			}
		})
	}
}

func TestWorkflowsRejectsEscapingSymlink(t *testing.T) {
	base := t.TempDir()
	const canary = "OUTSIDE_SECRET_CANARY"
	outside := filepath.Join(base, "outside", "evil.yml")
	if err := os.MkdirAll(filepath.Dir(outside), 0o755); err != nil {
		t.Fatalf("create outside dir: %v", err)
	}
	if err := os.WriteFile(outside, []byte("name: "+canary+"\n"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	root := filepath.Join(base, "root")
	writeFile(t, root, ".github/workflows/good.yml", "name: good\n")
	link := filepath.Join(root, ".github", "workflows", "evil.yml")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("create escape symlink: %v", err)
	}

	files, err := openWorkspace(t, root, DefaultLimits).Workflows()
	if !errors.Is(err, ErrSymlink) {
		t.Fatalf("Workflows() error = %v, want ErrSymlink", err)
	}
	if files != nil {
		t.Fatalf("Workflows() files = %v, want nil", workflowPaths(files))
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("Workflows() error leaks outside file content: %v", err)
	}
}

func TestSymlinksInsideRootRejected(t *testing.T) {
	t.Run("workflow file symlink", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "real.yml", "name: real\n")
		writeFile(t, root, ".github/workflows/placeholder", "")
		link := filepath.Join(root, ".github", "workflows", "x.yml")
		if err := os.Symlink(filepath.Join(root, "real.yml"), link); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		_, err := openWorkspace(t, root, DefaultLimits).Workflows()
		if !errors.Is(err, ErrSymlink) {
			t.Fatalf("Workflows() error = %v, want ErrSymlink", err)
		}
	})

	t.Run("symlinked .github", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "dotgithub/workflows/ci.yml", "name: ci\n")
		if err := os.Symlink(filepath.Join(root, "dotgithub"), filepath.Join(root, ".github")); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		_, err := openWorkspace(t, root, DefaultLimits).Workflows()
		if !errors.Is(err, ErrSymlink) {
			t.Fatalf("Workflows() error = %v, want ErrSymlink", err)
		}
	})

	t.Run("symlinked intermediate directory in ReadFile", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "real/action.yml", "name: action\n")
		if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "alias")); err != nil {
			t.Fatalf("create symlink: %v", err)
		}
		_, err := openWorkspace(t, root, DefaultLimits).ReadFile("alias/action.yml")
		if !errors.Is(err, ErrSymlink) {
			t.Fatalf("ReadFile() error = %v, want ErrSymlink", err)
		}
	})
}

func TestOpenAcceptsSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	writeFile(t, realRoot, ".github/workflows/ci.yml", "name: ci\n")
	linkRoot := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}

	files, err := openWorkspace(t, linkRoot, DefaultLimits).Workflows()
	if err != nil {
		t.Fatalf("Workflows() error: %v", err)
	}
	if got := workflowPaths(files); len(got) != 1 || got[0] != ".github/workflows/ci.yml" {
		t.Fatalf("Workflows() paths = %v, want [.github/workflows/ci.yml]", got)
	}
}

func TestFileSizeLimit(t *testing.T) {
	limits := Limits{MaxFileBytes: 8, MaxTotalBytes: 1 << 20}

	t.Run("over limit", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".github/workflows/big.yml", strings.Repeat("a", int(limits.MaxFileBytes)+1))
		files, err := openWorkspace(t, root, limits).Workflows()
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Workflows() error = %v, want ErrTooLarge", err)
		}
		if files != nil {
			t.Fatalf("Workflows() files = %v, want nil", workflowPaths(files))
		}
		if !strings.Contains(err.Error(), ".github/workflows/big.yml") {
			t.Fatalf("Workflows() error %q does not name the path", err)
		}
		if !strings.Contains(err.Error(), "8") {
			t.Fatalf("Workflows() error %q does not name the limit", err)
		}
	})

	t.Run("exactly at limit", func(t *testing.T) {
		root := t.TempDir()
		content := strings.Repeat("a", int(limits.MaxFileBytes))
		writeFile(t, root, ".github/workflows/ok.yml", content)
		files, err := openWorkspace(t, root, limits).Workflows()
		if err != nil {
			t.Fatalf("Workflows() error: %v", err)
		}
		if len(files) != 1 || string(files[0].Content) != content {
			t.Fatalf("Workflows() = %+v, want one file of %d bytes", workflowPaths(files), len(content))
		}
	})
}

func TestTotalSizeLimit(t *testing.T) {
	limits := Limits{MaxFileBytes: 10, MaxTotalBytes: 12}

	t.Run("workflows", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, ".github/workflows/a.yml", "12345678")
		writeFile(t, root, ".github/workflows/b.yml", "12345678")
		files, err := openWorkspace(t, root, limits).Workflows()
		if !errors.Is(err, ErrTooLarge) {
			t.Fatalf("Workflows() error = %v, want ErrTooLarge", err)
		}
		if files != nil {
			t.Fatalf("Workflows() files = %v, want nil", workflowPaths(files))
		}
	})

	t.Run("read file calls accumulate", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "a.txt", "12345678")
		writeFile(t, root, "b.txt", "12345678")
		ws := openWorkspace(t, root, limits)
		if _, err := ws.ReadFile("a.txt"); err != nil {
			t.Fatalf("ReadFile(a.txt) error: %v", err)
		}
		if _, err := ws.ReadFile("b.txt"); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("ReadFile(b.txt) error = %v, want ErrTooLarge", err)
		}
	})
}

func TestBinaryFileRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".github/workflows/binary.yml", "name: binary\x00\n")
	files, err := openWorkspace(t, root, DefaultLimits).Workflows()
	if !errors.Is(err, ErrBinary) {
		t.Fatalf("Workflows() error = %v, want ErrBinary", err)
	}
	if files != nil {
		t.Fatalf("Workflows() files = %v, want nil", workflowPaths(files))
	}
}

func TestFIFORejected(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("mkfifo is Linux-only in this test")
	}
	tests := []struct {
		name  string
		setup func(t *testing.T, root string) string
	}{
		{
			name: "workflow file",
			setup: func(t *testing.T, root string) string {
				writeFile(t, root, ".github/workflows/placeholder", "")
				return filepath.Join(root, ".github", "workflows", "fifo.yml")
			},
		},
		{
			name: "workflows directory",
			setup: func(t *testing.T, root string) string {
				if err := os.MkdirAll(filepath.Join(root, ".github"), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				return filepath.Join(root, ".github", "workflows")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := syscall.Mkfifo(tt.setup(t, root), 0o600); err != nil {
				t.Fatalf("mkfifo: %v", err)
			}

			ws := openWorkspace(t, root, DefaultLimits)
			type result struct {
				files []File
				err   error
			}
			done := make(chan result, 1)
			go func() {
				files, err := ws.Workflows()
				done <- result{files: files, err: err}
			}()

			select {
			case got := <-done:
				if !errors.Is(got.err, ErrNotRegular) {
					t.Fatalf("Workflows() error = %v, want ErrNotRegular", got.err)
				}
				if got.files != nil {
					t.Fatalf("Workflows() files = %v, want nil", workflowPaths(got.files))
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Workflows() hung on a FIFO")
			}
		})
	}
}

func TestOpenRejectsInvalidRoot(t *testing.T) {
	base := t.TempDir()
	fileRoot := filepath.Join(base, "file")
	if err := os.WriteFile(fileRoot, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file root: %v", err)
	}
	if _, err := Open(fileRoot, DefaultLimits); err == nil {
		t.Fatal("Open(file) error = nil, want error")
	}
	if _, err := Open(filepath.Join(base, "missing"), DefaultLimits); err == nil {
		t.Fatal("Open(missing) error = nil, want error")
	}
}

func TestReadFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "action/action.yml", "name: local action\n")
	writeFile(t, root, "plain", "not a directory")
	ws := openWorkspace(t, root, DefaultLimits)

	t.Run("regular file", func(t *testing.T) {
		file, err := ws.ReadFile("action/action.yml")
		if err != nil {
			t.Fatalf("ReadFile() error: %v", err)
		}
		if file.Path != "action/action.yml" || string(file.Content) != "name: local action\n" {
			t.Fatalf("ReadFile() = %+v", file)
		}
	})

	t.Run("directory component is a file", func(t *testing.T) {
		if _, err := ws.ReadFile("plain/child"); !errors.Is(err, ErrNotRegular) {
			t.Fatalf("ReadFile() error = %v, want ErrNotRegular", err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		if _, err := ws.ReadFile("action"); !errors.Is(err, ErrNotRegular) {
			t.Fatalf("ReadFile() error = %v, want ErrNotRegular", err)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, err := ws.ReadFile("absent.yml"); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("ReadFile() error = %v, want os.ErrNotExist", err)
		}
	})
}
