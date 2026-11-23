package search

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"github.com/filebrowser/filebrowser/v2/rules"
)

type searchOptions struct {
	CaseSensitive bool
	Conditions    []condition
	Terms         []string
}

// Search searches for a query in a fs.
func Search(ctx context.Context,
	fs afero.Fs, scope, query string, checker rules.Checker, found func(path string, f os.FileInfo) error) error {
	search := parseSearch(query)

	scope = filepath.ToSlash(filepath.Clean(scope))
	scope = path.Join("/", scope)

	// If scope is not root, check if it's a regular file or directory.
	if scope != "/" && scope != "." && scope != "" {
		info, err := fs.Stat(scope)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			if checker != nil && !checker.Check(scope) {
				return nil
			}
			if matchEntry(scope, info, search) {
				return found("", info)
			}
			return nil
		}
	}

	return walk(ctx, fs, scope, scope, search, checker, found)
}

func matchEntry(fullPath string, f os.FileInfo, search *searchOptions) bool {
	if len(search.Conditions) > 0 {
		match := false
		for _, cond := range search.Conditions {
			if cond(fullPath) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}

	if len(search.Terms) > 0 {
		fileName := f.Name()
		if !search.CaseSensitive {
			fileName = strings.ToLower(fileName)
		}
		for _, term := range search.Terms {
			if strings.Contains(fileName, term) {
				return true
			}
		}
		return false
	}

	return true
}

func walk(ctx context.Context, fs afero.Fs, scope, currentDir string, search *searchOptions, checker rules.Checker, found func(path string, f os.FileInfo) error) error {
	if ctx.Err() != nil {
		return context.Cause(ctx)
	}

	entries, err := readDirEntries(fs, currentDir)
	if err != nil {
		// Inaccessible directory, skip it
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if ctx.Err() != nil {
			return context.Cause(ctx)
		}

		entryPath := path.Join(currentDir, entry.Name())

		if checker != nil && !checker.Check(entryPath) {
			continue
		}

		relativePath := strings.TrimPrefix(entryPath, scope)
		relativePath = strings.TrimPrefix(relativePath, "/")

		if matchEntry(entryPath, entry, search) {
			if err := found(relativePath, entry); err != nil {
				return err
			}
		}

		if entry.IsDir() {
			if err := walk(ctx, fs, scope, entryPath, search, checker, found); err != nil {
				return err
			}
		}
	}

	return nil
}

func readDirEntries(fs afero.Fs, dirPath string) ([]os.FileInfo, error) {
	f, err := fs.Open(dirPath)
	if err != nil {
		return nil, err
	}
	entries, err := f.Readdir(-1)
	closeErr := f.Close()
	if err == nil {
		return entries, closeErr
	}

	// Fallback for filesystems where Readdir(-1) is not fully supported
	f2, err2 := fs.Open(dirPath)
	if err2 != nil {
		return nil, err
	}
	names, err2 := f2.Readdirnames(-1)
	_ = f2.Close()
	if err2 != nil {
		return nil, err
	}

	var fallbackEntries []os.FileInfo
	for _, name := range names {
		subPath := path.Join(dirPath, name)
		var info os.FileInfo
		if lstater, ok := fs.(afero.Lstater); ok {
			fi, _, lerr := lstater.LstatIfPossible(subPath)
			if lerr == nil {
				info = fi
			}
		}
		if info == nil {
			fi, serr := fs.Stat(subPath)
			if serr == nil {
				info = fi
			}
		}
		if info != nil {
			fallbackEntries = append(fallbackEntries, info)
		}
	}
	return fallbackEntries, nil
}
