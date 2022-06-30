package main

import (
	"bytes"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var availableTargets = []target{
	{goos: "linux", goarch: "arm", goarm: "5"},
	{goos: "linux", goarch: "arm", goarm: "6"},
	{goos: "linux", goarch: "arm", goarm: "7"},
	{goos: "linux", goarch: "arm64"}, // ARMv8,
	{goos: "linux", goarch: "386"},
	{goos: "linux", goarch: "amd64"},
}

type target struct {
	goos   string
	goarch string
	goarm  string
}

func (t *target) String() string {
	if t.goarm != "" {
		return fmt.Sprintf("%s-%s-v%s", t.goos, t.goarch, t.goarm)
	} else {
		return fmt.Sprintf("%s-%s", t.goos, t.goarch)
	}
}

type buildError struct {
	target         target
	project, base  string
	stdout, stderr string
}

func build(target target, project, basename string, buildErrors chan<- buildError) error {
	var binaryPath = fmt.Sprintf("./builds/%s-%s", basename, target.String())

	var envVars = []string{
		fmt.Sprintf("GOOS=%s", target.goos),
		fmt.Sprintf("GOARCH=%s", target.goarch),
	}
	if target.goarm != "" {
		envVars = append(envVars, fmt.Sprintf("GOARM=%s", target.goarm))
	}

	var targetFiles []string
	files, err := os.ReadDir(project)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(f.Name()), ".go") {
			targetFiles = append(targetFiles, fmt.Sprintf("%s/%s", project, f.Name()))
		}
	}

	params := []string{"build", "-o", binaryPath}
	params = append(params, targetFiles...)

	cmd := exec.Command("go", params...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, envVars...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()

	if err != nil {
		buildErrors <- buildError{
			target:  target,
			project: project,
			base:    basename,
			stdout:  stdout.String(),
			stderr:  stderr.String(),
		}
	}
	return err
}

func main() {
	var list bool
	var selection, project, basename string

	flag.BoolVar(&list, "list", false, "list all available target platforms")
	flag.StringVar(&selection, "platforms", "all", fmt.Sprintf("comma-separated target platrofm list"))
	flag.StringVar(&project, "project", "cmd/hidi/", fmt.Sprintf("choose project directory"))
	flag.StringVar(&basename, "base", "HIDI", fmt.Sprintf("base filename for output binaries"))
	flag.Parse()

	log.SetFlags(log.Ltime)

	if list {
		for _, target := range availableTargets {
			fmt.Printf("%s\n", target.String())
		}
		os.Exit(0)
	}
	var selectedTargets []target

	if selection != "all" {
		rawTargets := strings.Split(selection, ",")
		for _, rt := range rawTargets {
			var found = false
			for _, t := range availableTargets {
				if t.String() == rt {
					selectedTargets = append(selectedTargets, t)
					found = true
					break
				}
			}
			if !found {
				log.Printf("target not found: %s", rt)
				os.Exit(1)
			}
		}
	} else {
		selectedTargets = append(selectedTargets, availableTargets...)
	}

	var selectedTargetsString []string
	for _, t := range selectedTargets {
		selectedTargetsString = append(selectedTargetsString, t.String())
	}
	log.Printf("selected targets: %s", strings.Join(selectedTargetsString, ", "))

	var results = make(chan error)
	var ok = true

	wgResults := sync.WaitGroup{}
	wgResults.Add(1)
	go func() {
		defer wgResults.Done()
		for err := range results {
			if err != nil {
				log.Printf("%s", err)
				ok = false
			}
		}
	}()

	var buildErrors = make(chan buildError, len(selectedTargets)) // "smart" buffering

	wgBuild := sync.WaitGroup{}
	log.Printf("engaging parallel building for %d targets\n", len(selectedTargets))
	for _, t := range selectedTargets {
		wgBuild.Add(1)
		go func(target target) {
			defer wgBuild.Done()
			log.Printf("building target %s          %s", project, target.String())
			err := build(target, project, basename, buildErrors)
			results <- err
			if err != nil {
				log.Printf("building target %s failed:  %s", project, target.String())
			} else {
				log.Printf("building target %s success: %s", project, target.String())
			}
		}(t)
	}

	wgBuild.Wait()
	close(results)
	wgResults.Wait()

	wgResults.Add(1)
	go func() {
		defer wgResults.Done()
		for err := range buildErrors {
			fmt.Printf("\n>>> Failed build: project: %s, base: %s, target: %s\n", err.project, err.base, err.target.String())
			if err.stdout != "" {
				fmt.Printf("======== STDOUT ========\n")
				fmt.Printf("%s", err.stdout)
				fmt.Printf("========================\n")
			}
			if err.stderr != "" {
				fmt.Printf("======== STDERR ========\n")
				fmt.Printf("%s", err.stderr)
				fmt.Printf("========================\n")
			}
		}
	}()

	close(buildErrors)
	wgResults.Wait()

	if !ok {
		os.Exit(1)
	}
	os.Exit(0)
}
