package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"

	ghclient "github.com/google/go-github/github"
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	"golang.org/x/oauth2"
	ghwebhooks "gopkg.in/go-playground/webhooks.v5/github"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
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

type RepoConfigStep struct {
	Name  string
	Image string
	Args  []string
}

type RepoConfig struct {
	Steps []RepoConfigStep
}

func createBuildTemplateSpec(config *RepoConfig) (*buildv1alpha1.BuildTemplateSpec, error) {
	steps := []corev1.Container{}
	for _, configStep := range config.Steps {
		newStep := corev1.Container{
			Name:  configStep.Name,
			Image: configStep.Image,
			Args:  configStep.Args,
		}
		steps = append(steps, newStep)
	}

	return &buildv1alpha1.BuildTemplateSpec{
		Parameters: []buildv1alpha1.ParameterSpec{
			{
				Name:        "REPO_DIR",
				Description: "Local directory path to checked out repository",
			},
			{
				Name:        "USER_REPO_SLUG",
				Description: "<username>/<repository_name>",
			},
			{
				Name:        "COMMIT_REF",
				Description: "Git REF for the current change",
			},
		},
		Steps: steps,
	}, nil
}

func (h *Handler) getRepoConfig(ctx context.Context, ghcli *ghclient.Client, repoOwner, repoName, ref string, config *RepoConfig) error {
	getopts := ghclient.RepositoryContentGetOptions{
		Ref: ref,
	}
	content, _, _, err := ghcli.Repositories.GetContents(ctx, repoOwner, repoName, ".ciless.yaml", &getopts)
	if err != nil {
		return err
	}
	rawConfig, err := content.GetContent()
	if err != nil {
		return err
	}

	return yaml.Unmarshal([]byte(rawConfig), config)
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

	var repoConfig RepoConfig
	err := h.getRepoConfig(ctx, ghcli, pr.PullRequest.Head.Repo.Owner.Login, pr.PullRequest.Head.Repo.Name, pr.PullRequest.Head.Ref, &repoConfig)
	if err != nil {
		log.Printf("Error getting repo config: %v", err)
		return
	}

	bt, err := createBuildTemplateSpec(&repoConfig)
	if err != nil {
		log.Printf("Error creating buildtemplate: %v", err)
		return
	}

	log.Printf("Got buildtemplate: %v", bt)
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
