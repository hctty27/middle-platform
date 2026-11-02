// 生成跨平台一致的 Linux tar.gz 发布包，并显式写入 Unix 执行权限。
package main

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	source := flag.String("source", "", "build staging directory")
	output := flag.String("output", "", "destination tar.gz file")
	flag.Parse()

	if *source == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "usage: package-supervisor-archive -source <dir> -output <file.tar.gz>")
		os.Exit(2)
	}
	if err := writeArchive(*source, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func writeArchive(source, output string) (err error) {
	for _, name := range []string{"config-center", "config-center-access", "sdk-core", "migrate"} {
		info, statErr := os.Stat(filepath.Join(source, "bin", name))
		if statErr != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("required binary %s is missing or invalid", name)
		}
	}

	dst, err := os.CreateTemp(filepath.Dir(output), ".middle-platform-*.tar.gz.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(dst.Name())

	gz := gzip.NewWriter(dst)
	tw := tar.NewWriter(gz)
	writeErr := writeContents(tw, source)
	closeErr := errors.Join(tw.Close(), gz.Close(), dst.Close())
	if err = errors.Join(writeErr, closeErr); err != nil {
		return err
	}

	// Windows 不允许 Rename 覆盖已存在的目标文件；新包完整写入后才替换。
	if err := os.Remove(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(dst.Name(), output)
}

func writeContents(writer *tar.Writer, source string) error {
	for _, root := range []string{"bin", "admin"} {
		dir := filepath.Join(source, root)
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) && root == "admin" {
			continue
		} else if err != nil {
			return err
		}
		if err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			name := filepath.ToSlash(relative)
			header := &tar.Header{
				Name:    name,
				Mode:    0o644,
				ModTime: time.Unix(0, 0).UTC(),
			}
			if entry.IsDir() {
				header.Name += "/"
				header.Typeflag = tar.TypeDir
				header.Mode = 0o755
				return writer.WriteHeader(header)
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("unsupported non-regular file: %s", path)
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			header.Typeflag = tar.TypeReg
			header.Size = info.Size()
			if strings.HasPrefix(name, "bin/") {
				header.Mode = 0o755
			}
			if err := writer.WriteHeader(header); err != nil {
				return err
			}
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(writer, file)
			return errors.Join(copyErr, file.Close())
		}); err != nil {
			return err
		}
	}
	return nil
}
