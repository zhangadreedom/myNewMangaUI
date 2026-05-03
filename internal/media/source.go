package media

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nwaples/rardecode/v2"
)

const (
	refKindFile = "file"
	refKindZip  = "zip"
	refKindRAR  = "rar"
)

var imageExts = map[string]struct{}{
	".avif": {},
	".gif":  {},
	".jpeg": {},
	".jpg":  {},
	".png":  {},
	".webp": {},
}

var archiveExts = map[string]string{
	".cbz": refKindZip,
	".cbr": refKindRAR,
	".rar": refKindRAR,
	".zip": refKindZip,
}

type Ref struct {
	Kind      string
	Path      string
	EntryPath string
}

type ArchiveEntry struct {
	Name         string
	Size         int64
	ModifiedTime time.Time
}

func IsImageFile(name string) bool {
	_, ok := imageExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

func IsArchiveFile(name string) bool {
	_, ok := archiveExts[strings.ToLower(filepath.Ext(name))]
	return ok
}

func ArchiveKind(name string) string {
	return archiveExts[strings.ToLower(filepath.Ext(name))]
}

func GuessMime(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".avif":
		return "image/avif"
	case ".gif":
		return "image/gif"
	case ".jpeg", ".jpg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

func FileRef(path string) string {
	return refKindFile + "|" + filepath.Clean(path)
}

func ArchiveRef(kind string, archivePath string, entryPath string) string {
	return kind + "|" + filepath.Clean(archivePath) + "|" + filepath.ToSlash(entryPath)
}

func ParseRef(raw string) (Ref, error) {
	parts := strings.SplitN(raw, "|", 3)
	switch len(parts) {
	case 2:
		if parts[0] != refKindFile {
			return Ref{}, fmt.Errorf("unsupported asset ref kind %q", parts[0])
		}
		return Ref{Kind: parts[0], Path: parts[1]}, nil
	case 3:
		if parts[0] != refKindZip && parts[0] != refKindRAR {
			return Ref{}, fmt.Errorf("unsupported asset ref kind %q", parts[0])
		}
		return Ref{Kind: parts[0], Path: parts[1], EntryPath: filepath.ToSlash(parts[2])}, nil
	default:
		return Ref{}, fmt.Errorf("invalid asset ref %q", raw)
	}
}

func Open(raw string) (io.ReadCloser, time.Time, error) {
	ref, err := ParseRef(raw)
	if err != nil {
		return nil, time.Time{}, err
	}

	switch ref.Kind {
	case refKindFile:
		file, err := os.Open(ref.Path)
		if err != nil {
			return nil, time.Time{}, err
		}
		info, err := file.Stat()
		if err != nil {
			file.Close()
			return nil, time.Time{}, err
		}
		return file, info.ModTime(), nil
	case refKindZip:
		return openZIPEntry(ref.Path, ref.EntryPath)
	case refKindRAR:
		return openRAREntry(ref.Path, ref.EntryPath)
	default:
		return nil, time.Time{}, fmt.Errorf("unsupported asset ref kind %q", ref.Kind)
	}
}

func ListArchiveImages(path string) ([]ArchiveEntry, error) {
	switch ArchiveKind(path) {
	case refKindZip:
		return listZIPImages(path)
	case refKindRAR:
		return listRARImages(path)
	default:
		return nil, fmt.Errorf("unsupported archive type for %q", path)
	}
}

type multiCloser struct {
	reader io.Reader
	close  func() error
}

func (m *multiCloser) Read(p []byte) (int, error) {
	return m.reader.Read(p)
}

func (m *multiCloser) Close() error {
	return m.close()
}

func listZIPImages(path string) ([]ArchiveEntry, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	items := make([]ArchiveEntry, 0)
	for _, file := range reader.File {
		if file.FileInfo().IsDir() || !IsImageFile(file.Name) {
			continue
		}
		items = append(items, ArchiveEntry{
			Name:         filepath.ToSlash(file.Name),
			Size:         int64(file.UncompressedSize64),
			ModifiedTime: file.Modified,
		})
	}
	return items, nil
}

func listRARImages(path string) ([]ArchiveEntry, error) {
	reader, err := rardecode.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	items := make([]ArchiveEntry, 0)
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return items, nil
		}
		if err != nil {
			return nil, err
		}
		if header.IsDir || !IsImageFile(header.Name) {
			continue
		}
		items = append(items, ArchiveEntry{
			Name:         filepath.ToSlash(header.Name),
			Size:         header.UnPackedSize,
			ModifiedTime: header.ModificationTime,
		})
	}
}

func openZIPEntry(path string, entryPath string) (io.ReadCloser, time.Time, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	for _, file := range reader.File {
		if filepath.ToSlash(file.Name) != entryPath {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			reader.Close()
			return nil, time.Time{}, err
		}
		return &multiCloser{
			reader: rc,
			close: func() error {
				rc.Close()
				return reader.Close()
			},
		}, file.Modified, nil
	}
	reader.Close()
	return nil, time.Time{}, fmt.Errorf("zip entry not found: %s", entryPath)
}

func openRAREntry(path string, entryPath string) (io.ReadCloser, time.Time, error) {
	reader, err := rardecode.OpenReader(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			reader.Close()
			return nil, time.Time{}, fmt.Errorf("rar entry not found: %s", entryPath)
		}
		if err != nil {
			reader.Close()
			return nil, time.Time{}, err
		}
		if header.IsDir || filepath.ToSlash(header.Name) != entryPath {
			continue
		}
		return &multiCloser{
			reader: reader,
			close:  reader.Close,
		}, header.ModificationTime, nil
	}
}
