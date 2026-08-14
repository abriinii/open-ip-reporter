# Publishing this repo

One-time setup, then you never think about it again. You already have the
`gh` command installed (version 2.95.0), which does most of it.

## 1. Sign in to GitHub from the terminal

```bash
gh auth login
```

It asks four questions. Answer them:

| Prompt | Answer |
|---|---|
| What account do you want to log into? | **GitHub.com** |
| What is your preferred protocol? | **HTTPS** |
| Authenticate Git with your GitHub credentials? | **Yes** |
| How would you like to authenticate? | **Login with a web browser** |

It then shows an **eight-character code** and waits. Press Enter, your browser
opens to `github.com/login/device`, paste the code, click **Continue**, then
**Authorize github**. Back in the terminal it will say `Logged in as ...`.

This stores a token on your machine. You will not be asked again.

## 2. Create the repo and push

From the project folder:

```bash
gh repo create open-ip-reporter --public --source=. --remote=origin --push
```

That creates the public repo under your account, connects this folder to it,
and pushes the code in one step. It prints the URL when it finishes.

## 3. Check that CI is alive

Open the repo on github.com and click the **Actions** tab. You should see a
run called **CI** with a green check. That is the build verifying the code
compiles for both macOS and Windows on every push.

If it is red, run:

```bash
gh run view --log-failed
```

## 4. Publishing a release

This is the only thing you do from now on. Pick a version number and tag it:

```bash
git tag v0.1.0 && git push origin v0.1.0
```

That is it. Within about a minute the **Actions** tab shows a **Release** run,
and when it goes green the [Releases page](../../releases) will have:

- `ipreporter-macos-arm64`
- `ipreporter-windows-x64.exe`
- `SHA256SUMS.txt`

Next release is `v0.2.0`, then `v0.3.0`, and so on. Bump the middle number for
new features, the last number for fixes.

### If a release goes wrong

Delete the tag and the release, fix, and tag again:

```bash
gh release delete v0.1.0 --yes
git push --delete origin v0.1.0
git tag -d v0.1.0
```

## 5. Day-to-day: saving changes

```bash
git add -A
git commit -m "short description of what changed"
git push
```

## What must never be committed

`.gitignore` already blocks these, but the rule matters more than the file:

- **Capture files** (`captures/`) — real MAC addresses from the site
- **Exports** (`exports/`) — the serial/MAC/position map itself
- **Sitemap CSVs** — the physical layout of the site
- Any `*.csv` at all, unless it is named `sample-*.csv` and has been sanitized

Since the repo is public, anything committed is public immediately, and
deleting it later does not remove it from git history. If something sensitive
does get pushed, say so rather than just deleting it — the history has to be
rewritten and the repo may need to be recreated.

To check what you are about to commit:

```bash
git status
```

## macOS Gatekeeper

Unsigned binaries are blocked on first run. The workaround is in the
[README](../README.md#macos): `chmod +x` then `xattr -d com.apple.quarantine`.

Removing that step permanently means an Apple Developer account ($99/year)
plus a notarization step added to the release workflow. Worth it only if you
start handing builds to other techs regularly.
