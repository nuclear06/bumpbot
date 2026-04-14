package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/pelletier/go-toml"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"
)

type Issue struct {
	Id     githubv4.ID
	Number githubv4.Int
	Title  githubv4.String
	Body   githubv4.String
	State  githubv4.IssueState
	Url    githubv4.String
}

type PackageConfig struct {
	tree *toml.Tree
	name string
}

type GentooDepsRequest struct {
	Lang           string
	Repo           string
	SourceURL      string
	Tag            string
	P              string
	Workdir        string
	Vendordir      string
	SourceIssueURL string
}

func loadPackageConfig(tomlPath string, packageName string) PackageConfig {
	file, err := os.Open(tomlPath)
	if err != nil {
		log.Fatalf("Failed to open TOML file: %v", err)
	}
	defer file.Close()

	tree, err := toml.LoadReader(file)
	if err != nil {
		log.Fatalf("Failed to parse TOML file: %v", err)
	}

	if !tree.Has(packageName) {
		log.Fatalf("Package %s not found in overlay.toml", packageName)
	}

	node, ok := tree.Get(packageName).(*toml.Tree)
	if !ok {
		log.Fatalf("Package %s in overlay.toml is not a table", packageName)
	}

	return PackageConfig{
		tree: node,
		name: packageName,
	}
}

func (c PackageConfig) get(key string) interface{} {
	return c.tree.Get(key)
}

func (c PackageConfig) getString(key string) string {
	value := c.get(key)
	if value == nil {
		return ""
	}

	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprint(typed)
	}
}

func (c PackageConfig) getStringList(key string) []string {
	value := c.get(key)
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return []string{typed}
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, fmt.Sprint(item))
		}
		return result
	default:
		return []string{fmt.Sprint(typed)}
	}
}

func (c PackageConfig) getBool(key string) bool {
	value := c.get(key)
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true")
	default:
		return strings.EqualFold(fmt.Sprint(typed), "true")
	}
}

func repoParts(repoName string) (string, string) {
	parts := strings.Split(repoName, "/")
	if len(parts) != 2 {
		log.Fatalf("Invalid repo name: %s", repoName)
	}
	return parts[0], parts[1]
}

func newGitHubClient(token string) *githubv4.Client {
	httpClient := oauth2.NewClient(
		context.Background(),
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}),
	)
	return githubv4.NewClient(httpClient)
}

func getRepositoryID(client *githubv4.Client, repoName string) githubv4.String {
	var q struct {
		Repository struct {
			Id githubv4.String
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	owner, name := repoParts(repoName)
	variables := map[string]interface{}{
		"owner": githubv4.String(owner),
		"name":  githubv4.String(name),
	}

	err := client.Query(context.Background(), &q, variables)
	if err != nil {
		log.Fatal(err)
	}
	return q.Repository.Id
}

func getLabelIDByName(client *githubv4.Client, repoName string, labelName githubv4.String) githubv4.ID {
	var q struct {
		Repository struct {
			Labels struct {
				Nodes []struct {
					Id   githubv4.ID
					Name githubv4.String
				}
			} `graphql:"labels(first: 20, query: $labelName)"`
		} `graphql:"repository(owner: $owner, name: $name)"`
	}

	owner, name := repoParts(repoName)
	variables := map[string]interface{}{
		"owner":     githubv4.String(owner),
		"name":      githubv4.String(name),
		"labelName": labelName,
	}

	err := client.Query(context.Background(), &q, variables)
	if err != nil {
		log.Fatalf("Failed to fetch labels: %v", err)
	}

	for _, node := range q.Repository.Labels.Nodes {
		if node.Name == labelName {
			return node.Id
		}
	}

	log.Printf("label %s not found in repository %s, creating ...", labelName, repoName)

	var m struct {
		CreateLabel struct {
			Label struct {
				Id githubv4.ID
			}
		} `graphql:"createLabel(input: $input)"`
	}

	input := githubv4.CreateLabelInput{
		RepositoryID: getRepositoryID(client, repoName),
		Name:         labelName,
		Color:        githubv4.String("5319e7"),
		Description:  githubv4.NewString(githubv4.String("Labels created by bumpbot")),
	}

	err = client.Mutate(context.Background(), &m, input, nil)
	if err != nil {
		log.Fatalf("Failed to create label: %v", err)
	}

	return m.CreateLabel.Label.Id
}

func searchIssueByTitle(client *githubv4.Client, repoName string, titlePrefix string) Issue {
	emptyIssue := Issue{}

	var q struct {
		Search struct {
			Nodes []struct {
				Issue `graphql:"... on Issue"`
			}
		} `graphql:"search(query: $query, type: ISSUE, first: 1)"`
	}

	query := fmt.Sprintf("repo:%s is:issue in:title %s", repoName, titlePrefix)
	err := client.Query(
		context.Background(),
		&q,
		map[string]interface{}{"query": githubv4.String(query)},
	)
	if err != nil {
		log.Fatalf("Failed to search issue: %v", err)
	}

	if len(q.Search.Nodes) == 1 {
		return q.Search.Nodes[0].Issue
	}

	return emptyIssue
}

func createIssue(client *githubv4.Client, repoName string, title string, body string, labelIDs []githubv4.ID) Issue {
	var m struct {
		CreateIssue struct {
			Issue Issue
		} `graphql:"createIssue(input: $input)"`
	}

	input := githubv4.CreateIssueInput{
		RepositoryID: getRepositoryID(client, repoName),
		Title:        githubv4.String(title),
		Body:         githubv4.NewString(githubv4.String(body)),
		LabelIDs:     &labelIDs,
	}

	err := client.Mutate(context.Background(), &m, input, nil)
	if err != nil {
		log.Fatalf("Failed to create issue: %v", err)
	}

	fmt.Printf("Created issue: %s\n", m.CreateIssue.Issue.Url)
	return m.CreateIssue.Issue
}

func updateIssue(client *githubv4.Client, issue Issue, title string, body string, labelIDs []githubv4.ID) Issue {
	var m struct {
		UpdateIssue struct {
			Issue Issue
		} `graphql:"updateIssue(input: $input)"`
	}

	input := githubv4.UpdateIssueInput{
		ID:       issue.Id,
		Title:    githubv4.NewString(githubv4.String(title)),
		Body:     githubv4.NewString(githubv4.String(body)),
		LabelIDs: &labelIDs,
	}

	err := client.Mutate(context.Background(), &m, input, nil)
	if err != nil {
		log.Fatalf("Failed to update issue: %v", err)
	}

	fmt.Printf("Updated issue: %s\n", m.UpdateIssue.Issue.Url)
	return m.UpdateIssue.Issue
}

func upsertIssue(client *githubv4.Client, repoName string, titlePrefix string, title string, body string, labelIDs []githubv4.ID) Issue {
	currentIssue := searchIssueByTitle(client, repoName, titlePrefix)
	emptyIssue := Issue{}

	if currentIssue == emptyIssue {
		return createIssue(client, repoName, title, body, labelIDs)
	}

	if currentIssue.Body == githubv4.String(body) && currentIssue.Title == githubv4.String(title) {
		return currentIssue
	}

	if currentIssue.State == githubv4.IssueStateOpen {
		return updateIssue(client, currentIssue, title, body, labelIDs)
	}

	return createIssue(client, repoName, title, body, labelIDs)
}

func renderTemplate(template string, packageName string, newver string, oldver string) string {
	category := packageName
	pn := packageName
	if slash := strings.Index(packageName, "/"); slash >= 0 {
		category = packageName[:slash]
		pn = packageName[slash+1:]
	}

	replacer := strings.NewReplacer(
		"{{name}}", packageName,
		"{{package}}", packageName,
		"{{category}}", category,
		"{{pn}}", pn,
		"{{newver}}", newver,
		"{{oldver}}", oldver,
	)

	return replacer.Replace(template)
}

func buildOverlayIssueBody(config PackageConfig, oldver string, depsIssueURL string, repoIsOfficial bool) string {
	lines := []string{}
	if oldver != "" {
		lines = append(lines, "oldver: "+oldver)
	}

	accounts := config.getStringList("github_account")
	if len(accounts) > 0 {
		cc := make([]string, 0, len(accounts))
		for _, account := range accounts {
			if repoIsOfficial {
				cc = append(cc, "@"+account)
			} else {
				cc = append(cc, account)
			}
		}
		lines = append(lines, "CC: "+strings.Join(cc, " "))
	}

	if depsIssueURL != "" {
		lines = append(lines, "gentoo-deps issue: "+depsIssueURL)
	}

	return strings.Join(lines, "\n")
}

func buildGentooDepsRequest(config PackageConfig, packageName string, newver string, oldver string, sourceIssueURL string) *GentooDepsRequest {
	if config.getBool("gentoo_deps_disabled") {
		return nil
	}

	lang := config.getString("gentoo_deps_lang")
	if lang == "" {
		return nil
	}

	request := &GentooDepsRequest{
		Lang:           renderTemplate(lang, packageName, newver, oldver),
		Repo:           renderTemplate(config.getString("gentoo_deps_repo"), packageName, newver, oldver),
		SourceURL:      renderTemplate(config.getString("gentoo_deps_source_url"), packageName, newver, oldver),
		Tag:            renderTemplate(config.getString("gentoo_deps_tag"), packageName, newver, oldver),
		P:              renderTemplate(config.getString("gentoo_deps_p"), packageName, newver, oldver),
		Workdir:        renderTemplate(config.getString("gentoo_deps_workdir"), packageName, newver, oldver),
		Vendordir:      renderTemplate(config.getString("gentoo_deps_vendordir"), packageName, newver, oldver),
		SourceIssueURL: sourceIssueURL,
	}

	if request.Repo == "" && config.getString("source") == "github" {
		request.Repo = config.getString("github")
	}

	if request.Repo == "" && request.SourceURL == "" {
		log.Fatalf("Package %s is missing gentoo_deps_repo or gentoo_deps_source_url", packageName)
	}
	if request.Tag == "" {
		log.Fatalf("Package %s is missing gentoo_deps_tag", packageName)
	}
	if request.P == "" {
		log.Fatalf("Package %s is missing gentoo_deps_p", packageName)
	}

	return request
}

func buildGentooDepsIssueBody(packageName string, newver string, request GentooDepsRequest) string {
	lines := []string{
		"### Package",
		packageName,
		"",
		"### Version",
		newver,
		"",
		"### Language",
		request.Lang,
		"",
		"### GitHub Repo",
		request.Repo,
		"",
		"### Source URL",
		request.SourceURL,
		"",
		"### Tag",
		request.Tag,
		"",
		"### P",
		request.P,
		"",
		"### Workdir",
		request.Workdir,
		"",
		"### Vendordir",
		request.Vendordir,
		"",
		"### Source Issue URL",
		request.SourceIssueURL,
	}

	return strings.Join(lines, "\n")
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var (
		name     string
		newver   string
		oldver   string
		tomlFile string
	)

	flag.StringVar(&name, "name", "", "package name")
	flag.StringVar(&newver, "newver", "", "new version")
	flag.StringVar(&oldver, "oldver", "", "old version")
	flag.StringVar(&tomlFile, "file", "", "overlay.toml path")
	flag.Parse()

	repoName := os.Getenv("GITHUB_REPOSITORY")
	if repoName == "" {
		log.Fatal("GITHUB_REPOSITORY environment is not set")
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		log.Fatal("GITHUB_TOKEN environment variable is not set")
	}

	config := loadPackageConfig(tomlFile, name)
	client := newGitHubClient(token)

	gentooZhOfficialRepoName := "microcai/gentoo-zh"
	repoIsOfficial := repoName == gentooZhOfficialRepoName

	var depsIssueURL string
	gentooDepsRepository := os.Getenv("GENTOO_DEPS_REPOSITORY")
	gentooDepsToken := os.Getenv("GENTOO_DEPS_GITHUB_TOKEN")
	depsRequest := buildGentooDepsRequest(config, name, newver, oldver, "")

	if depsRequest != nil {
		if gentooDepsRepository == "" {
			log.Fatal("GENTOO_DEPS_REPOSITORY must be set when gentoo_deps_* fields are configured")
		}
		if gentooDepsToken == "" {
			log.Fatal("GENTOO_DEPS_GITHUB_TOKEN must be set when gentoo_deps_* fields are configured")
		}
	}

	nvcheckerLabelID := getLabelIDByName(client, repoName, githubv4.String("nvchecker"))

	overlayTitlePrefix := "[nvchecker] " + name + " can be bump to "
	overlayTitle := overlayTitlePrefix + newver
	overlayBody := buildOverlayIssueBody(config, oldver, "", repoIsOfficial)
	overlayIssue := upsertIssue(client, repoName, overlayTitlePrefix, overlayTitle, overlayBody, []githubv4.ID{nvcheckerLabelID})

	if depsRequest != nil {
		depsRequest.SourceIssueURL = string(overlayIssue.Url)

		depsClient := newGitHubClient(gentooDepsToken)
		depsLabelID := getLabelIDByName(depsClient, gentooDepsRepository, githubv4.String("deps-request"))
		depsTitlePrefix := "[deps] " + name + " -> "
		depsTitle := depsTitlePrefix + newver
		depsBody := buildGentooDepsIssueBody(name, newver, *depsRequest)
		depsIssue := upsertIssue(depsClient, gentooDepsRepository, depsTitlePrefix, depsTitle, depsBody, []githubv4.ID{depsLabelID})
		depsIssueURL = string(depsIssue.Url)

		overlayBody = buildOverlayIssueBody(config, oldver, depsIssueURL, repoIsOfficial)
		upsertIssue(client, repoName, overlayTitlePrefix, overlayTitle, overlayBody, []githubv4.ID{nvcheckerLabelID})
	}
}
