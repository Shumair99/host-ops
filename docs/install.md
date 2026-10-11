# Install and start

## Install the latest release

**Not published yet:** the commands below become available once the GitHub repository is public, the installer scripts are on `main`, and the first downloadable release has been published.

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/Shumair99/host-ops/main/scripts/install-latest.ps1 | iex
hostops start
```

Linux / macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/Shumair99/host-ops/main/scripts/install-latest.sh | bash
export PATH="$HOME/.local/bin:$PATH"
hostops start
```

These commands require a published stable GitHub release. The bootstrap installer detects your workstation platform/architecture, resolves the latest stable release once, downloads the matching archive and `SHA256SUMS`, and verifies the download before extracting and running the bundle installer. It needs no Go or local Docker. Windows installation enables the command in the current PowerShell session; on Unix the explicit PATH line does the same. Re-run the install command to update.

The bootstrap scripts are ordinary, readable source files in `scripts/`. Windows runs the extracted installer with a process-only execution-policy override; it does not change your permanent policy. Custom locations can be supplied to a downloaded script using the same installer options described below.

## Manual installation

Use the bundle matching your **computer**, not your VPS. Windows amd64/arm64 bundles are ZIPs; Linux and macOS amd64/arm64 bundles are tar.gz archives. Every bundle includes both Linux collector architectures for remote discovery and both optional VPS history agents. End users do not need Go or a source checkout.

On Windows, extract the complete ZIP and double-click **Install.cmd**. The installer writes `%LOCALAPPDATA%\Programs\HostOps`, adds its `bin` folder to your user PATH, and requires no administrator privileges. Open a new terminal:

```text
hostops start
```

On Linux/macOS, extract the archive and run `bash install.sh` in its folder. Commands are linked into `~/.local/bin`, with a PATH entry in your Bash or Zsh startup files. Open a new terminal and run `hostops start`. Linux installs under `${XDG_DATA_HOME:-$HOME/.local/share}/hostops`; macOS uses `~/Library/Application Support/HostOps`. The installer supports `--prefix DIRECTORY --bin DIRECTORY --no-path` for managed environments. Windows supports `-InstallDirectory DIRECTORY -SkipPath`.

## First launch

Enter the server's SSH target (`user@address`, or an existing alias), an optional private-key path, and port. OpenSSH and an already authorized SSH key are required. Private keys are referenced by path, never copied into settings. An encrypted key needs an available SSH agent. OpenSSH performs first-connection fingerprint confirmation; changed fingerprints are rejected. Password-only automated SSH login is not supported.

HostOps verifies the connection, runs automatic discovery, and shows counts and unavailable readers. Supported runtimes and proxies produce one inventory; restricted permissions, unsupported software and opaque dynamic routes stay visible as coverage gaps. Discovery does not alter application/proxy configuration. Optional always-on history requires explicit consent and server privileges; pressing Enter keeps session-only history. Optional billing opens the existing dashboard form.

The dashboard opens with the discovered state. Check **Inventory** and **Sites**, then select **Export for AI** at the bottom of Views. Enter reviews the export and a second Enter confirms writing the Markdown. Provide that file to your chosen AI to review deployment improvements.

`hostops start` reuses the saved connection on later launches. Running `hostops` without arguments also starts this flow. Use `hostops start --demo` for an offline demonstration, and `hostops --version` to identify a build.

## Your data and updates

New settings go to `os.UserConfigDir()/HostOps/hostops.json`: `%APPDATA%\HostOps\hostops.json` on Windows, `$XDG_CONFIG_HOME/HostOps/hostops.json` or `~/.config/HostOps/hostops.json` on Linux, and `~/Library/Application Support/HostOps/hostops.json` on macOS. The file is written atomically with permissions limited to your account (and SYSTEM on Windows). History uses the private per-user cache; exports default to `~/HostOps/exports`.

Explicit `--config`, `HOSTOPS_CONFIG`, and existing `hostops.json` / `local/hostops.json` in the current project retain precedence. An installed user does not need to edit JSON. Use separate `--config` files to manage multiple VPSs.

To update, extract a new complete bundle and run its installer again. It verifies checksums and installs an immutable version alongside the previous one before switching the command launchers. Personal settings/history are outside the installation and remain intact. Integrity checks detect incomplete or damaged extraction; they are not a publisher signature.

To uninstall, run the installed `Uninstall-HostOps.ps1` on Windows or `bash /path/to/installation/uninstall.sh` on Unix. The managed app directory and its command entries are removed. Settings, history and exports are retained, as is an explicitly installed VPS history service; remove that separately with `hostops agent uninstall` before uninstalling the workstation app if desired.

## Building distributable packages

Contributors need Go from `go.mod`. Run `scripts/build-bundles.ps1` on Windows or `bash scripts/build-bundles.sh` on Unix. Set `-Version` / `--version` and `-Targets` / `--targets` to build selected platforms. Both wrappers call the same Go builder.

Each archive contains version/commit/build metadata in `bundle.json`, file checksums, installers, public documentation, examples, workstation executables and remote helper assets. The fixed public-file allowlist excludes local settings, private VPS audit reports, databases, SSH keys, build caches and repository metadata. `output/bundles/SHA256SUMS` contains the generated archive checksums.

The bundle workflow builds all six workstation targets, checks the code, and exercises both the bundle and latest-release installers before publishing. A manual workflow run produces downloadable Actions artifacts. Pushing a version tag such as `v1.0.0` publishes the tested archives and checksums as a GitHub release; tags with a prerelease suffix produce prereleases that the latest installer ignores. No release is created by an ordinary branch push. macOS bundles are built by cross-compilation; the workflow's install smoke tests run on Linux and Windows.
