package helm

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"helm.sh/helm/v4/pkg/chart/loader/archive"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/ignore"
)

var (
	errIrregularChartFile = errors.New("cannot load irregular file")
	errChartFileTooLarge  = errors.New("chart file is larger than the maximum file size")
)

const maxDecompressedChartFileSize int64 = 5 * 1024 * 1024

func loadChartFromFS(fsys fs.FS, root string) (*chart.Chart, error) {
	chartFS, err := fs.Sub(fsys, root)
	if err != nil {
		return nil, fmt.Errorf("failed to open chart root %q: %w", root, err)
	}

	rules, err := chartIgnoreRules(chartFS)
	if err != nil {
		return nil, err
	}

	files, err := collectChartFiles(chartFS, rules)
	if err != nil {
		return nil, fmt.Errorf("failed to walk chart root %q: %w", root, err)
	}

	slices.SortFunc(files, func(a, b *archive.BufferedFile) int {
		return strings.Compare(a.Name, b.Name)
	})

	result, err := loader.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart files: %w", err)
	}

	return result, nil
}

func chartIgnoreRules(fsys fs.FS) (*ignore.Rules, error) {
	rules := ignore.Empty()
	data, err := fs.ReadFile(fsys, ignore.HelmIgnore)
	if err == nil {
		rules, err = ignore.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", ignore.HelmIgnore, err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read %s: %w", ignore.HelmIgnore, err)
	}

	rules.AddDefaults()

	return rules, nil
}

func collectChartFiles(fsys fs.FS, rules *ignore.Rules) ([]*archive.BufferedFile, error) {
	files := make([]*archive.BufferedFile, 0)
	err := fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		return collectChartFile(fsys, rules, &files, path, entry, walkErr)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk filesystem: %w", err)
	}

	return files, nil
}

func collectChartFile(
	fsys fs.FS,
	rules *ignore.Rules,
	files *[]*archive.BufferedFile,
	path string,
	entry fs.DirEntry,
	walkErr error,
) error {
	if walkErr != nil {
		return walkErr
	}
	if path == "." {
		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("failed to stat %q: %w", path, err)
	}

	if info.IsDir() {
		if rules.Ignore(path, info) {
			return fs.SkipDir
		}

		return nil
	}
	if rules.Ignore(path, info) {
		return nil
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %q", errIrregularChartFile, path)
	}
	if info.Size() > maxDecompressedChartFileSize {
		return fmt.Errorf("%w: %q (limit %d)", errChartFileTooLarge, path, maxDecompressedChartFileSize)
	}

	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("failed to read chart file %q: %w", path, err)
	}

	*files = append(*files, &archive.BufferedFile{
		Name:    path,
		ModTime: info.ModTime(),
		Data:    bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}),
	})

	return nil
}
