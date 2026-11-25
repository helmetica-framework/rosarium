package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ghRepo struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	DefaultBranch string   `json:"default_branch"`
	HTMLURL       string   `json:"html_url"`
	Topics        []string `json:"topics"`
	Archived      bool     `json:"archived"`
}

func newServer(t *testing.T, pages [][]ghRepo, antora map[string]bool) (*httptest.Server, *[]*http.Request) {
	t.Helper()
	var reqs []*http.Request
	mux := http.NewServeMux()
	var srv *httptest.Server
	mux.HandleFunc("/orgs/helmetica-framework/repos", func(w http.ResponseWriter, r *http.Request) {
		reqs = append(reqs, r)
		page := 1
		fmt.Sscan(r.URL.Query().Get("page"), &page)
		if page < len(pages) {
			w.Header().Set("Link", fmt.Sprintf(`<%s/orgs/helmetica-framework/repos?per_page=100&page=%d>; rel="next"`, srv.URL, page+1))
		}
		json.NewEncoder(w).Encode(pages[page-1])
	})
	mux.HandleFunc("/raw/", func(w http.ResponseWriter, r *http.Request) {
		// /raw/<org>/<repo>/<branch>/docs/antora.yml
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/raw/"), "/", 4)
		if len(parts) == 4 && parts[3] == "docs/antora.yml" && antora[parts[1]] {
			fmt.Fprint(w, "name: "+parts[1]+"\nversion: ~\n")
			return
		}
		http.NotFound(w, r)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func client(srv *httptest.Server) Client {
	return Client{HTTP: srv.Client(), APIBase: srv.URL, RawBase: srv.URL + "/raw"}
}

func TestReagents_FiltersTopicArchivedAndAntora(t *testing.T) {
	repos := []ghRepo{
		{Name: "reagent-redis", DefaultBranch: "main", Topics: []string{"helmetica-reagent", "database"}},
		{Name: "reagent-old", DefaultBranch: "main", Topics: []string{"helmetica-reagent"}, Archived: true},
		{Name: "reagent-mariadb", DefaultBranch: "main", Topics: []string{"helmetica-reagent"}},
		{Name: "adept", DefaultBranch: "main"},
	}
	srv, _ := newServer(t, [][]ghRepo{repos}, map[string]bool{"reagent-redis": true, "reagent-old": true})
	found, skipped, err := client(srv).Reagents(context.Background(), "helmetica-framework", "helmetica-reagent")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Name != "reagent-redis" {
		t.Fatalf("found = %+v", found)
	}
	if len(skipped) != 1 || skipped[0].Repo.Name != "reagent-mariadb" || skipped[0].Reason != "no docs/antora.yml" {
		t.Fatalf("skipped = %+v", skipped)
	}
}

func TestOrgRepos_Pagination(t *testing.T) {
	srv, _ := newServer(t, [][]ghRepo{{{Name: "b"}}, {{Name: "a"}}}, nil)
	repos, err := client(srv).OrgRepos(context.Background(), "helmetica-framework")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 || repos[0].Name != "a" || repos[1].Name != "b" {
		t.Fatalf("want both pages sorted by name, got %+v", repos)
	}
}

func TestOrgRepos_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer srv.Close()
	_, err := client(srv).OrgRepos(context.Background(), "helmetica-framework")
	if err == nil || !strings.Contains(err.Error(), "GITHUB_TOKEN") {
		t.Fatalf("want error mentioning GITHUB_TOKEN, got %v", err)
	}
}

func TestClient_SendsToken(t *testing.T) {
	srv, reqs := newServer(t, [][]ghRepo{{}}, nil)
	c := client(srv)
	c.Token = "s3cr3t"
	if _, err := c.OrgRepos(context.Background(), "helmetica-framework"); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[0].Header.Get("Authorization"); got != "Bearer s3cr3t" {
		t.Errorf("Authorization = %q", got)
	}
	c.Token = ""
	if _, err := c.OrgRepos(context.Background(), "helmetica-framework"); err != nil {
		t.Fatal(err)
	}
	if got := (*reqs)[1].Header.Get("Authorization"); got != "" {
		t.Errorf("unauthenticated request sent Authorization %q", got)
	}
}

func TestRawFile_NotFound(t *testing.T) {
	srv, _ := newServer(t, [][]ghRepo{{}}, nil)
	_, err := client(srv).RawFile(context.Background(), "helmetica-framework", "x", "main", "docs/antora.yml")
	if err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
