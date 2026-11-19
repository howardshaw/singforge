package host

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/howardshaw/singforge/internal/store"
)

const Bin = "/usr/local/lib/singforge"
const CoreVersion = "1.14.4"

var hashes = map[string]map[string]string{"sing-box": {"amd64": "699a1b510aa4396b2176d32d1181920399ebf1e267835da6f3b598d0f7c24968", "arm64": "436441f76ee2f7fe21b5b2d426d3cb261a480455a704865789ad533e4273ec26"}, "cloudflared": {"amd64": "97bb3b02d23a5980fc9c15a1a270e692da1923f103f920d5b16d4931d5ab55e7", "arm64": "b155cec3e48e489cfb8e5c602769fb8c771083018d8881d68b4d18a791567c5a"}}

var rawCores = map[string]string{"amd64": "1264361b5f8868c8768f7555d3e77e20118bf503c41ac08bb290a0d99056aa3a", "arm64": "d953af885f11d6b547c21a3c0ffe70796963afc802fab959d548311002b4908a"}

const maxArtifactSize = 200 << 20

func copyArtifact(dst io.Writer, src io.Reader) (string, error) {
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(dst, h), io.LimitReader(src, maxArtifactSize+1))
	if err != nil {
		return "", err
	}
	if n > maxArtifactSize {
		return "", fmt.Errorf("artifact exceeds %d MiB", maxArtifactSize>>20)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyVerified(dst io.Writer, src io.Reader, want string) error {
	if !digest.MatchString(want) {
		return fmt.Errorf("provide a trusted lowercase SHA-256 digest")
	}
	got, err := copyArtifact(dst, src)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("artifact SHA-256 mismatch")
	}
	return nil
}

func verifyFile(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return copyVerified(io.Discard, f, want)
}

func writeVerified(path string, src io.Reader, want string) error {
	return store.WriteAtomic(path, 0755, func(dst io.Writer) error {
		return copyVerified(dst, src, want)
	})
}

func (m Manager) artifact(name string) (string, error) {
	arch := runtime.GOARCH
	want := hashes[name][arch]
	if want == "" {
		return "", fmt.Errorf("unsupported architecture or component")
	}
	target := filepath.Join(Bin, name+"-"+want[:16])
	raw := want
	if name == "sing-box" {
		raw = rawCores[arch]
	}
	if _, e := os.Stat(target); e == nil {
		m.report("Verifying cached " + name)
		return target, verifyFile(target, raw)
	} else if !errors.Is(e, os.ErrNotExist) {
		return "", e
	}
	archive := "sing-box-" + CoreVersion + "-linux-" + arch
	u := "https://github.com/SagerNet/sing-box/releases/download/v" + CoreVersion + "/" + archive + ".tar.gz"
	if name == "cloudflared" {
		u = "https://github.com/cloudflare/cloudflared/releases/download/2026.11.1/cloudflared-linux-" + arch
	}
	m.report("Downloading and verifying " + name)
	client := http.Client{Timeout: 3 * time.Minute}
	resp, e := client.Get(u)
	if e != nil {
		return "", e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download %s returned %d", name, resp.StatusCode)
	}
	f, e := os.CreateTemp(Bin, ".download-")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = copyVerified(f, resp.Body, want); e != nil {
		return "", fmt.Errorf("download %s: %w", name, e)
	}
	m.report("Installing verified " + name + " executable")
	if _, e = f.Seek(0, 0); e != nil {
		return "", e
	}
	var r io.Reader = f
	if name == "sing-box" {
		gz, e := gzip.NewReader(f)
		if e != nil {
			return "", e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			hdr, e := tr.Next()
			if e != nil {
				return "", e
			}
			if hdr.Name == archive+"/sing-box" && hdr.Typeflag == tar.TypeReg {
				r = tr
				break
			}
		}
	}
	return target, writeVerified(target, r, raw)
}
