// Package discover finds reagent repos in the GitHub org.
package discover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
)

// ErrNotFound is returned by RawFile when the file does not exist.
var ErrNotFound = errors.New("not found")

// Repo is the subset of a GitHub repository rosarium needs.
type Repo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	DefaultBranch string   `json:"default_branch"`
	HTMLURL       string   `json:"html_url"`
	Topics        []string `json:"topics"`
	Archived      bool     `json:"archived"`
}

// Skipped is a reagent that was found but cannot be rendered.
type Skipped struct {
	Repo   Repo
	Reason string
}

// Client talks to the GitHub REST API and raw.githubusercontent.com.
// Zero values default to the public endpoints and http.DefaultClient.
type Client struct {
	HTTP    *http.Client
	APIBase string
	RawBase string
	Token   string
}

func (c Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c Client) apiBase() string {
	if c.APIBase != "" {
		return c.APIBase
	}
	return "https://api.github.com"
}

func (c Client) rawBase() string {
	if c.RawBase != "" {
		return c.RawBase
	}
	return "https://raw.githubusercontent.com"
}

var nextLink = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// OrgRepos lists all public repos of org, sorted by name.
func (c Client) OrgRepos(ctx context.Context, org string) ([]Repo, error) {
	var all []Repo
	url := fmt.Sprintf("%s/orgs/%s/repos?per_page=100&type=public", c.apiBase(), org)
	for url != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		if c.Token != "" {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
		resp, err := c.http().Do(req)
		if err != nil {
			return nil, fmt.Errorf("listing repos of %s: %w", org, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
				resp.Header.Get("X-RateLimit-Remaining") == "0" {
				return nil, fmt.Errorf("GitHub API rate limit exceeded listing repos of %s; set GITHUB_TOKEN to raise it", org)
			}
			return nil, fmt.Errorf("listing repos of %s: %s: %s", org, resp.Status, body)
		}
		var page []Repo
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("decoding repos of %s: %w", org, err)
		}
		all = append(all, page...)
		url = ""
		if m := nextLink.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
			url = m[1]
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}

// RawFile fetches one file from a repo branch. It returns ErrNotFound on 404.
func (c Client) RawFile(ctx context.Context, org, repo, branch, path string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s/%s/%s/%s", c.rawBase(), org, repo, branch, path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// Reagents returns the non-archived repos of org carrying topic. Those
// without docs/antora.yml on their default branch are returned as skipped.
func (c Client) Reagents(ctx context.Context, org, topic string) (found []Repo, skipped []Skipped, err error) {
	repos, err := c.OrgRepos(ctx, org)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range repos {
		if r.Archived || !slices.Contains(r.Topics, topic) {
			continue
		}
		_, err := c.RawFile(ctx, org, r.Name, r.DefaultBranch, "docs/antora.yml")
		switch {
		case errors.Is(err, ErrNotFound):
			skipped = append(skipped, Skipped{Repo: r, Reason: "no docs/antora.yml"})
		case err != nil:
			skipped = append(skipped, Skipped{Repo: r, Reason: err.Error()})
		default:
			found = append(found, r)
		}
	}
	return found, skipped, nil
}
