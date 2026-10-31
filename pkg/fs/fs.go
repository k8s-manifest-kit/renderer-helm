package fs

import (
	"bytes"
	"errors"
	"fmt"
	iofs "io/fs"
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

// LoadChartFromFS loads a Helm chart from the chart root directory inside
// fsys, honoring `.helmignore` rules and Helm's chart loader. Helpers,
// sentinels, and the decompressed size limit are private to this package.
func LoadChartFromFS(fsys iofs.FS, root string) (*chart.Chart, error) {
	chartFS, err := iofs.Sub(fsys, root)
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

func chartIgnoreRules(fsys iofs.FS) (*ignore.Rules, error) {
	rules := ignore.Empty()
	data, err := iofs.ReadFile(fsys, ignore.HelmIgnore)
	if err == nil {
		rules, err = ignore.Parse(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", ignore.HelmIgnore, err)
		}
	} else if !errors.Is(err, iofs.ErrNotExist) {
		return nil, fmt.Errorf("failed to read %s: %w", ignore.HelmIgnore, err)
	}

	rules.AddDefaults()

	return rules, nil
}

func collectChartFiles(fsys iofs.FS, rules *ignore.Rules) ([]*archive.BufferedFile, error) {
	files := make([]*archive.BufferedFile, 0)
	err := iofs.WalkDir(fsys, ".", func(path string, entry iofs.DirEntry, walkErr error) error {
		return collectChartFile(fsys, rules, &files, path, entry, walkErr)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk filesystem: %w", err)
	}

	return files, nil
}

func collectChartFile(
	fsys iofs.FS,
	rules *ignore.Rules,
	files *[]*archive.BufferedFile,
	path string,
	entry iofs.DirEntry,
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
			return iofs.SkipDir
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

	data, err := iofs.ReadFile(fsys, path)
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
