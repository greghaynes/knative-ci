package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	ghclient "github.com/google/go-github/github"
	buildv1alpha1 "github.com/knative/build/pkg/apis/build/v1alpha1"
	buildclientset "github.com/knative/build/pkg/client/clientset/versioned"
	buildv1alpha1client "github.com/knative/build/pkg/client/clientset/versioned/typed/build/v1alpha1"
	"golang.org/x/oauth2"
	ghwebhooks "gopkg.in/go-playground/webhooks.v5/github"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
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
	paToken              string
	buildTemplatesClient buildv1alpha1client.BuildTemplateInterface
}

type RepoConfigStep struct {
	Name  string
	Image string
	Args  []string
}

type RepoConfig struct {
	Steps []RepoConfigStep
}

func createBuildTemplate(repoSlug, ref string, config *RepoConfig) (*buildv1alpha1.BuildTemplate, error) {
	steps := []corev1.Container{}
	for _, configStep := range config.Steps {
		newStep := corev1.Container{
			Name:  configStep.Name,
			Image: configStep.Image,
			Args:  configStep.Args,
		}
		steps = append(steps, newStep)
	}

	btName := "knative-ci-" + strings.Replace(repoSlug, "/", "-", 1) + "-ref-" + ref

	return &buildv1alpha1.BuildTemplate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "build.knative.dev/v1alpha1",
			Kind:       "BuildTemplate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: btName,
		},
		Spec: buildv1alpha1.BuildTemplateSpec{
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
		},
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
	prHead := &pr.PullRequest.Head

	err := h.getRepoConfig(ctx, ghcli, prHead.Repo.Owner.Login, prHead.Repo.Name, prHead.Ref, &repoConfig)
	if err != nil {
		log.Printf("Error getting repo config: %v", err)
		return
	}

	bt, err := createBuildTemplate(prHead.Repo.FullName, prHead.Ref, &repoConfig)
	if err != nil {
		log.Printf("Error creating buildtemplate: %v", err)
		return
	}

	existingBt, err := h.buildTemplatesClient.Get(bt.ObjectMeta.Name, metav1.GetOptions{})
	if err != nil {
		bt, err = h.buildTemplatesClient.Create(bt)
	} else {
		bt.ObjectMeta.ResourceVersion = existingBt.ObjectMeta.ResourceVersion
		bt, err = h.buildTemplatesClient.Update(bt)
	}

	if err != nil {
		log.Printf("Error create/updateing buildtemplate: %v", err)
	}

	log.Printf("Got buildtemplate: %v", bt)
	log.Print("Doing nothing")
}

var (
	kubeconfig = flag.String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	masterURL  = flag.String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
)

func main() {
	flag.Parse()
	log.Print("Starting webhook-handler")

	cfg, err := clientcmd.BuildConfigFromFlags(*masterURL, *kubeconfig)
	if err != nil {
		log.Printf("Error building kubeconfig: %v", err)
	}

	buildClient, err := buildclientset.NewForConfig(cfg)
	if err != nil {
		log.Printf("Error building Build clientset: %v", err)
	}

	btCli := buildClient.BuildV1alpha1().BuildTemplates("default")

	personalAccessToken := os.Getenv(personalAccessTokenKey)
	secretToken := os.Getenv(webhookSecretKey)

	h := &Handler{
		paToken:              personalAccessToken,
		buildTemplatesClient: btCli,
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

	log.Print("Started webhook-handler")
	http.ListenAndServe(":8080", nil)
}
