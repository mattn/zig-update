package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	bufra "github.com/avvmoto/buf-readerat"
	"github.com/ulikunitz/xz"
	"golang.org/x/mod/semver"
)

const name = "zig-update"

const version = "0.0.1"

var revision = "HEAD"

func copyFileTgz(base string, from *tar.Reader, header *tar.Header) error {
	tok := []string{base}
	tok = append(tok, strings.Split(header.Name, "/")[1:]...)
	fullpath := filepath.Join(tok...)

	fmt.Println(fullpath)

	if header.FileInfo().Mode().IsDir() {
		return os.MkdirAll(fullpath, 0755)
	}

	newf, err := os.Create(fullpath)
	if err != nil {
		return err
	}
	_, err = io.Copy(newf, from)
	if err != nil {
		return err
	}
	newf.Close()
	return os.Chmod(newf.Name(), fs.FileMode(header.Mode))
}

func extractTgz(base string, resp *http.Response) error {
	r1, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	r := tar.NewReader(r1)
	for {
		cur, err := r.Next()
		if err != nil {
			return err
		}

		err = copyFileTgz(base, r, cur)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func copyFileZip(base string, from *zip.File) error {
	tok := []string{base}
	tok = append(tok, strings.Split(from.Name, "/")[1:]...)
	fullpath := filepath.Join(tok...)

	fmt.Println(fullpath)

	if from.Mode().IsDir() {
		return os.MkdirAll(fullpath, 0755)
	}

	f, err := from.Open()
	if err != nil {
		return err
	}

	newf, err := os.Create(fullpath)
	if err != nil {
		return err
	}
	_, err = io.Copy(newf, f)
	if err != nil {
		return err
	}
	newf.Close()
	f.Close()
	return os.Chmod(newf.Name(), from.Mode())
}

func extractZip(base string, resp *http.Response) error {
	var buf bytes.Buffer
	_, err := io.Copy(&buf, resp.Body)
	if err != nil {
		return err
	}
	if resp.ContentLength != int64(buf.Len()) {
		return errors.New("size not matched")
	}

	bufr := bufra.NewBufReaderAt(bytes.NewReader(buf.Bytes()), buf.Len())
	r, err := zip.NewReader(bufr, int64(buf.Len()))
	if err != nil {
		return err
	}
	for _, file := range r.File {
		err = copyFileZip(base, file)
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTXZ(base string, resp *http.Response) error {
	r1, err := xz.NewReader(resp.Body)
	if err != nil {
		return err
	}
	r := tar.NewReader(r1)
	for {
		cur, err := r.Next()
		if err != nil {
			return err
		}

		err = copyFileTgz(base, r, cur)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil

}

func contains(a []string, i string) bool {
	for _, v := range a {
		if v == i {
			return true
		}
	}
	return false
}

func main() {
	var showVersion bool
	var downloadVersion string

	flag.BoolVar(&showVersion, "v", false, "Print the version")
	flag.StringVar(&downloadVersion, "d", "master", "Zig version to download")
	flag.Parse()

	if showVersion {
		fmt.Printf("%s %s (rev: %s/%s)\n", name, version, revision, runtime.Version())
		return
	}

	resp, err := http.Get("https://ziglang.org/download/index.json")
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	var m map[string]any
	err = json.NewDecoder(resp.Body).Decode(&m)
	if err != nil {
		log.Fatal(err)
	}

	versions := []string{}
	for k := range m {
		versions = append(versions, "v"+k)
	}
	semver.Sort(versions)
	for i := 0; i < len(m); i++ {
		versions[i] = versions[i][1:]
	}

	mm, ok := m[downloadVersion]
	if !ok {
		log.Fatal("Zig version not exists")
	}
	archs := []string{}
	for k := range mm.(map[string]any) {
		if contains([]string{"version", "date", "docs", "stdDocs", "src"}, k) {
			continue
		}
		archs = append(archs, k)
	}
	sort.Strings(archs)

	if flag.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [type] [path]\n", os.Args[0])
		fmt.Fprintln(os.Stderr, "Arch:")
		for _, v := range archs {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "Version:")
		for _, v := range versions {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		os.Exit(1)
	}

	typ := flag.Arg(0)
	base := flag.Arg(1)

	if _, ok := m["master"].(map[string]any)[typ]; !ok {
		log.Fatal("unsupported type: ", typ)
	}

	uri := m["master"].(map[string]any)[typ].(map[string]any)["tarball"].(string)
	resp, err = http.Get(uri)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	err = os.RemoveAll(base)
	if err != nil {
		log.Fatal(err)
	}

	if strings.HasSuffix(uri, ".tar.gz") {
		err = extractTgz(base, resp)
	} else if strings.HasSuffix(uri, ".zip") {
		err = extractZip(base, resp)
	} else if strings.HasSuffix(uri, ".xz") {
		err = extractTXZ(base, resp)
	} else {
		err = fmt.Errorf("unsupported archive: %v", uri)
	}
	if err != nil && err != io.EOF {
		log.Fatal(err)
	}
}
