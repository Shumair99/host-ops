# Start here

## Windows

Extract the complete ZIP, then double-click **Install.cmd**. Installation is for your account and requires no administrator access. Open a new terminal and type:

```text
hostops start
```

## Linux and macOS

Extract the complete archive, open its folder in a terminal, and run `bash install.sh`. Open a new terminal and run `hostops start`.

## Onboarding

1. Enter your VPS SSH target (`user@address`, or an SSH alias), an optional private-key path, and the SSH port. An authorized SSH key and the OpenSSH client are required. HostOps uses your SSH agent/config; it never saves a password or copies private-key contents.
2. On a new SSH connection, OpenSSH can ask you to confirm the server fingerprint. Check it against your provider before accepting. Encrypted keys can be unlocked through your existing SSH agent. [OpenSSH explains host verification and agent key loading](https://man.openbsd.org/ssh_config).
3. HostOps detects supported runtimes, routes, website addresses, storage and service relationships. It reports source coverage and gaps. You do not need to enter each site or create a JSON file.
4. Choose whether to enable optional background history and optional billing. Background history installation needs an explicit Yes and server privileges. Both choices can be left off and configured later.
5. The dashboard opens. Check **Inventory** and **Sites** to review what was observed. Use **Export for AI** at the bottom of **Views**, then Enter to review and Enter again to confirm saving the Markdown.

Run `hostops start` again to reopen your saved connection from any folder. `hostops start --demo` tries the dashboard offline. `hostops --version` identifies the installed build. A terminal at least 60 columns by 16 rows is required; 120 by 36 is more comfortable.

Connection settings are stored in your operating system's user configuration folder (`HostOps/hostops.json`); history uses the user cache, and exports default to `~/HostOps/exports`. Existing `HOSTOPS_CONFIG`, explicit `--config FILE` and project-local settings remain supported. Installation and updates never bundle or replace your personal connection settings.

This package includes the matching Linux amd64 and arm64 discovery collectors and optional history agents. Keep the bundle together. Users do not need Go, Docker on their computer, or the source repository. The VPS must run supported Linux; unavailable readers and unsupported routing remain visible coverage gaps.

On Windows, remove the app by running the installed `Uninstall-HostOps.ps1` with PowerShell. On Linux/macOS, run the installed `uninstall.sh`. Uninstall removes the application and its command launchers; settings, history and exports are retained.
