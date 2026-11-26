package cli

import (
	"strings"

	"github.com/Brg92/agent-code-review/internal/auth"
)

// helperSetupAdvice says what to install and configure, on the operating
// system goos names, so that git keeps the token of host. The commands give
// acr-api its own helper: a helper that ignores store, such as the one of
// the GitHub CLI, stays in charge of the user's own git commands.
func helperSetupAdvice(goos, host string) string {
	helper := auth.SuggestedHelper(goos)
	var install string
	switch goos {
	case "darwin":
		install = "to do (macOS): use the helper `osxkeychain`, which comes with the git of Xcode and of Homebrew."
	case "windows":
		install = "to do (Windows): use Git Credential Manager, `manager`, which Git for Windows installs unless it was left out: install it again with that option."
	default:
		install = "to do (Linux): install a Secret Service helper, `git-credential-libsecret` (a package of your distribution, or built from contrib/credential/libsecret of git), " +
			"or Git Credential Manager and use `manager` in place of `libsecret` below."
	}
	var b strings.Builder
	b.WriteString(install)
	b.WriteString(" Then give acr-api its own helper, which leaves the helpers of your own git commands alone:")
	for _, c := range auth.SetupCommands(host, helper) {
		b.WriteString("\n  " + c)
	}
	b.WriteString("\nthen run " + loginHint(host) + " again.")
	return b.String()
}
