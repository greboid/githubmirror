package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/imdario/mergo"
	"github.com/kouhin/envflag"
	"github.com/shurcooL/githubv4"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

var (
	checkoutPath = flag.String("checkoutpath", "/data", "Folder to clone repos into")
	authToken    = flag.String("authtoken", "", "Personal Access token")
	duration     = flag.Duration("duration", 0, "Number of seconds between executions")
	starred      = flag.Bool("starred", false, "Mirror starred repositories")
	debug        = flag.Bool("debug", false, "Output debug logging")
	log          *zap.SugaredLogger
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
	Url           string
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
	logger, err := CreateLogger(*debug)
	if err != nil {
		fmt.Printf("Unable to create logger: %s\r\n", err.Error())
		return
	}
	log = logger

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
		return
	}
	mirror.login = user

	if *duration < time.Minute {
		if err := mirror.updateOrCloneRepos(); err != nil {
			log.Fatalf("Error mirroring repos: %s", err.Error())
		}
		return
	}
	log.Infof("Running every %s", *duration)
	for {
		log.Infof("Mirroring Repositories")
		if err := mirror.updateOrCloneRepos(); err != nil {
			log.Fatalf("Error mirroring repos: %s", err.Error())
		}
		time.Sleep(*duration)
	}
}

func (m *Mirror) updateOrClone(repo Repository) {
	if _, err := os.Stat(filepath.Join(*checkoutPath, repo.NameWithOwner)); err == nil {
		log.Debugf("Updating: %s", repo.Url)
		m.update(repo)
	} else {
		log.Debugf("Cloning: %s", repo.Url)
		m.clone(repo)
	}
}

func (m *Mirror) updateOrCloneRepos() error {
	reposToSync := make(map[Repository]struct{})
	log.Infof("Getting repositories")
	repos := m.getRepos()
	err := mergo.Map(&reposToSync, &repos)
	if err != nil {
		log.Errorf("Unable to merge repos: %s", err.Error())
	}
	if *starred {
		repos = m.getStarredRepos()
		err := mergo.Map(&reposToSync, &repos)
		if err != nil {
			log.Errorf("Unable to merge starred repos: %s", err.Error())
		}
	}
	log.Infof("Looping %d repositories", len(reposToSync))
	for repo := range reposToSync {
		m.updateOrClone(repo)
	}
	log.Infof("Finished looping")
	return nil
}

func (m *Mirror) getRepos() map[Repository]struct{} {
	q := ListRepositories{}
	variables := map[string]interface{}{
		"login":  githubv4.String(m.login),
		"cursor": (*githubv4.String)(nil),
	}
	allRepos := make(map[Repository]struct{})
	for {
		err := m.client.Query(m.ctx, &q, variables)
		if err != nil {
			log.Errorf("Unable to query for repositories: %s", err.Error())
			return nil
		}
		for index := range q.User.Repositories.Nodes {
			allRepos[q.User.Repositories.Nodes[index]] = struct{}{}
		}
		if !q.User.Repositories.PageInfo.HasNextPage {
			break
		}
		variables["cursor"] = q.User.Repositories.PageInfo.EndCursor
	}
	return allRepos
}

func (m *Mirror) getStarredRepos() map[Repository]struct{} {
	q := ListStarredRepositories{}
	variables := map[string]interface{}{
		"login":  githubv4.String(m.login),
		"cursor": (*githubv4.String)(nil),
	}
	allRepos := make(map[Repository]struct{})
	for {
		err := m.client.Query(m.ctx, &q, variables)
		if err != nil {
			log.Errorf("Unable to query for repositories: %s", err.Error())
			return nil
		}
		for index := range q.User.StarredRepositories.Edges {
			allRepos[q.User.StarredRepositories.Edges[index].Node] = struct{}{}
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
		log.Errorf("Error cloning: %s: %", repo.NameWithOwner, err)
	}
}

func (m *Mirror) update(repo Repository) {
	gitRepo, err := git.PlainOpen(filepath.Join(*checkoutPath, repo.NameWithOwner))
	if err != nil {
		log.Errorf("Open error: %s: %s", repo.NameWithOwner, err)
	}
	workTree, err := gitRepo.Worktree()
	if err != nil {
		log.Errorf("Worktree error: %s: %s", repo.NameWithOwner, err)
	}
	err = workTree.Pull(&git.PullOptions{
		Force: true,
		Auth:  m.auth,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		log.Errorf("Pull error: %s: %s", repo.NameWithOwner, err)
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

func CreateLogger(debug bool) (*zap.SugaredLogger, error) {
	zapConfig := zap.NewDevelopmentConfig()
	zapConfig.DisableCaller = !debug
	zapConfig.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	zapConfig.DisableStacktrace = !debug
	zapConfig.OutputPaths = []string{"stdout"}
	if debug {
		zapConfig.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	} else {
		zapConfig.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	logger, err := zapConfig.Build()
	if err != nil {
		return nil, err
	}
	return logger.Sugar(), nil
}
