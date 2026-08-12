package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MLOps-Courses/agentops-open-course-go/tools/internal/courseevidence"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	root, err := repositoryRoot()
	if err != nil {
		fail(err)
	}
	config := courseevidence.DefaultConfig(root)
	switch os.Args[1] {
	case "create":
		set := flag.NewFlagSet("create", flag.ExitOnError)
		output := set.String("output", defaultManifest(root), "manifest output path")
		_ = set.Parse(os.Args[2:])
		if err := courseevidence.Create(context.Background(), config, *output); err != nil {
			fail(err)
		}
		fmt.Println("course evidence: wrote", *output)
		fmt.Println("course evidence: wrote", courseevidence.SummaryPath(*output))
	case "verify":
		set := flag.NewFlagSet("verify", flag.ExitOnError)
		// The same default as `create`, so the documented pair works on a clean
		// tree. A positional argument still overrides it, which is how a reviewer
		// verifies a manifest someone handed them.
		manifest := set.String("manifest", defaultManifest(root), "manifest path to verify")
		_ = set.Parse(os.Args[2:])
		if set.NArg() > 1 {
			fail(errors.New("verify accepts at most one manifest path"))
		}
		if set.NArg() == 1 {
			*manifest = set.Arg(0)
		}
		if *manifest == "" {
			fail(errors.New("verify needs a manifest path"))
		}
		revision, err := courseevidence.Verify(context.Background(), config, *manifest)
		if err != nil {
			fail(err)
		}
		fmt.Println("course evidence: verified", revision)
	default:
		usage()
		os.Exit(2)
	}
}

func repositoryRoot() (string, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("reading working directory: %w", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "mise.toml")); err == nil {
			if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err == nil {
				return root, nil
			}
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", errors.New("could not find the repository root")
		}
		root = parent
	}
}

// defaultManifest is gitignored on purpose: the manifest attests to a checkout,
// not to a commit, so committing one would be attesting to the wrong thing.
func defaultManifest(root string) string {
	return filepath.Join(root, ".agents/tmp/course-completion.json")
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: course-evidence create [--output path] | verify [--manifest path | <manifest>]")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "course evidence:", err)
	os.Exit(1)
}
