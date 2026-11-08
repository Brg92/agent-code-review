package packs

import "fmt"

// Where a profile comes from.
const (
	ProfileFromConfig      = "config"
	ProfileFromCascade     = "cascade-dependency"
	ProfileFromPack        = "pack-detect"
	ProfileFromPackDefault = "pack-default"
	// ProfileUndetermined: no configuration and no active pack.
	ProfileUndetermined = "undetermined"
)

// libraryProfile is the profile of a repository another change request
// of the cascade depends on.
const libraryProfile = "library"

// ProfileInput is what decides a profile before the packs do.
type ProfileInput struct {
	// Configured is the profile of the repository configuration, if any.
	Configured string
	// DependentCR is the key of a change request of the cascade that
	// depends on this repository, if any.
	DependentCR string
}

// Profile is a resolved profile and why it was chosen.
type Profile struct {
	Name   string
	Reason string
	Detail string
}

// ResolveProfile decides the profile of a repository: its configuration
// first; then library, if another change request of the cascade depends
// on it; then the profile signs of the active packs, in order, the first
// match winning; then the default profile of the first active pack.
func ResolveProfile(active []*Pack, in ProfileInput, paths []string, repo Reader) (Profile, error) {
	switch {
	case in.Configured != "":
		return Profile{Name: in.Configured, Reason: ProfileFromConfig}, nil
	case in.DependentCR != "":
		return Profile{Name: libraryProfile, Reason: ProfileFromCascade, Detail: in.DependentCR}, nil
	}
	for _, p := range active {
		for _, s := range p.ProfileDetect {
			ok, err := s.matches(paths, repo)
			if err != nil {
				return Profile{}, fmt.Errorf("pack %s: profileDetect: %w", p.ID, err)
			}
			if ok {
				return Profile{Name: s.Profile, Reason: ProfileFromPack, Detail: p.ID + ": " + s.describe()}, nil
			}
		}
	}
	if len(active) == 0 {
		return Profile{Reason: ProfileUndetermined}, nil
	}
	return Profile{Name: active[0].DefaultProfile, Reason: ProfileFromPackDefault, Detail: active[0].ID}, nil
}

func (s Sign) describe() string {
	if s.Contains == "" {
		return s.Path.String()
	}
	return fmt.Sprintf("%s contains %q", s.Path, s.Contains)
}
