package util

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/containers/storage/pkg/fileutils"
	"github.com/containers/storage/pkg/ioutils"
	"github.com/hashicorp/go-multierror"
	gzip "github.com/klauspost/pgzip"
	"github.com/sirupsen/logrus"
)

type devino struct {
	Dev uint64
	Ino uint64
}

func CreateTar(excludes []string, sources ...string) (io.ReadCloser, error) {
	pm, err := fileutils.NewPatternMatcher(excludes)
	if err != nil {
		return nil, fmt.Errorf("processing excludes list %v: %w", excludes, err)
	}

	if len(sources) == 0 {
		return nil, errors.New("no source(s) provided for build")
	}

	pr, pw := io.Pipe()
	gw := gzip.NewWriter(pw)
	tw := tar.NewWriter(gw)

	var merr *multierror.Error
	go func() {
		defer pw.Close()
		defer gw.Close()
		defer tw.Close()
		seen := make(map[devino]string)
		for i, src := range sources {
			source, err := filepath.Abs(src)
			if err != nil {
				logrus.Errorf("Cannot stat one of source context: %v", err)
				merr = multierror.Append(merr, err)
				return
			}
			err = filepath.WalkDir(source, func(path string, dentry fs.DirEntry, err error) error {
				if err != nil {
					return err
				}

				separator := string(filepath.Separator)
				// check if what we are given is an empty dir, if so then continue w/ it. Else return.
				// if we are given a file or a symlink, we do not want to exclude it.
				if source == path {
					separator = ""
					if dentry.IsDir() {
						var p *os.File
						p, err = os.Open(path)
						if err != nil {
							return err
						}
						defer p.Close()
						_, err = p.Readdir(1)
						if err == nil {
							return nil // non empty root dir, need to return
						}
						if err != io.EOF {
							logrus.Errorf("While reading directory %v: %v", path, err)
						}
					}
				}
				var name string
				if i == 0 {
					name = filepath.ToSlash(strings.TrimPrefix(path, source+separator))
				} else {
					if !dentry.Type().IsRegular() {
						return fmt.Errorf("path %s must be a regular file", path)
					}
					name = filepath.ToSlash(path)
				}
				// If name is absolute path, then it has to be containerfile outside of build context.
				// If not, we should check it for being excluded via pattern matcher.
				if !filepath.IsAbs(name) {
					excluded, err := pm.Matches(name) //nolint:staticcheck
					if err != nil {
						return fmt.Errorf("checking if %q is excluded: %w", name, err)
					}
					if excluded {
						// Note: filepath.SkipDir is not possible to use given .dockerignore semantics.
						// An exception to exclusions may include an excluded directory, therefore we
						// are required to visit all files. :(
						return nil
					}
				}
				switch {
				case dentry.Type().IsRegular(): // add file item
					info, err := dentry.Info()
					if err != nil {
						return err
					}
					di, isHardLink := CheckHardLink(info)
					if err != nil {
						return err
					}

					hdr, err := tar.FileInfoHeader(info, "")
					if err != nil {
						return err
					}
					hdr.Uid, hdr.Gid = 0, 0
					orig, ok := seen[di]
					if ok {
						hdr.Typeflag = tar.TypeLink
						hdr.Linkname = orig
						hdr.Size = 0
						hdr.Name = name
						return tw.WriteHeader(hdr)
					}
					f, err := os.Open(path)
					if err != nil {
						return err
					}

					hdr.Name = name
					if err := tw.WriteHeader(hdr); err != nil {
						f.Close()
						return err
					}

					_, err = io.Copy(tw, f)
					f.Close()
					if err == nil && isHardLink {
						seen[di] = name
					}
					return err
				case dentry.IsDir(): // add folders
					info, err := dentry.Info()
					if err != nil {
						return err
					}
					hdr, lerr := tar.FileInfoHeader(info, name)
					if lerr != nil {
						return lerr
					}
					hdr.Name = name
					hdr.Uid, hdr.Gid = 0, 0
					if lerr := tw.WriteHeader(hdr); lerr != nil {
						return lerr
					}
				case dentry.Type()&os.ModeSymlink != 0: // add symlinks as it, not content
					link, err := os.Readlink(path)
					if err != nil {
						return err
					}
					info, err := dentry.Info()
					if err != nil {
						return err
					}
					hdr, lerr := tar.FileInfoHeader(info, link)
					if lerr != nil {
						return lerr
					}
					hdr.Name = name
					hdr.Uid, hdr.Gid = 0, 0
					if lerr := tw.WriteHeader(hdr); lerr != nil {
						return lerr
					}
				} // skip other than file,folder and symlinks
				return nil
			})
			merr = multierror.Append(merr, err)
		}
	}()
	rc := ioutils.NewReadCloserWrapper(pr, func() error {
		if merr != nil {
			merr = multierror.Append(merr, pr.Close())
			return merr.ErrorOrNil()
		}
		return pr.Close()
	})
	return rc, nil
}
