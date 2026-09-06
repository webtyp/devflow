package devflow

import (
	gitmod "webtyp.com/git"
	"path/filepath"
	"strings"

	"webtyp.com/context"
	"webtyp.com/wizard"
)

// GetSteps returns the sequence of steps to create a new Go project
func (gn *GoNew) GetSteps() []*wizard.Step {
	return []*wizard.Step{
		// Step 1: Project Name
		{
			LabelText: "Project Name",
			DefaultFn: func(ctx *context.Context) string { return "" },
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				if in == "" {
					return false, nil // Wait for valid input
				}
				err := ctx.Set("project_name", in)
				return true, err
			},
		},

		// Step 2: Project Location
		{
			LabelText: "Project Location",
			DefaultFn: func(ctx *context.Context) string {
				abs, _ := filepath.Abs(".")
				return filepath.Join(abs, ctx.Value("project_name"))
			},
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				if in == "" {
					return false, nil
				}
				err := ctx.Set("project_dir", in)
				return true, err
			},
		},

		// Step 3: Project Owner
		{
			LabelText: "Project Owner",
			DefaultFn: func(ctx *context.Context) string {
				// Try GitHub first
				if gn.github != nil {
					if res, err := gn.github.Get(); err == nil {
						if gh, ok := res.(gitmod.GitHubClient); ok {
							if user, err := gh.GetCurrentUser(); err == nil && user != "" {
								return user
							}
						}
					}
				}
				// Fallback to Git config
				if gn.git != nil {
					if user, err := gn.git.GetConfigUserName(); err == nil && user != "" {
						return strings.ReplaceAll(strings.ToLower(user), " ", "")
					}
				}
				return ""
			},
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				if in == "" {
					return false, nil
				}
				err := ctx.Set("project_owner", in)
				return true, err
			},
		},

		// Step 4: Description
		{
			LabelText: "Description",
			DefaultFn: func(ctx *context.Context) string { return "Created via WebTyp Wizard" },
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				if in == "" {
					return false, nil
				}
				err := ctx.Set("project_desc", in)
				return true, err
			},
		},

		// Step 5: Visibility
		{
			LabelText: "Visibility (public/private)",
			DefaultFn: func(ctx *context.Context) string { return "public" },
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				if in == "" {
					return false, nil
				}
				err := ctx.Set("project_vis", in)
				return true, err
			},
		},

		// Step 6: License
		{
			LabelText: "License",
			DefaultFn: func(ctx *context.Context) string { return "MIT" },
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				if in == "" {
					return false, nil
				}
				err := ctx.Set("project_lic", in)
				return true, err
			},
		},

		// Step 7: Create Execution
		{
			LabelText: "Create Project",
			DefaultFn: func(ctx *context.Context) string { return "Press Enter to Create" },
			OnInputFn: func(in string, ctx *context.Context) (bool, error) {
				name := ctx.Value("project_name")
				dir := ctx.Value("project_dir")
				owner := ctx.Value("project_owner")
				desc := ctx.Value("project_desc")
				vis := ctx.Value("project_vis")
				lic := ctx.Value("project_lic")

				opts := NewProjectOptions{
					Name:        name,
					Directory:   dir,
					Owner:       owner,
					Description: desc,
					Visibility:  vis,
					License:     lic,
					LocalOnly:   gn.github == nil, // Skip remote if no GitHub handler
				}

				gn.log("[...", "Creating project")
				summary, err := gn.Create(opts)
				if err != nil {
					gn.log("...]", "Error: "+err.Error())
					return false, err
				}

				gn.log("...]", summary)
				err = ctx.Set("creation_summary", summary)
				return true, err
			},
		},
	}
}
