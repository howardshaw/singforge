package host

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/howardshaw/singforge/internal/store"
)

type Release struct{ File, Digest, Tag string }

// FetchRelease accepts metadata only from the named upstream's stable release API.
func FetchRelease(dir, component string) (Release, error) {
	repo := map[string]string{"sing-box": "SagerNet/sing-box", "cloudflared": "cloudflare/cloudflared", "singforge": "howardshaw/singforge"}[component]
	if repo == "" {
		return Release{}, fmt.Errorf("unknown release component")
	}
	client := http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) > 5 {
			return fmt.Errorf("unsafe redirect")
		}
		return nil
	}}
	resp, e := client.Get("https://api.github.com/repos/" + repo + "/releases/latest")
	if e != nil {
		return Release{}, e
	}
	defer resp.Body.Close()
	var meta struct {
		Tag               string `json:"tag_name"`
		Prerelease, Draft bool
		Assets            []struct {
			Name, Digest, URL string
			Download          string `json:"browser_download_url"`
		}
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&meta) != nil || meta.Prerelease || meta.Draft {
		return Release{}, fmt.Errorf("stable %s release metadata unavailable", component)
	}
	arch := runtime.GOARCH
	name := component + "-linux-" + arch
	archive := ""
	if component == "sing-box" {
		version := strings.TrimPrefix(meta.Tag, "v")
		if !compatible.MatchString("sing-box version " + version) {
			return Release{}, fmt.Errorf("latest core %s is outside the tested 1.14.x series", meta.Tag)
		}
		archive = "sing-box-" + version + "-linux-" + arch
		name = archive + ".tar.gz"
	}
	for _, a := range meta.Assets {
		if a.Name != name {
			continue
		}
		want := strings.TrimPrefix(a.Digest, "sha256:")
		if !digest.MatchString(want) || a.Download != "https://github.com/"+repo+"/releases/download/"+meta.Tag+"/"+name {
			return Release{}, fmt.Errorf("unverified upstream asset metadata")
		}
		r, e := client.Get(a.Download)
		if e != nil {
			return Release{}, e
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return Release{}, fmt.Errorf("upstream asset HTTP %d", r.StatusCode)
		}
		tmp, e := os.CreateTemp(dir, ".package-")
		if e != nil {
			return Release{}, e
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		h := sha256.New()
		if _, e = io.Copy(io.MultiWriter(tmp, h), io.LimitReader(r.Body, 200<<20)); e != nil {
			return Release{}, e
		}
		if hex.EncodeToString(h.Sum(nil)) != want {
			return Release{}, fmt.Errorf("upstream asset digest mismatch")
		}
		if _, e = tmp.Seek(0, 0); e != nil {
			return Release{}, e
		}
		var src io.Reader = tmp
		if archive != "" {
			gz, e := gzip.NewReader(tmp)
			if e != nil {
				return Release{}, e
			}
			defer gz.Close()
			tr := tar.NewReader(gz)
			for {
				entry, e := tr.Next()
				if e != nil {
					return Release{}, e
				}
				if entry.Name == archive+"/sing-box" && entry.Typeflag == tar.TypeReg {
					src = tr
					break
				}
			}
		}
		b, e := io.ReadAll(io.LimitReader(src, 201<<20))
		if e != nil || len(b) > 200<<20 {
			return Release{}, fmt.Errorf("invalid release executable")
		}
		raw := sha256.Sum256(b)
		p := filepath.Join(dir, component)
		if e = store.Write(p, b, 0700); e != nil {
			return Release{}, e
		}
		return Release{p, hex.EncodeToString(raw[:]), meta.Tag}, nil
	}
	return Release{}, fmt.Errorf("no verified %s Linux %s artifact", component, arch)
}
