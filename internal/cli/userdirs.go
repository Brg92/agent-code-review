package cli

import (
	"errors"
	"path/filepath"
)

// userDirs finds the directories of the user from an environment, as
// os.UserCacheDir, os.UserConfigDir and os.UserHomeDir do from the one of
// the process. Plan 9 is not a target and is not handled.
type userDirs struct {
	getenv func(string) string
	// goos is the operating system whose conventions apply.
	goos string
}

func (u userDirs) cache() (string, error) {
	switch u.goos {
	case "windows":
		return u.required("LocalAppData", "%LocalAppData% is not defined")
	case "darwin", "ios":
		home, err := u.home()
		if err != nil {
			return "", errors.New("neither $XDG_CACHE_HOME nor $HOME are defined")
		}
		return filepath.Join(home, "Library", "Caches"), nil
	}
	return u.xdg("XDG_CACHE_HOME", ".cache", "neither $XDG_CACHE_HOME nor $HOME are defined", "path in $XDG_CACHE_HOME is relative")
}

func (u userDirs) config() (string, error) {
	switch u.goos {
	case "windows":
		return u.required("AppData", "%AppData% is not defined")
	case "darwin", "ios":
		home, err := u.home()
		if err != nil {
			return "", errors.New("$HOME is not defined")
		}
		return filepath.Join(home, "Library", "Application Support"), nil
	}
	return u.xdg("XDG_CONFIG_HOME", ".config", "neither $XDG_CONFIG_HOME nor $HOME are defined", "path in $XDG_CONFIG_HOME is relative")
}

func (u userDirs) home() (string, error) {
	if u.goos == "windows" {
		return u.required("userprofile", "%userprofile% is not defined")
	}
	return u.required("HOME", "$HOME is not defined")
}

func (u userDirs) required(name, undefined string) (string, error) {
	if v := u.getenv(name); v != "" {
		return v, nil
	}
	return "", errors.New(undefined)
}

// xdg returns the directory in the variable name, which must be absolute, or
// fallback in the home directory.
func (u userDirs) xdg(name, fallback, undefined, relative string) (string, error) {
	if v := u.getenv(name); v != "" {
		if !filepath.IsAbs(v) {
			return "", errors.New(relative)
		}
		return v, nil
	}
	home := u.getenv("HOME")
	if home == "" {
		return "", errors.New(undefined)
	}
	return filepath.Join(home, fallback), nil
}
