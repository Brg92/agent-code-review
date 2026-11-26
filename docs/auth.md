# `auth`

Stores, shows the source of, and removes the token the other commands use. The exit codes are in [exit-codes.md](exit-codes.md).

```bash
agent-code-review auth login [--host <host>]
agent-code-review auth status [--host <host>]
agent-code-review auth logout [--host <host>]
```

The token is read from `ACR_GITHUB_TOKEN` first (for CI), then from the git credential helpers under the username `acr-api`, so the credentials the user has for the same host are never read or overwritten. It is never printed, logged or written to disk by the tool, and never taken from the command line: `login` reads it from stdin, hidden at a terminal.

## `auth login`

1. It checks the credential helpers before asking for the token: none, or `store`, which keeps the token in plain text, ends with exit 10 and says what to configure.
2. It reads the token and, before changing anything, reads the token `acr-api` has now with `git credential fill`, in memory only. If git or a helper fails there, the command stops with nothing changed.
3. It gives the new token to git with `git credential approve`.
4. It reads it back with `git credential fill` and compares it, in memory, with the one it approved. Some helpers accept `approve` and keep nothing: the helper of the GitHub CLI (`!gh auth git-credential`, configured by `gh auth setup-git`) does, and it answers every request for github.com with the token of its own account.
5. If nothing comes back, or another token or the token of another account comes back, no helper kept the new token. The entry is rejected, which removes it from every helper, **the previous token included**. So if there was a previous token, it is approved again and read back to make sure, and the message says "your previous token was kept"; if it could not be put back, the message says so, and you have to log in again. With no previous token, nothing stays stored. The command ends with exit 10 in every case.

The message says what happened, never quotes a token, and says what to install and configure for the operating system:

| System | Install | Helper |
| --- | --- | --- |
| Linux | A Secret Service helper, `git-credential-libsecret` (a package of the distribution, or built from `contrib/credential/libsecret` of git), or Git Credential Manager | `libsecret`, or `manager` |
| macOS | Nothing: `osxkeychain` comes with the git of Xcode and of Homebrew | `osxkeychain` |
| Windows | Git Credential Manager, which Git for Windows installs unless it was left out | `manager` |

The commands give `acr-api` its own helper. The empty value first resets the helpers that apply to it, such as the one of the GitHub CLI; the section names `acr-api`, so your own `git push` and `git fetch` keep the helper they have:

```bash
git config --global --add credential.https://acr-api@github.com.helper ""
git config --global --add credential.https://acr-api@github.com.helper libsecret
```

If your shell drops the empty argument (Windows PowerShell 5.1 may), run `git config --global --edit` and write `helper =` as the first line of the section `[credential "https://acr-api@github.com"]`. Then run `auth login` again.

### `git-credential-libsecret` on Debian, Ubuntu and Pop!_OS

First check whether it is there: `ls /usr/lib/git-core/git-credential-libsecret` or `command -v git-credential-libsecret`. If it is, `libsecret` works as it is. Debian, Ubuntu and Pop!_OS 22.04 do not package it: the `git` package ships only its source, in `/usr/share/doc/git/contrib/credential/libsecret` (a Makefile and one C file). Build it in a copy and put it on the `PATH`, where git finds `git-credential-libsecret` for the helper `libsecret`:

```bash
sudo apt install build-essential pkg-config libsecret-1-dev
cp -r /usr/share/doc/git/contrib/credential/libsecret ~/libsecret-build
make -C ~/libsecret-build
sudo install -m 0755 ~/libsecret-build/git-credential-libsecret /usr/local/bin/
```

It needs a Secret Service in your session, such as GNOME Keyring, which GNOME and Pop!_OS run by default; without one it fails when asked for the token. Then run the two `git config` commands of the message and `auth login` again.

## `auth status` and `auth logout`

`status` says whether a token is available and where it comes from, never its value. `logout` removes the `acr-api` entry from the helpers, and does not touch `ACR_GITHUB_TOKEN`.

## `doctor`

[`doctor`](doctor.md) checks the storage before you log in. When `git credential fill` for `acr-api` answers with a token of another account, or the helper of the GitHub CLI is configured and no helper that keeps a token comes before it, it adds the warning `credential helper for github.com`, with the same commands, and the exit code does not change. A recent `gh` answers nothing when the username is not its own, which is the second case; a helper that answers for any username is the first.
