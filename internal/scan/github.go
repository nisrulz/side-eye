package scan

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
)

// githubSource reads tracked files and hook paths through the GitHub API.
type githubSource struct {
	target *remoteTarget
	http   *http.Client
	ref    string
	paths  []string
	cache  map[string][]byte
}

func newGitHubSource(t *remoteTarget) (*githubSource, error) {
	s := &githubSource{target: t, http: httpClient(), cache: map[string][]byte{}}
	s.ref = t.ref
	if s.ref == "" {
		ref, err := s.getDefaultBranch()
		if err != nil {
			return nil, err
		}
		s.ref = ref
	}
	paths, err := s.listTree()
	if err != nil {
		return nil, err
	}
	s.paths = paths
	t.ref = s.ref
	return s, nil
}

func (s *githubSource) file(p string) []byte {
	if v, ok := s.cache[p]; ok {
		return v
	}
	var body []byte
	if s.target.token != "" {
		body = s.getContents(p)
	} else {
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
			s.target.owner, s.target.repo, url.PathEscape(s.ref), escapePath(p))
		body = s.get(rawURL, "")
	}
	s.cache[p] = body
	return body
}

func (s *githubSource) hookFiles() []string {
	var hooks []string
	for _, p := range s.paths {
		if strings.Contains(p, "/.git/") {
			continue
		}
		if _, ok := hookTriggers[strings.ToLower(path.Base(p))]; ok && hookDirs[path.Dir(p)] {
			hooks = append(hooks, p)
		}
	}
	return hooks
}

// list returns every tracked file path from the git tree.
func (s *githubSource) list() []string { return s.paths }

func (s *githubSource) apiURL(format string, args ...any) string {
	return fmt.Sprintf("https://api.github.com/repos/%s/%s%s", s.target.owner, s.target.repo, fmt.Sprintf(format, args...))
}

func (s *githubSource) getDefaultBranch() (string, error) {
	var doc struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := s.getJSON(s.apiURL(""), &doc); err != nil {
		return "", err
	}
	if doc.DefaultBranch == "" {
		return "", fmt.Errorf("github returned no default branch for %s/%s", s.target.owner, s.target.repo)
	}
	return doc.DefaultBranch, nil
}

func (s *githubSource) listTree() ([]string, error) {
	var doc struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := s.getJSON(s.apiURL("/git/trees/%s?recursive=1", url.PathEscape(s.ref)), &doc); err != nil {
		return nil, err
	}
	var paths []string
	for _, item := range doc.Tree {
		if item.Type == "blob" {
			paths = append(paths, item.Path)
		}
	}
	return paths, nil
}

func (s *githubSource) getContents(p string) []byte {
	var doc struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	if err := s.getJSON(s.apiURL("/contents/%s?ref=%s", escapePath(p), url.PathEscape(s.ref)), &doc); err != nil {
		return nil
	}
	if doc.Encoding != "base64" {
		return nil
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(doc.Content, "\n", ""))
	if err != nil {
		return nil
	}
	return data
}

func (s *githubSource) getJSON(u string, out any) error {
	body := s.get(u, "application/vnd.github+json")
	if body == nil {
		return fmt.Errorf("github request failed: %s", u)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("github response was not JSON: %w", err)
	}
	return nil
}

func (s *githubSource) get(u, accept string) []byte {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "side-eye")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if s.target.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.target.token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReadBytes))
	if err != nil {
		return nil
	}
	return body
}
