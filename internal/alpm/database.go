package alpm

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

func ReadDatabase(path string) ([]Package, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Package{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open repository database: %w", err)
	}
	defer file.Close()

	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, fmt.Errorf("open repository database: %w", err)
	}
	defer reader.Close()

	var packages []Package
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read repository database: %w", err)
		}
		if strings.HasSuffix(header.Name, "/desc") {
			pkg, err := parseDescription(tarReader)
			if err != nil {
				return nil, fmt.Errorf("parse %s: %w", header.Name, err)
			}
			packages = append(packages, pkg)
		}
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].Name < packages[j].Name })
	return packages, nil
}

func parseDescription(r io.Reader) (Package, error) {
	values := make(map[string][]string)
	var section string
	dependencyBytes := 0
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "%") && strings.HasSuffix(line, "%") {
			section = strings.Trim(line, "%")
			continue
		}
		if line != "" && section != "" {
			if section == "DEPENDS" {
				if len(values[section]) >= packageMaxDependencies {
					return Package{}, fmt.Errorf("description exceeds %d dependency limit", packageMaxDependencies)
				}
				dependencyBytes += len(line)
				if dependencyBytes > packageDependenciesMaxBytes {
					return Package{}, fmt.Errorf("description dependencies exceed %d byte limit", packageDependenciesMaxBytes)
				}
			}
			values[section] = append(values[section], line)
		}
	}
	if err := scanner.Err(); err != nil {
		return Package{}, err
	}
	pkg := Package{
		Name:         first(values["NAME"]),
		Version:      first(values["VERSION"]),
		Architecture: first(values["ARCH"]),
		Description:  first(values["DESC"]),
		Filename:     first(values["FILENAME"]),
		Depends:      values["DEPENDS"],
	}
	if size := first(values["CSIZE"]); size != "" {
		pkg.Size, _ = strconv.ParseInt(size, 10, 64)
	}
	if pkg.Name == "" || pkg.Version == "" {
		return Package{}, fmt.Errorf("description requires NAME and VERSION")
	}
	return pkg, nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
