//go:build !windows
// +build !windows

package util

import (
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"syscall"

	"github.com/spf13/afero"
)

// filestat return a FileInfo describing the named file.
func FileStat(fs afero.Fs, name string) (fi FileInfo, err error) {
	if IsFileExist(fs, name) {
		f, err := fs.Open(name)
		if err != nil {
			return fi, err
		}
		defer f.Close()
		stats, _ := f.Stat()
		fi.Uid = stats.Sys().(*syscall.Stat_t).Uid
		fi.Gid = stats.Sys().(*syscall.Stat_t).Gid
		fi.Mode = stats.Mode()
		h := md5.New()
		io.Copy(h, f)
		fi.Md5 = fmt.Sprintf("%x", h.Sum(nil))
		return fi, nil
	}
	return fi, errors.New("File not found")
}
