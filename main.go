package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/google/go-github/v31/github"
	"github.com/gorilla/feeds"
	"github.com/gorilla/mux"
	"github.com/speps/go-hashids"
	"golang.org/x/oauth2"
)

type RepoScanner struct {
	client *github.Client
	delay  int
	hash   *hashids.HashID
	repo   []*Repo
	feed   []*feeds.Feed
	mutex  sync.Mutex
}

type Repo struct {
	feed          *feeds.Feed
	owner         string
	repo          string
	label         []string
	commentsCount map[int64]int
	commentsTime  map[int64]time.Time
}

func (r Repo) String() string {
	return fmt.Sprintf("%v/%v, %v", r.owner, r.repo, r.label)
}

func NewRepoScanner(client *github.Client, delay int, hash *hashids.HashID) *RepoScanner {
	return &RepoScanner{client: client, delay: delay, hash: hash}
}

func (s *RepoScanner) Start() {
	for {
		fmt.Println("Starting Scanner")
		for i := range s.repo {
			fmt.Printf("Scanning %v\n", s.repo[i])
			err := s.scanIssues(s.repo[i])
			if err != nil {
				fmt.Println(err)
			}
		}
		time.Sleep(time.Duration(s.delay) * time.Minute)
	}
}

func (s *RepoScanner) AddRepo(owner string, repo string, label []string) (string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	hash, err := s.hash.Encode([]int{len(s.feed)})
	if err != nil {
		return "", err
	}

	r, _, err := s.client.Repositories.Get(context.Background(), owner, repo)
	if err != nil {
		return "", err
	}

	des := "no description given"
	if r.Description != nil {
		des = *r.Description
	}

	feed := &feeds.Feed{
		Title:       fmt.Sprintf("%v/%v Labels: %v", owner, repo, label),
		Link:        &feeds.Link{Href: *r.HTMLURL},
		Description: des,
		Author:      &feeds.Author{Name: *r.Owner.Login},
		Created:     r.CreatedAt.Time,
	}

	s.feed = append(s.feed, feed)
	s.repo = append(s.repo, &Repo{
		owner:         owner,
		repo:          repo,
		label:         label,
		feed:          feed,
		commentsCount: make(map[int64]int),
		commentsTime:  make(map[int64]time.Time),
	})

	fmt.Printf("Created new Feed %v for Repo %v/%v with lables %v\n", hash, owner, repo, label)

	return hash, nil
}

func (s *RepoScanner) scanIssues(repo *Repo) error {
	fmt.Printf("Scanning for new Issues and Comments updates for Repo %v\n", repo)
	issues, _, err := s.client.Issues.ListByRepo(
		context.Background(),
		repo.owner,
		repo.repo,
		&github.IssueListByRepoOptions{Labels: repo.label},
	)
	if err != nil {
		return err
	}

	for _, issue := range issues {
		val, ok := repo.commentsCount[*issue.ID]
		if !ok {
			fmt.Printf("Found new Issue (%v)\n", *issue.Title)
			repo.addIssue(issue)
			err := s.scanComments(repo, issue, repo.commentsTime[*issue.ID])
			if err != nil {
				return err
			}
			continue
		}

		if *issue.Comments > val {
			fmt.Printf("Repo: %v Issue %v has new Comments, adding them\n", repo, *issue.Title)
			err := s.scanComments(repo, issue, repo.commentsTime[*issue.ID])
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Repo) addIssue(issue *github.Issue) {
	r.feed.Add(&feeds.Item{
		Title:       fmt.Sprintf("New Issue '%v' with matching lables found", *issue.Title),
		Link:        &feeds.Link{Href: *issue.URL},
		Description: *issue.Body,
		Author:      &feeds.Author{Name: *issue.User.Login},
		Created:     *issue.CreatedAt,
	})

	r.commentsCount[*issue.ID] = 0
	r.commentsTime[*issue.ID] = *issue.CreatedAt
}

func (s *RepoScanner) scanComments(repo *Repo, issue *github.Issue, since time.Time) error {
	sinceAdd := since.Add(1 * time.Second)

	comments, _, err := s.client.Issues.ListComments(context.Background(),
		repo.owner, repo.repo, *issue.Number,
		&github.IssueListCommentsOptions{
			Since: &sinceAdd,
		},
	)
	if err != nil {
		return err
	}

	for _, comment := range comments {
		repo.addComment(issue, comment)
	}

	return nil
}

func (r *Repo) addComment(issue *github.Issue, comment *github.IssueComment) {
	r.feed.Add(&feeds.Item{
		Title:       fmt.Sprintf("New Comment on '%v' from %v", issue.GetTitle(), *comment.User.Login),
		Link:        &feeds.Link{Href: *comment.URL},
		Description: *comment.Body,
		Author:      &feeds.Author{Name: *comment.User.Login},
		Created:     *comment.CreatedAt,
	})

	r.commentsCount[*issue.ID]++
	r.commentsTime[*issue.ID] = *comment.CreatedAt
}

func (s *RepoScanner) getFeed(hash string) (*feeds.Feed, error) {
	numbers, err := s.hash.DecodeWithError(hash)
	if err != nil {
		return nil, err
	}

	if len(numbers) != 1 {
		return nil, errors.New("invalid Feed Hash")
	}

	return s.feed[numbers[0]], nil
}

func createRssHandler(scanner *RepoScanner) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		feed, err := scanner.getFeed(vars["hash"])
		if err != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%v", err)
			return
		}

		rss, err := feed.ToRss()
		if err != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%v", rss)
	}
}

func createAtomHandler(scanner *RepoScanner) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		feed, err := scanner.getFeed(vars["hash"])
		if err != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%v", err)
			return
		}

		atom, err := feed.ToAtom()
		if err != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%v", atom)
	}
}

func createJSONHandler(scanner *RepoScanner) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		feed, err := scanner.getFeed(vars["hash"])
		if err != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%v", err)
			return
		}

		json, err := feed.ToJSON()
		if err != nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "%v", err)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%v", json)
	}
}

func main() {
	hd := hashids.NewData()
	hd.Salt = "SuperSecretSalt"
	hd.MinLength = 8

	if os.Getenv("RSS_FEED_SALT") != "" {
		hd.Salt = os.Getenv("RSS_FEED_SALT")
	}

	h, err := hashids.NewWithData(hd)
	if err != nil {
		log.Fatal(err)
	}

	if os.Getenv("RSS_FEED_GITHUB_TOKEN") == "" ||
		os.Getenv("RSS_FEED_DEFAULT_ORG") == "" ||
		os.Getenv("RSS_FEED_DEFAULT_REPO") == "" ||
		os.Getenv("RSS_FEED_DEFAULT_LABEL") == "" {
		log.Fatal("Missing Enviroment Variables")
	}

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("RSS_FEED_GITHUB_TOKEN")},
	)

	tc := oauth2.NewClient(ctx, ts)

	scanner := NewRepoScanner(github.NewClient(tc), 5, h)
	feedHash, err := scanner.AddRepo(
		os.Getenv("RSS_FEED_DEFAULT_ORG"),
		os.Getenv("RSS_FEED_DEFAULT_REPO"),
		[]string{os.Getenv("RSS_FEED_DEFAULT_LABEL")},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Hash for default feed is:", feedHash)

	go scanner.Start()

	r := mux.NewRouter()
	r.HandleFunc("/rss/{hash}", createRssHandler(scanner))
	r.HandleFunc("/atom/{hash}", createAtomHandler(scanner))
	r.HandleFunc("/json/{hash}", createJSONHandler(scanner))

	fmt.Println(http.ListenAndServe(":8080", r))
}
