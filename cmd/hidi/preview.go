package main

import (
	"fmt"
	"sync"

	"github.com/gethiox/HIDI/internal/config"
)

// previews holds the profiles that are tried from the window before they
// are saved: text that takes the place of a profile file for as long as
// that file stays as it is on disk.
type previews struct {
	mu       sync.Mutex
	profiles []*config.Profile          // as loaded from disk
	tried    map[string]*config.Profile // by the file they stand in for
	// stood is the content of each file when its stand-in was set.
	stood map[string][32]byte
}

// set makes text stand in for a profile file, or ends that when text is
// empty, and returns the profiles to use. Text that is no profile, or a
// file that is none of the profiles in use, is refused.
func (p *previews) set(file, text string) ([]*config.Profile, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if text == "" {
		delete(p.tried, file)
		delete(p.stood, file)
		return p.effective(), nil
	}
	var on *config.Profile
	for _, loaded := range p.profiles {
		if loaded.Source == file {
			on = loaded
		}
	}
	if on == nil {
		return nil, fmt.Errorf("%s is not one of the profiles in use", file)
	}
	profile, err := config.Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	profile.Source = file
	if p.tried == nil {
		p.tried, p.stood = map[string]*config.Profile{}, map[string][32]byte{}
	}
	p.tried[file], p.stood[file] = profile, on.Digest
	return p.effective(), nil
}

// loaded takes in the profiles as they are on disk now and returns those
// to use. A stand-in ends when its file changed or is gone: what is on
// disk was saved, by the window or by other hands, and counts.
func (p *previews) loaded(profiles []*config.Profile) []*config.Profile {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.profiles = profiles
	still := map[string]bool{}
	for _, loaded := range profiles {
		if digest, tried := p.stood[loaded.Source]; tried && digest == loaded.Digest {
			still[loaded.Source] = true
		}
	}
	for file := range p.tried {
		if !still[file] {
			delete(p.tried, file)
			delete(p.stood, file)
		}
	}
	return p.effective()
}

// effective returns the loaded profiles with the stand-ins in the places
// of their files. The caller holds p.mu.
func (p *previews) effective() []*config.Profile {
	out := make([]*config.Profile, len(p.profiles))
	for i, loaded := range p.profiles {
		out[i] = loaded
		if tried, ok := p.tried[loaded.Source]; ok {
			out[i] = tried
		}
	}
	return out
}
