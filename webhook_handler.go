package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	ghclient "github.com/google/go-github/github"
	"golang.org/x/oauth2"
	ghwebhooks "gopkg.in/go-playground/webhooks.v5/github"
)

const (
	// Secret given to github. Used for verifying the incoming objects.
	personalAccessTokenKey = "GITHUB_PERSONAL_TOKEN"
	// Personal Access Token created in github that allows us to make
	// calls into github.
	webhookSecretKey = "WEBHOOK_SECRET"
)

type Handler struct {
	// Personal Access Token
	paToken string
}

type RepoConfig struct {
}

func (h *Handler) getRepoConfig(ctx context.Context, ghcli *ghclient.Client, repoOwner, repoName string) (string, error) {
	content, _, _, err := ghcli.Repositories.GetContents(ctx, repoOwner, repoName, ".ciless.yaml", nil)
	if err != nil {
		return "", err
	}
	return content.GetContent()
}

func (h *Handler) createGhClient(ctx context.Context) *ghclient.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: h.paToken},
	)
	tc := oauth2.NewClient(ctx, ts)
	return ghclient.NewClient(tc)
}

// HandlePullRequest is invoked whenever a PullRequest is modified (created, updated, etc.)
func (h *Handler) HandlePullRequest(ctx context.Context, ghcli *ghclient.Client, pr *ghwebhooks.PullRequestPayload) {
	log.Print("Handling Pull Request")

	repoConfig, err := h.getRepoConfig(ctx, ghcli, pr.PullRequest.Head.Repo.Owner.Login, pr.PullRequest.Head.Repo.Name)
	if err != nil {
		log.Printf("Error getting repo config: %v", err)
		return
	}
	log.Printf("Got config %v", repoConfig)
}

func main() {
	flag.Parse()
	log.Print("Started webhook-handler")

	personalAccessToken := os.Getenv(personalAccessTokenKey)
	secretToken := os.Getenv(webhookSecretKey)

	h := &Handler{
		paToken: personalAccessToken,
	}

	hook, _ := ghwebhooks.New(ghwebhooks.Options.Secret(secretToken))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		payload, err := hook.Parse(r, ghwebhooks.PullRequestEvent)
		if err != nil {
			if err == ghwebhooks.ErrEventNotFound {
				log.Print("Unexpected webhook event")
			} else {
				log.Printf("Webhook parse error: %#v", err)
			}
			return
		}

		ctx := context.Background()
		ghcli := h.createGhClient(ctx)

		switch payload.(type) {
		case ghwebhooks.PullRequestPayload:
			pr := payload.(ghwebhooks.PullRequestPayload)
			h.HandlePullRequest(ctx, ghcli, &pr)
		}
	})

	http.ListenAndServe(":8080", nil)
}
