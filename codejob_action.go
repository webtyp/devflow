package devflow

import (
	_ "embed"
	"fmt"
	gitmod "webtyp.com/git"
	"os"
	"path/filepath"

	keyring "webtyp.com/keyring/auto"
)

//go:embed templates/codejob.yml
var codejobWorkflowTemplate string

// InitCodejobAction scaffolds the GitHub Actions workflow at .github/workflows/codejob.yml
// and registers the JULES_API_KEY and GH_TOKEN secrets in the repository or organization.
func InitCodejobAction(force bool, org, visibility string) error {
	workflowPath := filepath.Join(".github", "workflows", "codejob.yml")

	exists := false
	if _, err := os.Stat(workflowPath); err == nil {
		exists = true
	}

	if exists && !force {
		fmt.Printf("ℹ️  %s already exists. Use --force to overwrite.\n", workflowPath)
	} else {
		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(workflowPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for workflow: %w", err)
		}
		// Write workflow file
		if err := os.WriteFile(workflowPath, []byte(codejobWorkflowTemplate), 0644); err != nil {
			return fmt.Errorf("failed to write workflow file: %w", err)
		}
		fmt.Printf("✅ Scaffolded %s successfully.\n", workflowPath)
	}

	// Resolve secrets
	auth, err := NewJulesAuth()
	if err != nil {
		return fmt.Errorf("failed to initialize JulesAuth: %w", err)
	}
	apiKey, err := auth.EnsureAPIKey()
	if err != nil {
		return fmt.Errorf("failed to retrieve JULES_API_KEY: %w", err)
	}

	patAuth := gitmod.NewGitHubPATAuth(keyring.OpenKeyring("devflow"))
	ghToken, err := patAuth.EnsureToken()
	if err != nil {
		return fmt.Errorf("failed to retrieve GH_TOKEN: %w", err)
	}

	// Auto-detect repo
	owner, repo, err := autoDetectOwnerRepo()
	if err != nil {
		return fmt.Errorf("failed to auto-detect GitHub repo name: %w", err)
	}
	fullRepo := owner + "/" + repo

	gh, err := gitmod.NewGitHub(func(args ...any) {}, nil, patAuth)
	if err != nil {
		return fmt.Errorf("failed to initialize GitHub client: %w", err)
	}

	fmt.Println("🔑 Registering secrets in GitHub...")
	if err := gh.SetSecretWithScope(fullRepo, "JULES_API_KEY", apiKey, org, visibility); err != nil {
		return err
	}
	if err := gh.SetSecretWithScope(fullRepo, "GH_TOKEN", ghToken, org, visibility); err != nil {
		return err
	}

	fmt.Println("✅ GitHub secrets configured successfully.")
	return nil
}
