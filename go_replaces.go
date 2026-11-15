package devflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"webtyp.com/command"
)

// SetKeepReplaces lets a publish that creates a tag go ahead although go.mod still replaces a module that is not
// published (gopush --keep-replaces). Replaces of published modules are always reconciled.
func (g *Go) SetKeepReplaces(keep bool) { g.keepReplaces = keep }

// reconcileOwnReplaces makes this module's own go.mod publishable before a tag is created.
//
// A module whose go.mod keeps a local replace of another ecosystem module would be tagged in a state that only
// builds on this machine, and its requires would still name the versions from before the change that needs them.
// The cascade already drops a dependent's replace when the dependency is published; this covers the module being
// published itself, which the cascade skips whenever it has uncommitted work.
//
//   - the replaced module is published (clean tree, HEAD tagged, tag pushed): the replace is dropped and the module is
//     required at that tag, then go mod tidy rewrites go.sum;
//   - it is not published: the publish is refused naming it, unless KeepReplaces says to go ahead;
//   - a replace to this module's own directory ("=> ./", a subpackage's go.mod) is not another module's: it stays.
func (g *Go) reconcileOwnReplaces() error {
	gomod := &GoModHandler{}
	gomod.SetRootDir(g.rootDir)
	entries, err := gomod.GetReplacePaths()
	if err != nil {
		return fmt.Errorf("go.mod replaces could not be read: %w", err)
	}
	root, err := filepath.Abs(g.rootDir)
	if err != nil {
		return err
	}

	type pin struct{ module, tag string }
	var pins []pin
	var unpublished []string
	for _, e := range entries {
		if e.LocalPath == root || strings.HasPrefix(e.LocalPath, root+string(filepath.Separator)) {
			continue
		}
		tag, why := publishedTag(e.LocalPath)
		if why != "" {
			unpublished = append(unpublished, e.ModulePath+" ("+why+")")
			continue
		}
		pins = append(pins, pin{e.ModulePath, tag})
	}

	if len(unpublished) > 0 && !g.keepReplaces {
		return fmt.Errorf("go.mod replaces modules that are not published: %s — publish them first, or pass --keep-replaces to tag knowing the go.mod only builds here",
			strings.Join(unpublished, ", "))
	}

	for _, p := range pins {
		if _, err := command.RunInDir(g.rootDir, "go", "mod", "edit", "-dropreplace="+p.module, "-require="+p.module+"@"+p.tag); err != nil {
			return fmt.Errorf("go.mod could not pin %s at %s: %w", p.module, p.tag, err)
		}
		g.log("go.mod:", p.module, "required at", p.tag, "(local replace dropped)")
	}
	if len(pins) > 0 {
		// GOPROXY=direct: a tag created minutes ago is not in the module proxy yet.
		cmd := command.Exec("go", "mod", "tidy")
		cmd.Dir = g.rootDir
		cmd.Env = append(os.Environ(), "GOPROXY=direct", "GOFLAGS=-mod=mod")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("go mod tidy failed after pinning replaced modules: %w\n%s", err, out)
		}
	}
	return nil
}

// publishedTag reports the tag the module in dir is published at: its tree is clean, HEAD carries a tag and that
// tag exists on origin. why is empty when it is; otherwise it says what is missing.
func publishedTag(dir string) (tag, why string) {
	git := func(args ...string) (string, error) {
		cmd := command.Exec("git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	if status, err := git("status", "--porcelain"); err != nil || status != "" {
		return "", "uncommitted work"
	}
	tag, err := git("describe", "--tags", "--exact-match", "HEAD")
	if err != nil || tag == "" {
		return "", "HEAD has no tag"
	}
	if remote, err := git("ls-remote", "--tags", "origin", tag); err != nil || remote == "" {
		return "", "tag " + tag + " is not pushed"
	}
	return tag, ""
}
