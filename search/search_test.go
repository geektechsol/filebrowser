package search

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/filebrowser/filebrowser/v2/files"
)

type dummyChecker struct {
	disallowPrefix string
}

func (d dummyChecker) Check(p string) bool {
	if d.disallowPrefix != "" && strings.HasPrefix(p, d.disallowPrefix) {
		return false
	}
	return true
}

func setupTestFs(t *testing.T) afero.Fs {
	t.Helper()
	fs := afero.NewMemMapFs()

	// Create test structure:
	// /doc1.txt
	// /image.png
	// /sub/doc2.txt
	// /sub/image2.jpg
	// /sub/deep/doc3.txt
	// /hidden/secret.txt
	files := []string{
		"/doc1.txt",
		"/image.png",
		"/sub/doc2.txt",
		"/sub/image2.jpg",
		"/sub/deep/doc3.txt",
		"/hidden/secret.txt",
	}

	for _, file := range files {
		dir := path.Dir(file)
		if dir != "/" {
			if err := fs.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		f, err := fs.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	return fs
}

func TestSearchTerms(t *testing.T) {
	fs := setupTestFs(t)

	var found []string
	err := Search(context.Background(), fs, "/", "doc", dummyChecker{}, func(p string, f os.FileInfo) error {
		found = append(found, p)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"doc1.txt", "sub/deep/doc3.txt", "sub/doc2.txt"}
	if len(found) != len(expected) {
		t.Fatalf("expected %d results, got %d: %v", len(expected), len(found), found)
	}
	for i, exp := range expected {
		if found[i] != exp {
			t.Errorf("result %d: expected %q, got %q", i, exp, found[i])
		}
	}
}

func TestSearchCaseSensitivity(t *testing.T) {
	fs := afero.NewMemMapFs()
	f, _ := fs.Create("/Readme.TXT")
	f.Close()

	// Insensitive (default)
	var found []string
	err := Search(context.Background(), fs, "/", "readme", dummyChecker{}, func(p string, f os.FileInfo) error {
		found = append(found, p)
		return nil
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("expected 1 match for case-insensitive search, got %v (err: %v)", found, err)
	}

	// Sensitive
	found = nil
	err = Search(context.Background(), fs, "/", "case:sensitive readme", dummyChecker{}, func(p string, f os.FileInfo) error {
		found = append(found, p)
		return nil
	})
	if err != nil || len(found) != 0 {
		t.Fatalf("expected 0 matches for case-sensitive 'readme', got %v", found)
	}

	found = nil
	err = Search(context.Background(), fs, "/", "case:sensitive Readme", dummyChecker{}, func(p string, f os.FileInfo) error {
		found = append(found, p)
		return nil
	})
	if err != nil || len(found) != 1 {
		t.Fatalf("expected 1 match for case-sensitive 'Readme', got %v", found)
	}
}

func TestSearchConditions(t *testing.T) {
	fs := setupTestFs(t)

	var found []string
	err := Search(context.Background(), fs, "/", "type:image", dummyChecker{}, func(p string, f os.FileInfo) error {
		found = append(found, p)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"image.png", "sub/image2.jpg"}
	if len(found) != len(expected) {
		t.Fatalf("expected %d results, got %d: %v", len(expected), len(found), found)
	}
}

func TestSearchCheckerPruning(t *testing.T) {
	fs := setupTestFs(t)

	checker := dummyChecker{disallowPrefix: "/hidden"}
	var found []string
	err := Search(context.Background(), fs, "/", "secret", checker, func(p string, f os.FileInfo) error {
		found = append(found, p)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(found) != 0 {
		t.Fatalf("expected 0 results because /hidden is disallowed, got %v", found)
	}
}

func TestSearchContextCancellation(t *testing.T) {
	fs := setupTestFs(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := Search(ctx, fs, "/", "doc", dummyChecker{}, func(p string, f os.FileInfo) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

func BenchmarkSearchFastWalk(b *testing.B) {
	tempDir := b.TempDir()
	for i := 0; i < 20; i++ {
		dir := filepath.Join(tempDir, "dir"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		for j := 0; j < 50; j++ {
			f, err := os.Create(filepath.Join(dir, "file"+strconv.Itoa(j)+".txt"))
			if err != nil {
				b.Fatal(err)
			}
			f.Close()
		}
	}

	scopedFs := files.NewScopedFs(afero.NewOsFs(), tempDir)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		count := 0
		err := Search(context.Background(), scopedFs, "/", "file10", dummyChecker{}, func(p string, f os.FileInfo) error {
			count++
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
		if count != 20 {
			b.Fatalf("expected 20, got %d", count)
		}
	}
}
