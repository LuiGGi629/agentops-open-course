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
		output := set.String("output", filepath.Join(root, ".agents/tmp/course-completion.json"), "manifest output path")
		_ = set.Parse(os.Args[2:])
		if err := courseevidence.Create(context.Background(), config, *output); err != nil {
			fail(err)
		}
		fmt.Println("course evidence: wrote", *output)
	case "verify":
		set := flag.NewFlagSet("verify", flag.ExitOnError)
		_ = set.Parse(os.Args[2:])
		if set.NArg() != 1 {
			fail(errors.New("verify expects one manifest path"))
		}
		revision, err := courseevidence.Verify(context.Background(), config, set.Arg(0))
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

func usage() {
	fmt.Fprintln(os.Stderr, "usage: course-evidence create [--output path] | verify <manifest>")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "course evidence:", err)
	os.Exit(1)
}
