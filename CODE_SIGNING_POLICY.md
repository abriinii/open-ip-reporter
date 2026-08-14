# Code signing policy

This document exists because [SignPath Foundation](https://signpath.org/terms.html)
requires a published policy from any project it issues a certificate to. It
describes who can change this software, who can authorise a signed release, and
what the software does with your data.

## Project

- **Name:** OpenIPReporter
- **Repository:** https://github.com/abriinii/open-ip-reporter
- **License:** [MIT](LICENSE)
- **Purpose:** Records the IP and MAC address that Bitcoin mining hardware
  broadcasts when its IP-report button is pressed, together with the physical
  rack position of the machine, and exports the result as CSV. It replaces the
  vendor-supplied IP Reporter tools for that task.

## Team and roles

This is a single-maintainer project. All three roles are currently held by the
same person, which is disclosed here rather than obscured:

| Role | Who | Responsibility |
|---|---|---|
| Author | Alex Brinager ([@abriinii](https://github.com/abriinii)) | Writes and commits source code |
| Reviewer | Alex Brinager ([@abriinii](https://github.com/abriinii)) | Reviews any external contribution before merge |
| Approver | Alex Brinager ([@abriinii](https://github.com/abriinii)) | Approves signing requests for releases |

External contributions are accepted only through pull requests, and are reviewed
before merge. Nobody outside the team above can cause a signed artifact to be
produced.

Multi-factor authentication is required on the GitHub account and on the
SignPath account used for release approval.

## How releases are built

Releases are produced only by GitHub Actions, from a tagged commit in the
repository above, using the workflow at
[`.github/workflows/release.yml`](.github/workflows/release.yml).

- The Windows and macOS applications are compiled on GitHub-hosted runners.
- No artifact is built on a maintainer's machine and uploaded by hand.
- The build takes its input entirely from the tagged commit; nothing is
  downloaded into the artifact at build time beyond declared Go module
  dependencies, which are pinned by `go.mod` and `go.sum`.
- `SHA256SUMS.txt` is published alongside every release.

Signed binaries carry a consistent product name (`OpenIPReporter`) and a
version string set from the release tag.

## Third-party components

All dependencies are open source under permissive licenses. The application
uses the [Wails](https://wails.io) framework (MIT) and its transitive Go module
dependencies; the command-line tool uses only the Go standard library. No
proprietary or closed-source component is included.

## Privacy

**This software collects no data about you and sends none anywhere.**

It makes exactly one outbound request, and it is worth describing precisely:

- **On startup it asks GitHub whether a newer release exists**, by fetching
  `https://api.github.com/repos/abriinii/open-ip-reporter/releases/latest`.
  Nothing is sent but the request itself and a `User-Agent` naming the program
  and its version. GitHub necessarily sees the requesting IP address, as it
  would for any web request.
- **Nothing is downloaded or installed.** If a newer version exists you are
  shown its release notes and a button that opens the download page in your
  browser. The program never replaces itself.
- **It can be turned off**, from the checkbox on that notice, and the choice is
  remembered in `settings.json`. Turned off, the program makes no outbound
  network requests at all.
- The check fails silently when there is no route to the internet, which is the
  normal case on a miner network.

Beyond that: no telemetry, no analytics, no crash reporting, no account,
licence key, or registration.

Everything the program records — miner IP addresses, MAC addresses, notes and
rack positions — is written only to files on the machine it runs on, in
`sessions/` and wherever you choose to export a CSV. **None of it is ever
transmitted anywhere**, including in the update check. Nothing is written
outside the working directory and the file you pick in the export dialog.

## Uninstalling

The application is a single executable and installs nothing. Delete the file.
To remove its data as well, delete the `sessions/` folder next to it and any
CSV files you exported.

## Reporting a problem

Security or integrity concerns:
[open an issue](https://github.com/abriinii/open-ip-reporter/issues).

---

<!--
Once SignPath Foundation approves the application, their terms require this
attribution to appear here and on the download page:

  Free code signing provided by [SignPath.io](https://signpath.io), certificate
  by [SignPath Foundation](https://signpath.org).

It is deliberately left commented out until approval, because publishing it
before then would claim a relationship that does not exist yet.
-->
