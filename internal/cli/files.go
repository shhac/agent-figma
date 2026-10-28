package cli

import (
	"io"
	"os"
	"path/filepath"
)

func absPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	outFile, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outFile, in); err != nil {
		_ = outFile.Close()
		return err
	}
	return outFile.Close()
}
