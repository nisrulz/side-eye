package scan

import (
	"io"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
)

// rawSource reads files through the host raw file endpoint. It is best effort:
// it probes known risky paths because other hosts have no common file listing.
type rawSource struct {
	http  *http.Client
	refs  []string
	bases []string
	cache map[string][]byte
}

func newRawSource(t *remoteTarget) (*rawSource, error) {
	s := &rawSource{
		http:  httpClient(),
		cache: map[string][]byte{},
		bases: rawBases(t.host, t.owner, t.repo),
	}
	if t.ref != "" {
		s.refs = []string{t.ref}
	} else {
		s.refs = []string{"HEAD", "main", "master"}
	}
	return s, nil
}

func (s *rawSource) file(p string) []byte {
	if v, ok := s.cache[p]; ok {
		return v
	}
	var body []byte
	for _, base := range s.bases {
		for _, ref := range s.refs {
			u := base + "/" + escapeFirst(ref) + "/" + escapePath(p)
			if got := s.get(u); got != nil {
				body = got
				break
			}
		}
		if body != nil {
			break
		}
	}
	s.cache[p] = body
	return body
}

// hookFiles probes the hook directories and hook names worth asking for,
// because other hosts have no file listing. The hook directories come from
// hookDirs so both sources test the same set.
func (s *rawSource) hookFiles() []string {
	var hooks []string
	for _, dir := range slices.Sorted(maps.Keys(hookDirs)) {
		for _, name := range rawHookNames {
			p := dir + "/" + name
			if s.file(p) != nil {
				hooks = append(hooks, p)
			}
		}
	}
	return hooks
}

// list returns the probed paths that returned content. Other hosts have no file
// listing, so this is best effort.
func (s *rawSource) list() []string {
	var paths []string
	for p, data := range s.cache {
		if data != nil {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths
}

func (s *rawSource) get(u string) []byte {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "side-eye")
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

// escapeFirst keeps a branch or tag that contains a slash in one path segment,
// because the raw host URLs put the ref in one segment.
func escapeFirst(s string) string {
	if strings.Contains(s, "/") {
		return strings.ReplaceAll(s, "/", "%2F")
	}
	return s
}

func rawBases(host, owner, repo string) []string {
	prefix := "https://" + host + "/" + owner + "/" + repo
	switch {
	case strings.Contains(host, "gitlab"):
		return []string{prefix + "/-/raw", prefix + "/raw"}
	case strings.Contains(host, "bitbucket"):
		return []string{prefix + "/raw"}
	default:
		return []string{prefix + "/raw", prefix + "/-/raw"}
	}
}

// rawHookNames is the subset of hookTriggers worth probing on a host without a
// file listing: the hooks projects commit to a tracked hooks directory.
var rawHookNames = []string{
	"pre-commit", "pre-push", "commit-msg", "prepare-commit-msg",
	"post-checkout", "post-merge", "post-rewrite", "pre-rebase",
}
