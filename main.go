package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/google/go-github/v32/github"
	"github.com/kouhin/envflag"
	"golang.org/x/oauth2"
	"log"
	"os"
	"path/filepath"
	"time"
)

var (
	checkoutPath = flag.String("checkoutpath", "", "Folder to clone repos into")
	authToken    = flag.String("authtoken", "", "Personal Access token")
	duration     = flag.Duration("duration", 0, "Number of seconds between executions")
)

type Mirror struct {
	ctx    context.Context
	client *github.Client
	auth   *http.BasicAuth
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
		client: github.NewClient(oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: *authToken},
		))),
		auth: &http.BasicAuth{
			Username: "authToken", //Anything but blank
			Password: *authToken,
		},
	}

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

func (m *Mirror) updateOrClone(repo github.Repository) {
	if _, err := os.Stat(filepath.Join(*checkoutPath, *repo.FullName)); err == nil {
		log.Printf("Updating: %s\n", *repo.CloneURL)
		m.update(repo)
	} else {
		log.Printf("Cloning: %s\n", *repo.CloneURL)
		m.clone(repo)
	}
}

func (m *Mirror) updateOrCloneRepos() error {
	log.Printf("Getting repositories\n")
	repos := m.getRepos()
	log.Printf("Looping repositories\n")
	for repo := range repos {
		m.updateOrClone(*repos[repo])
	}
	log.Printf("Finished looping\n")
	return nil
}

func (m *Mirror) getRepos() []*github.Repository {
	opt := &github.RepositoryListOptions{
		ListOptions: github.ListOptions{PerPage: 10},
	}
	var allRepos []*github.Repository
	for {
		repos, resp, err := m.client.Repositories.List(m.ctx, "", opt)
		if err != nil {
			log.Printf("Error listing: %s\n", err)
		}
		allRepos = append(allRepos, repos...)
		if resp.NextPage == 0 {
			break
		}
		opt.Page = resp.NextPage
	}
	return allRepos
}

func (m *Mirror) clone(repo github.Repository) {
	_, err := git.PlainClone(filepath.Join(*checkoutPath, *repo.FullName), false, &git.CloneOptions{
		URL:  *repo.CloneURL,
		Tags: git.AllTags,
		Auth: m.auth,
	})
	if err != nil {
		log.Printf("Error cloning: %s\n", err)
	}
}

func (m *Mirror) update(repo github.Repository) {
	gitRepo, err := git.PlainOpen(filepath.Join(*checkoutPath, *repo.FullName))
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
