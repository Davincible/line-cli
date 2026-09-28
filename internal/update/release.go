// Package update checks official releases without accessing a LINE session.
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	repositoryURL = "https://github.com/kongesque/line-cli"
	latestAPI     = "https://api.github.com/repos/kongesque/line-cli/releases/latest"
	maxMetadata   = 1 << 20
)

// Only stable, canonical tags can authorize replacement. Development versions
// are deliberately not guessed from git-describe strings or prerelease labels.
var stableVersion = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

type release struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func releaseURL(tag string) string { return repositoryURL + "/releases/tag/" + tag }
func assetURL(tag, name string) string {
	return repositoryURL + "/releases/download/" + tag + "/" + name
}

func (r release) hasAsset(name string) bool {
	count := 0
	for _, a := range r.Assets {
		if a.Name == name {
			if a.URL != assetURL(r.Tag, name) {
				return false
			}
			count++
		}
	}
	return count == 1
}

func versionStatus(current, latest string) string {
	c, l := stableVersion.FindStringSubmatch(current), stableVersion.FindStringSubmatch(latest)
	if c == nil || l == nil {
		return "unknown_version"
	}
	for i := 1; i <= 3; i++ {
		// Length before lexical comparison avoids overflow for large components.
		if len(c[i]) < len(l[i]) {
			return "update_available"
		}
		if len(c[i]) > len(l[i]) {
			return "ahead"
		}
		if c[i] < l[i] {
			return "update_available"
		}
		if c[i] > l[i] {
			return "ahead"
		}
	}
	return "up_to_date"
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 2 * time.Minute,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" || req.URL.User != nil || len(via) >= 5 {
				return errors.New("unsupported release redirect")
			}
			return nil
		},
	}
}

func (s *Service) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New("invalid release URL")
	}
	req.Header.Set("User-Agent", "line-cli-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := s.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("could not reach GitHub; check your connection and try again")
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusForbidden, http.StatusTooManyRequests:
			return nil, errors.New("GitHub refused the update request or its rate limit was reached; try again later")
		case http.StatusNotFound:
			return nil, errors.New("the official release or download is unavailable; try again later")
		default:
			return nil, fmt.Errorf("GitHub update request failed (HTTP %d); try again later", resp.StatusCode)
		}
	}
	return resp, nil
}

func (s *Service) latest(ctx context.Context) (release, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := s.get(ctx, latestAPI)
	if err != nil {
		return release{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadata+1))
	if err != nil || len(body) > maxMetadata {
		return release{}, errors.New("could not read GitHub release information; try again later")
	}
	var r release
	if json.Unmarshal(body, &r) != nil || r.Draft || r.Prerelease || !stableVersion.MatchString(r.Tag) {
		return release{}, errors.New("GitHub returned unsupported release information; no files were changed")
	}
	return r, nil
}

func archiveName(os, arch string) string { return "line-" + os + "-" + arch + ".tar.gz" }

func supportedPlatform(os, arch string) bool {
	return (os == "darwin" || os == "linux" || os == "windows") && (arch == "arm64" || arch == "amd64")
}

func checksumFor(data []byte, name string) (string, error) {
	var found string
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || strings.TrimPrefix(fields[1], "*") != name {
			continue
		}
		if found != "" || len(fields[0]) != 64 || strings.Trim(fields[0], "0123456789abcdefABCDEF") != "" {
			return "", errors.New("invalid release checksum; no files were changed")
		}
		found = strings.ToLower(fields[0])
	}
	if found == "" {
		return "", errors.New("release checksum is missing; no files were changed")
	}
	return found, nil
}
