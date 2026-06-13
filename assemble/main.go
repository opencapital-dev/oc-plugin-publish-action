// Command assemble packages a Grafana plugin into an OpenCapital OCI artifact
// and pushes it to ghcr.io/<owner>/plugins-staging/<id>:<version>.
//
// Usage:
//
//	assemble -dir <plugin_dir> -id <id> -owner <owner> -version <version> [-platforms a,b,c]
//
// Auth: GHCR_TOKEN or GITHUB_TOKEN env var (GitHub PAT or Actions token).
// Prints only the pushed manifest digest (sha256:...) to stdout.
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// platformBinSuffix maps an artifact platform tag to the backend binary
// filename suffix the Grafana plugin SDK produces.
var platformBinSuffix = map[string]string{
	"linux-amd64":   "_linux_amd64",
	"linux-arm64":   "_linux_arm64",
	"darwin-amd64":  "_darwin_amd64",
	"darwin-arm64":  "_darwin_arm64",
	"windows-amd64": "_windows_amd64.exe",
}

func main() {
	dir, id, owner, version, platforms, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "assemble:", err)
		os.Exit(1)
	}

	token := os.Getenv("GHCR_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		fmt.Fprintln(os.Stderr, "assemble: GHCR_TOKEN or GITHUB_TOKEN must be set")
		os.Exit(1)
	}

	digest, err := run(context.Background(), dir, id, owner, version, token, platforms)
	if err != nil {
		fmt.Fprintln(os.Stderr, "assemble:", err)
		os.Exit(1)
	}
	fmt.Println(digest)
}

func parseFlags(args []string) (dir, id, owner, version string, platforms []string, err error) {
	platformsRaw := "linux-amd64,linux-arm64,darwin-amd64,darwin-arm64,windows-amd64"
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			return "", "", "", "", nil, fmt.Errorf("unexpected argument %q", a)
		}
		a = strings.TrimLeft(a, "-")
		name, val, hasVal := strings.Cut(a, "=")
		if !hasVal {
			if i+1 >= len(args) {
				return "", "", "", "", nil, fmt.Errorf("flag -%s needs a value", name)
			}
			i++
			val = args[i]
		}
		switch name {
		case "dir":
			dir = val
		case "id":
			id = val
		case "owner":
			owner = val
		case "version":
			version = val
		case "platforms":
			platformsRaw = val
		default:
			return "", "", "", "", nil, fmt.Errorf("unknown flag -%s", name)
		}
	}
	if dir == "" || id == "" || owner == "" || version == "" {
		return "", "", "", "", nil, fmt.Errorf("-dir, -id, -owner, -version are required")
	}
	for _, p := range strings.Split(platformsRaw, ",") {
		p = strings.TrimSpace(p)
		if _, ok := platformBinSuffix[p]; !ok {
			return "", "", "", "", nil, fmt.Errorf("unsupported platform %q", p)
		}
		platforms = append(platforms, p)
	}
	return dir, id, owner, version, platforms, nil
}

// artifact is the result of packaging one platform.
type artifact struct {
	platform string
	tarball  string
}

// packagePlugin strips dist/ to the one backend binary for the platform, tars
// it (flat layout), writes to a temp file, and returns the artifact.
func packagePlugin(pluginDir, id, version, platform string) (artifact, error) {
	suffix, ok := platformBinSuffix[platform]
	if !ok {
		return artifact{}, fmt.Errorf("unsupported platform %q", platform)
	}
	dist := filepath.Join(pluginDir, "dist")
	info, err := os.Stat(dist)
	if err != nil || !info.IsDir() {
		return artifact{}, fmt.Errorf("missing %s (run the plugin build first)", dist)
	}

	entries, err := os.ReadDir(dist)
	if err != nil {
		return artifact{}, err
	}

	var keep []string
	haveWanted := false
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "gpx_") {
			if strings.HasSuffix(name, suffix) {
				keep = append(keep, name)
				haveWanted = true
			}
			continue
		}
		keep = append(keep, name)
	}
	if !haveWanted {
		return artifact{}, fmt.Errorf("no backend binary matching gpx_*%s in %s", suffix, dist)
	}
	sort.Strings(keep)

	tarball := filepath.Join(os.TempDir(), fmt.Sprintf("%s-%s-%s.tar.gz", id, version, platform))
	if _, _, err := writeTarGz(tarball, dist, keep); err != nil {
		return artifact{}, err
	}
	return artifact{platform: platform, tarball: tarball}, nil
}

func writeTarGz(dest, srcDir string, names []string) (string, int64, error) {
	f, err := os.Create(dest)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, name := range names {
		if err := addPath(tw, filepath.Join(srcDir, name), name); err != nil {
			tw.Close()
			gz.Close()
			return "", 0, err
		}
	}
	if err := tw.Close(); err != nil {
		gz.Close()
		return "", 0, err
	}
	if err := gz.Close(); err != nil {
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, err
	}
	return hashFile(dest)
}

func addPath(tw *tar.Writer, fsPath, archiveName string) error {
	info, err := os.Lstat(fsPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil
	}
	if info.IsDir() {
		entries, err := os.ReadDir(fsPath)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			if err := addPath(tw, filepath.Join(fsPath, e.Name()), archiveName+"/"+e.Name()); err != nil {
				return err
			}
		}
		return nil
	}
	hdr := &tar.Header{
		Name:    archiveName,
		Mode:    int64(info.Mode().Perm()),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	src, err := os.Open(fsPath)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(tw, src)
	return err
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}
