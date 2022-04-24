package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

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

func build(target target) error {
	var binaryPath = fmt.Sprintf("./builds/HIDI-%s", target.String())

	var envVars = []string{
		fmt.Sprintf("GOOS=%s", target.goos),
		fmt.Sprintf("GOARCH=%s", target.goarch),
	}
	if target.goarm != "" {
		envVars = append(envVars, fmt.Sprintf("GOARM=%s", target.goarm))
	}

	var targetFiles []string
	files, err := os.ReadDir("cmd")
	if err != nil {
		panic(err)
	}
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		if strings.HasSuffix(strings.ToLower(f.Name()), ".go") {
			targetFiles = append(targetFiles, fmt.Sprintf("cmd/%s", f.Name()))
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
		fmt.Printf("build failed: %s (%v)\n", binaryPath, err)
		fmt.Println("--- stdout ---")
		fmt.Println(stdout.String())
		fmt.Println("--- stderr ---")
		fmt.Println(stderr.String())
	} else {
		fmt.Printf("build succesful!: %s\n", binaryPath)
	}
	return err
}

var targets = []target{
	{goos: "linux", goarch: "arm", goarm: "5"},
	{goos: "linux", goarch: "arm", goarm: "6"},
	{goos: "linux", goarch: "arm", goarm: "7"},
	{goos: "linux", goarch: "arm64"}, // ARMv8,
	{goos: "linux", goarch: "386"},
	{goos: "linux", goarch: "amd64"},
}

func main() {
	var list bool
	var selection int

	flag.BoolVar(&list, "list", false, "list all available platforms")
	flag.IntVar(&selection, "select", -1, fmt.Sprintf("select specific platrofm (0-%d)", len(targets)-1))
	flag.Parse()

	if list {
		for i, target := range targets {
			fmt.Printf("%d: %s\n", i, target.String())
		}
		os.Exit(0)
	}

	if selection >= 0 {
		if selection > len(targets)-1 {
			fmt.Printf("selection out of range: %d\n", selection)
			os.Exit(1)
		}
		err := build(targets[selection])
		if err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}

	var results = make(chan error)
	var ok bool

	wgResults := sync.WaitGroup{}
	wgResults.Add(1)
	go func() {
		defer wgResults.Done()
		for err := range results {
			if err != nil {
				return
			}
		}
		ok = true
	}()

	wgBuild := sync.WaitGroup{}
	for _, t := range targets {
		wgBuild.Add(1)
		go func(target target) {
			defer wgBuild.Done()
			results <- build(target)
		}(t)
	}
	wgBuild.Wait()
	close(results)
	wgResults.Wait()

	if !ok {
		os.Exit(1)
	}
	os.Exit(0)
}
