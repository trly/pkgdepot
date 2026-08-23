package alpm

import (
	"archive/tar"
	"bufio"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
	"github.com/ulikunitz/xz"
)

type Package struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Architecture string   `json:"architecture"`
	Description  string   `json:"description,omitempty"`
	Filename     string   `json:"filename,omitempty"`
	Size         int64    `json:"size,omitempty"`
	Depends      []string `json:"depends,omitempty"`
}

const (
	packageDecoderMaxMemory     = 64 << 20
	packageDecoderMaxWindow     = 64 << 20
	packageDecoderMaxOutput     = 64 << 20
	packageMetadataMaxBytes     = 256 << 10
	packageMaxDependencies      = 256
	packageDependenciesMaxBytes = 64 << 10
)

func ParsePackageInfo(r io.Reader) (Package, error) {
	var pkg Package
	scanner := bufio.NewScanner(r)
	metadataBytes, dependencyBytes := 0, 0
	for scanner.Scan() {
		metadataBytes += len(scanner.Bytes()) + 1
		if metadataBytes > packageMetadataMaxBytes {
			return Package{}, fmt.Errorf(".PKGINFO metadata exceeds %d byte limit", packageMetadataMaxBytes)
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			return Package{}, fmt.Errorf("invalid .PKGINFO line %q", line)
		}
		switch key {
		case "pkgname":
			pkg.Name = value
		case "pkgver":
			pkg.Version = value
		case "arch":
			pkg.Architecture = value
		case "pkgdesc":
			pkg.Description = value
		case "depend":
			if len(pkg.Depends) >= packageMaxDependencies {
				return Package{}, fmt.Errorf(".PKGINFO exceeds %d dependency limit", packageMaxDependencies)
			}
			dependencyBytes += len(value)
			if dependencyBytes > packageDependenciesMaxBytes {
				return Package{}, fmt.Errorf(".PKGINFO dependencies exceed %d byte limit", packageDependenciesMaxBytes)
			}
			pkg.Depends = append(pkg.Depends, value)
		}
	}
	if err := scanner.Err(); err != nil {
		return Package{}, fmt.Errorf("read .PKGINFO: %w", err)
	}
	if pkg.Name == "" || pkg.Version == "" || pkg.Architecture == "" {
		return Package{}, fmt.Errorf(".PKGINFO requires pkgname, pkgver, and arch")
	}
	return pkg, nil
}

func InspectPackage(path string) (Package, error) {
	return InspectPackageContext(context.Background(), path)
}

func InspectPackageContext(ctx context.Context, path string) (Package, error) {
	file, err := os.Open(path)
	if err != nil {
		return Package{}, fmt.Errorf("open package: %w", err)
	}
	defer file.Close()

	reader, closeReader, err := compressedReader(ctx, file)
	if err != nil {
		return Package{}, err
	}
	if closeReader != nil {
		defer closeReader()
	}

	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return Package{}, fmt.Errorf("package does not contain .PKGINFO")
		}
		if err != nil {
			return Package{}, fmt.Errorf("read package archive: %w", err)
		}
		if strings.TrimPrefix(header.Name, "./") != ".PKGINFO" {
			continue
		}
		pkg, err := ParsePackageInfo(tarReader)
		if err != nil {
			return Package{}, err
		}
		info, err := file.Stat()
		if err != nil {
			return Package{}, fmt.Errorf("stat package: %w", err)
		}
		pkg.Filename = info.Name()
		pkg.Size = info.Size()
		return pkg, nil
	}
}

type limitedReader struct {
	r     io.Reader
	left  int64
	error error
	ctx   context.Context
}

func (lr *limitedReader) Read(p []byte) (int, error) {
	if err := lr.ctx.Err(); err != nil {
		return 0, err
	}
	if lr.left <= 0 {
		return 0, lr.error
	}
	if int64(len(p)) > lr.left {
		p = p[:lr.left]
	}
	n, err := lr.r.Read(p)
	lr.left -= int64(n)
	if contextErr := lr.ctx.Err(); contextErr != nil {
		return n, contextErr
	}
	return n, err
}

func compressedReader(ctx context.Context, r *os.File) (io.Reader, func(), error) {
	header := make([]byte, 6)
	n, err := io.ReadFull(r, header)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, nil, fmt.Errorf("read package header: %w", err)
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, nil, fmt.Errorf("rewind package: %w", err)
	}
	header = header[:n]

	switch {
	case len(header) >= 2 && header[0] == 0x1f && header[1] == 0x8b:
		reader, err := gzip.NewReader(r)
		if err != nil {
			return nil, nil, fmt.Errorf("open gzip package: %w", err)
		}
		lr := &limitedReader{r: reader, left: packageDecoderMaxOutput, ctx: ctx, error: fmt.Errorf("gzip decompression exceeded %d byte limit", packageDecoderMaxOutput)}
		return lr, func() { _ = reader.Close() }, nil
	case len(header) >= 3 && string(header[:3]) == "BZh":
		lr := &limitedReader{r: bzip2.NewReader(r), left: packageDecoderMaxOutput, ctx: ctx, error: fmt.Errorf("bzip2 decompression exceeded %d byte limit", packageDecoderMaxOutput)}
		return lr, nil, nil
	case len(header) >= 6 && string(header[:6]) == "\xfd7zXZ\x00":
		reader, err := xz.ReaderConfig{DictCap: packageDecoderMaxWindow, SingleStream: true}.NewReader(r)
		if err != nil {
			return nil, nil, fmt.Errorf("open xz package: %w", err)
		}
		lr := &limitedReader{r: reader, left: packageDecoderMaxOutput, ctx: ctx, error: fmt.Errorf("xz decompression exceeded %d byte limit", packageDecoderMaxOutput)}
		return lr, nil, nil
	case len(header) >= 4 && header[0] == 0x28 && header[1] == 0xb5 && header[2] == 0x2f && header[3] == 0xfd:
		reader, err := zstd.NewReader(r,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderLowmem(true),
			zstd.WithDecoderMaxMemory(packageDecoderMaxMemory),
			zstd.WithDecoderMaxWindow(packageDecoderMaxWindow),
		)
		if err != nil {
			return nil, nil, fmt.Errorf("open zstd package: %w", err)
		}
		lr := &limitedReader{r: reader, left: packageDecoderMaxOutput, ctx: ctx, error: fmt.Errorf("zstd decompression exceeded %d byte limit", packageDecoderMaxOutput)}
		return lr, reader.Close, nil
	default:
		return &limitedReader{r: r, left: packageDecoderMaxOutput, ctx: ctx, error: fmt.Errorf("package decompression exceeded %d byte limit", packageDecoderMaxOutput)}, nil, nil
	}
}
