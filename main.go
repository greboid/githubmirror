package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/kouhin/envflag"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	checkoutPath = flag.String("checkoutpath", "/data", "Folder to clone repos into")
	authToken    = flag.String("authtoken", "", "Personal Access token")
	duration     = flag.Duration("duration", 0, "Number of seconds between executions")
	starred      = flag.Bool("starred", false, "Mirror starred repositories")
)

type Mirror struct {
	ctx    context.Context
	client *githubv4.Client
	auth   *http.BasicAuth
	login  string
}

type ListRepositories struct {
	User struct {
		Repositories struct {
			PageInfo struct {
				EndCursor   *githubv4.String
				HasNextPage bool
			}
			Nodes []Repository
		} `graphql:"repositories(first: 100, after: $cursor)"`
	} `graphql:"user(login: $login)"`
}

type ListStarredRepositories struct {
	User struct {
		StarredRepositories struct {
			PageInfo struct {
				EndCursor   *githubv4.String
				HasNextPage bool
			}
			Edges []struct {
				Node Repository
			}
		} `graphql:"starredRepositories(first: 100, after: $cursor)"`
	} `graphql:"user(login: $login)"`
}

type Repository struct {
	NameWithOwner string
	Url string
}

func main() {
	if err := envflag.Parse(); err != nil {
		fmt.Printf("Unable to load config: %s\r\n", err.Error())
		return
	}
	if *authToken == "" || *checkoutPath == "" {
		flag.Usage()
		return
	}

	mirror := &Mirror{
		ctx: context.Background(),
		client: githubv4.NewClient(oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: *authToken},
		))),
		auth: &http.BasicAuth{
			Username: "authToken", //Anything but blank
			Password: *authToken,
		},
	}
	user, err := mirror.getUser()
	if err != nil {
		log.Fatalf("Unable to get username: %s", err.Error())
	}
	mirror.login = user

	if *duration < time.Minute {
		if err := mirror.updateOrCloneRepos(); err != nil {
			log.Fatalf("Error mirroring repos: %s\n", err.Error())
		}
		return
	}
	log.Printf("Running every %s", *duration)
	for {
		log.Print("Mirroring Repositories\n")
		if err := mirror.updateOrCloneRepos(); err != nil {
			log.Fatalf("Error mirroring repos: %s\n", err.Error())
		}
		time.Sleep(*duration)
	}
}

func (m *Mirror) updateOrClone(repo Repository) {
	if _, err := os.Stat(filepath.Join(*checkoutPath, repo.NameWithOwner)); err == nil {
		log.Printf("Updating: %s\n", repo.Url)
		m.update(repo)
	} else {
		log.Printf("Cloning: %s\n", repo.Url)
		m.clone(repo)
	}
}

func (m *Mirror) updateOrCloneRepos() error {
	log.Printf("Getting repositories\n")
	repos := m.getRepos()
	log.Printf("Looping repositories\n")
	for repo := range repos {
		m.updateOrClone(repos[repo])
	}
	if *starred {
		repos = m.getStarredRepos()
		log.Printf("Looping starred repositories\n")
		for repo := range repos {
			m.updateOrClone(repos[repo])
		}
	}
	log.Printf("Finished looping\n")
	return nil
}

func (m *Mirror) getRepos() []Repository {
	q := ListRepositories{}
	variables := map[string]interface{}{
		"login": githubv4.String(m.login),
		"cursor": (*githubv4.String)(nil),
	}
	var allRepos []Repository
	for {
		err := m.client.Query(m.ctx, &q, variables)
		if err != nil {
			return nil
		}
		allRepos = append(allRepos, q.User.Repositories.Nodes...)
		if !q.User.Repositories.PageInfo.HasNextPage {
			break
		}
		variables["cursor"] = q.User.Repositories.PageInfo.EndCursor
	}
	return allRepos
}

func (m *Mirror) getStarredRepos() []Repository {
	q := ListStarredRepositories{}
	variables := map[string]interface{}{
		"login": githubv4.String(m.login),
		"cursor": (*githubv4.String)(nil),
	}
	var allRepos []Repository
	for {
		err := m.client.Query(m.ctx, &q, variables)
		if err != nil {
			log.Printf("Error: %+v", err.Error())
			return nil
		}
		for index := range q.User.StarredRepositories.Edges {
			allRepos = append(allRepos, q.User.StarredRepositories.Edges[index].Node)
		}
		if !q.User.StarredRepositories.PageInfo.HasNextPage {
			break
		}
		variables["cursor"] = q.User.StarredRepositories.PageInfo.EndCursor
	}
	return allRepos
}

func (m *Mirror) clone(repo Repository) {
	_, err := git.PlainClone(filepath.Join(*checkoutPath, repo.NameWithOwner), false, &git.CloneOptions{
		URL:  repo.Url,
		Tags: git.AllTags,
		Auth: m.auth,
	})
	if err != nil {
		log.Printf("Error cloning: %s\n", err)
	}
}

func (m *Mirror) update(repo Repository) {
	gitRepo, err := git.PlainOpen(filepath.Join(*checkoutPath, repo.NameWithOwner))
	if err != nil {
		log.Printf("Open error: %s\n", err)
	}
	workTree, err := gitRepo.Worktree()
	if err != nil {
		log.Printf("Worktree error: %s\n", err)
	}
	err = workTree.Pull(&git.PullOptions{
		Force: true,
		Auth:  m.auth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		log.Printf("Pull error: %s\n", err)
	}
}

func (m *Mirror) getUser() (string, error) {
	var q struct {
		Viewer struct {
			Login string
		}
	}
	err := m.client.Query(context.Background(), &q, nil)
	if err != nil {
		return "", err
	}
	return q.Viewer.Login, nil
}
