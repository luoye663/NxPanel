package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func preflightArchiveSources(ctx context.Context, paths []string, limits archiveLimits) error {
	b := newArchiveBudget(limits)
	return walkArchiveSources(ctx, paths, func(_ string, info os.FileInfo, name string) error {
		size := info.Size()
		if info.IsDir() {
			size = 0
		}
		if err := b.checkEntry(name, size); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			return b.addInput(info.Size())
		}
		return nil
	})
}

func walkArchiveSources(ctx context.Context, paths []string, fn func(string, os.FileInfo, string) error) error {
	for _, root := range paths {
		err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := rejectSymlinkForArchive(path, info); err != nil {
				return err
			}
			if !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("不支持归档特殊文件: %s", path)
			}
			name, err := filepath.Rel(filepath.Dir(root), path)
			if err != nil {
				return err
			}
			return fn(path, info, filepath.ToSlash(name))
		})
		if err != nil {
			return fmt.Errorf("打包 %s 失败: %w", root, err)
		}
	}
	return nil
}

func writeZipSources(ctx context.Context, zw *zip.Writer, paths []string, b *archiveBudget) error {
	return walkArchiveSources(ctx, paths, func(path string, info os.FileInfo, name string) error {
		size := info.Size()
		if info.IsDir() {
			size = 0
		}
		if err := b.checkEntry(name, size); err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		if info.IsDir() {
			header.Name += "/"
		}
		entry, err := zw.CreateHeader(header)
		if err != nil || info.IsDir() {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := copyArchiveInput(ctx, entry, file, b, info.Size())
		closeErr := file.Close()
		return joinCopyCloseErrors(copyErr, closeErr)
	})
}

func writeTarSources(ctx context.Context, tw *tar.Writer, paths []string, b *archiveBudget) error {
	return walkArchiveSources(ctx, paths, func(path string, info os.FileInfo, name string) error {
		size := info.Size()
		if info.IsDir() {
			size = 0
		}
		if err := b.checkEntry(name, size); err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if info.IsDir() {
			header.Name += "/"
		}
		if err := tw.WriteHeader(header); err != nil || info.IsDir() {
			return err
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := copyArchiveInput(ctx, tw, file, b, info.Size())
		closeErr := file.Close()
		return joinCopyCloseErrors(copyErr, closeErr)
	})
}

func compressToZipWithLimits(parent context.Context, paths []string, outputPath string, limits archiveLimits) error {
	for _, source := range paths {
		if pathsOverlap(source, outputPath) {
			return fmt.Errorf("输出路径不能与压缩源重叠")
		}
	}
	ctx, cancel := context.WithTimeout(parent, limits.timeout)
	defer cancel()
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), ".nxpanel-archive-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	b := newArchiveBudget(limits)
	zw := zip.NewWriter(&budgetWriter{dst: tmp, budget: b})
	if err := writeZipSources(ctx, zw, paths, b); err != nil {
		_ = zw.Close()
		_ = tmp.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return err
	}
	ok = true
	return nil
}

func compressToTarGzWithLimits(parent context.Context, paths []string, outputPath string, limits archiveLimits) error {
	for _, source := range paths {
		if pathsOverlap(source, outputPath) {
			return fmt.Errorf("输出路径不能与压缩源重叠")
		}
	}
	ctx, cancel := context.WithTimeout(parent, limits.timeout)
	defer cancel()
	tmp, err := os.CreateTemp(filepath.Dir(outputPath), ".nxpanel-archive-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpPath)
		}
	}()
	b := newArchiveBudget(limits)
	gw := gzip.NewWriter(&budgetWriter{dst: tmp, budget: b})
	tw := tar.NewWriter(gw)
	if err := writeTarSources(ctx, tw, paths, b); err != nil {
		_ = tw.Close()
		_ = gw.Close()
		_ = tmp.Close()
		return err
	}
	if err := tw.Close(); err != nil {
		_ = gw.Close()
		_ = tmp.Close()
		return err
	}
	if err := gw.Close(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		return err
	}
	ok = true
	return nil
}

func extractZipWithLimits(ctx context.Context, policy *PathPolicy, archivePath, destDir string, limits archiveLimits) error {
	if pathsOverlap(archivePath, destDir) {
		return fmt.Errorf("压缩包路径与解压目录不能重叠")
	}
	if err := preflightZip(ctx, policy, archivePath, destDir, limits); err != nil {
		return err
	}
	if _, err := checkArchiveFileSize(archivePath, limits); err != nil {
		return err
	}
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer r.Close()
	b := newArchiveBudget(limits)
	created := make([]string, 0, len(r.File))
	success := false
	defer func() {
		if !success {
			cleanupExtractedPaths(created, destDir)
		}
	}()
	for _, entry := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许解压符号链接条目: %s", entry.Name)
		}
		if !entry.FileInfo().IsDir() && !entry.Mode().IsRegular() {
			return fmt.Errorf("不允许解压特殊条目: %s", entry.Name)
		}
		size := int64(entry.UncompressedSize64)
		if uint64(size) != entry.UncompressedSize64 {
			return budgetError("entry is too large: %s", entry.Name)
		}
		if err := b.checkEntry(entry.Name, size); err != nil {
			return err
		}
		if err := b.checkRatio(size, int64(entry.CompressedSize64), entry.Name); err != nil && !entry.FileInfo().IsDir() {
			return err
		}
		target, err := safeExtractTarget(policy, destDir, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			_, statErr := os.Stat(target)
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			if os.IsNotExist(statErr) {
				created = append(created, target)
			}
			continue
		}
		src, err := entry.Open()
		if err != nil {
			return err
		}
		_, statErr := os.Stat(target)
		copyErr := extractEntryAtomically(ctx, target, entry.Mode(), src, b, size)
		closeErr := src.Close()
		if err := joinCopyCloseErrors(copyErr, closeErr); err != nil {
			return err
		}
		if os.IsNotExist(statErr) {
			created = append(created, target)
		}
	}
	success = true
	return nil
}

func preflightZip(ctx context.Context, policy *PathPolicy, archivePath, destDir string, limits archiveLimits) error {
	if _, err := checkArchiveFileSize(archivePath, limits); err != nil {
		return err
	}
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer r.Close()
	b := newArchiveBudget(limits)
	for _, entry := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		info := entry.FileInfo()
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("不允许解压符号链接条目: %s", entry.Name)
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("不允许解压特殊条目: %s", entry.Name)
		}
		size := int64(entry.UncompressedSize64)
		if uint64(size) != entry.UncompressedSize64 {
			return budgetError("entry is too large: %s", entry.Name)
		}
		if err := b.checkEntry(entry.Name, size); err != nil {
			return err
		}
		if !info.IsDir() {
			if err := b.checkRatio(size, int64(entry.CompressedSize64), entry.Name); err != nil {
				return err
			}
			if err := b.addExtracted(size); err != nil {
				return err
			}
		}
		if _, err := safeExtractTarget(policy, destDir, entry.Name); err != nil {
			return err
		}
	}
	return nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func extractTarWithLimits(ctx context.Context, policy *PathPolicy, archivePath, destDir string, gzipped bool, limits archiveLimits) error {
	if pathsOverlap(archivePath, destDir) {
		return fmt.Errorf("压缩包路径与解压目录不能重叠")
	}
	if err := preflightTar(ctx, policy, archivePath, destDir, gzipped, limits); err != nil {
		return err
	}
	if _, err := checkArchiveFileSize(archivePath, limits); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer file.Close()
	compressed := &countingReader{r: file}
	var reader io.Reader = compressed
	var gr *gzip.Reader
	if gzipped {
		gr, err = gzip.NewReader(compressed)
		if err != nil {
			return err
		}
		defer gr.Close()
		reader = gr
	}
	b := newArchiveBudget(limits)
	tr := tar.NewReader(reader)
	created := []string{}
	success := false
	defer func() {
		if !success {
			cleanupExtractedPaths(created, destDir)
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("读取 tar 条目失败: %w", err)
		}
		if err := b.checkEntry(header.Name, header.Size); err != nil {
			return err
		}
		target, err := safeExtractTarget(policy, destDir, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			_, statErr := os.Stat(target)
			if err := os.MkdirAll(target, 0755); err != nil {
				return err
			}
			if os.IsNotExist(statErr) {
				created = append(created, target)
			}
		case tar.TypeReg, tar.TypeRegA:
			_, statErr := os.Stat(target)
			if err := extractEntryAtomically(ctx, target, os.FileMode(header.Mode), tr, b, header.Size); err != nil {
				return err
			}
			if os.IsNotExist(statErr) {
				created = append(created, target)
			}
			if gzipped {
				if err := b.checkRatio(b.extracted, compressed.n, header.Name); err != nil {
					return err
				}
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("不允许解压链接条目: %s", header.Name)
		default:
			return fmt.Errorf("不允许解压特殊条目: %s", header.Name)
		}
	}
	success = true
	return nil
}

func preflightTar(ctx context.Context, policy *PathPolicy, archivePath, destDir string, gzipped bool, limits archiveLimits) error {
	if _, err := checkArchiveFileSize(archivePath, limits); err != nil {
		return err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("打开压缩包失败: %w", err)
	}
	defer file.Close()
	compressed := &countingReader{r: file}
	var reader io.Reader = compressed
	var gr *gzip.Reader
	if gzipped {
		gr, err = gzip.NewReader(compressed)
		if err != nil {
			return fmt.Errorf("创建 gzip 读取器失败: %w", err)
		}
		defer gr.Close()
		reader = gr
	}
	b := newArchiveBudget(limits)
	tr := tar.NewReader(reader)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取 tar 条目失败: %w", err)
		}
		if err := b.checkEntry(header.Name, header.Size); err != nil {
			return err
		}
		if _, err := safeExtractTarget(policy, destDir, header.Name); err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			continue
		case tar.TypeReg, tar.TypeRegA:
			if _, err := copyArchiveOutput(ctx, io.Discard, tr, b, header.Size); err != nil {
				return err
			}
			if gzipped {
				if err := b.checkRatio(b.extracted, compressed.n, header.Name); err != nil {
					return err
				}
			}
		case tar.TypeSymlink, tar.TypeLink:
			return fmt.Errorf("不允许解压链接条目: %s", header.Name)
		default:
			return fmt.Errorf("不允许解压特殊条目: %s", header.Name)
		}
	}
}

func extractEntryAtomically(ctx context.Context, target string, mode os.FileMode, src io.Reader, b *archiveBudget, size int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".nxpanel-extract-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode.Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	_, copyErr := copyArchiveOutput(ctx, tmp, src, b, size)
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmpPath, target)
}
