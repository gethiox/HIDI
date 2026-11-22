// Package update asks GitHub whether a newer HIDI has been released. It
// only asks: nothing is downloaded or installed. HIDI does so only where
// the user turned it on.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Project is where HIDI's releases are, for people, and Latest where the
// newest one is described, for programs.
const (
	Project = "https://github.com/gethiox/HIDI"
	Latest  = "https://api.github.com/repos/gethiox/HIDI/releases/latest"
)

// Result is what a check found out.
type Result struct {
	// Current is the version that asked and Newest the newest release.
	Current, Newest string
	// Newer says that the newest release is newer than this HIDI.
	Newer bool
	// Page is where the release can be read about and fetched.
	Page string
}

// Check asks url, which is [Latest] outside of tests, for the newest
// release and compares it with current, the version of this HIDI. A HIDI
// that was not built as a release has no version to compare; for it the
// newest release is reported and never called newer.
func Check(ctx context.Context, client *http.Client, url, current string) (Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "HIDI/"+current)
	resp, err := client.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var release struct {
		Tag  string `json:"tag_name"`
		Page string `json:"html_url"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return Result{}, fmt.Errorf("the answer of GitHub could not be read: %w", err)
	}
	newest := strings.TrimPrefix(release.Tag, "v")
	if _, ok := parse(newest); !ok {
		return Result{}, fmt.Errorf("the newest release is called %q, which is not a version", release.Tag)
	}
	// Only a page of the project is passed on to be opened.
	page := release.Page
	if !strings.HasPrefix(page, Project+"/") {
		page = Project + "/releases"
	}
	return Result{Current: current, Newest: newest, Newer: Newer(newest, current), Page: page}, nil
}

// version is a version as releases have them: three numbers, and maybe
// what comes after a hyphen, such as beta.1.
type version struct {
	numbers [3]int
	pre     []string
}

func parse(s string) (version, bool) {
	var v version
	s = strings.TrimPrefix(s, "v")
	s, _, _ = strings.Cut(s, "+") // how it was built does not order versions
	core, pre, hasPre := strings.Cut(s, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return v, false
		}
		v.numbers[i] = n
	}
	if hasPre {
		if pre == "" {
			return v, false
		}
		v.pre = strings.Split(pre, ".")
	}
	return v, true
}

// Newer reports whether version a is newer than version b, the way
// versions are ordered: by their numbers, and a version with a suffix
// such as -beta.1 comes before the one without. Something that is not a
// version is not newer than anything, and nothing is newer than it.
func Newer(a, b string) bool {
	va, ok := parse(a)
	if !ok {
		return false
	}
	vb, ok := parse(b)
	if !ok {
		return false
	}
	for i := range va.numbers {
		if va.numbers[i] != vb.numbers[i] {
			return va.numbers[i] > vb.numbers[i]
		}
	}
	switch {
	case len(va.pre) == 0 || len(vb.pre) == 0:
		return len(va.pre) == 0 && len(vb.pre) > 0
	}
	for i := 0; i < len(va.pre) && i < len(vb.pre); i++ {
		x, y := va.pre[i], vb.pre[i]
		if x == y {
			continue
		}
		nx, errX := strconv.Atoi(x)
		ny, errY := strconv.Atoi(y)
		switch {
		case errX == nil && errY == nil:
			return nx > ny
		case errX == nil:
			return false // numbers come before words
		case errY == nil:
			return true
		}
		return x > y
	}
	return len(va.pre) > len(vb.pre)
}

// Known reports whether s is a version that can be compared: that of a
// release, and not of a HIDI built from source.
func Known(s string) bool {
	_, ok := parse(s)
	return ok
}

// Interval is how long a check is good for: a window that stays open asks
// again after this long.
const Interval = 24 * time.Hour
