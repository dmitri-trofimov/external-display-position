# External Display Position (`edp`)

Ultra-fast native Windows display-position manager written in Go.

Detects which physical port/GPU target (`TargetID`) your external monitor is connected to via Windows `QueryDisplayConfig`, and restores your saved `(x, y)` monitor layout immediately on `WM_DISPLAYCHANGE` and `WM_POWERBROADCAST`.

* **Zero runtime dependencies** (pure Go + Win32 `user32.dll` / `advapi32.dll`)
* **0% idle CPU** (sleeps in a native Win32 `GetMessageW` loop)
* **Git-safe config:** personal monitor IDs and coordinates live in `%LOCALAPPDATA%\ExternalDisplayPosition\config.json`

---

## Quick Install (after `git clone`)

Run the PowerShell installer from the cloned repo:

```powershell
.\install.ps1
```

Or install everything non-interactively (auto-installs Go via `winget` if missing, builds `edp.exe` & `edpw.exe`, enables Windows startup, starts the watcher, and creates the **Save Display Layout** Desktop shortcut with icon `assets\app.ico`):

```powershell
.\install.ps1 -y
```

### Installer flags
* `-y` / `-Yes` — non-interactive: auto-install Go if missing, enable Windows startup, start `edpw.exe`, create Desktop shortcut
* `-InstallGo` — auto-install Go via `winget` if missing without prompting
* `-EnableStartup` / `-DisableStartup` — explicitly enable or skip Windows startup
* `-SkipShortcut` — skip creating the `Save Display Layout` Desktop shortcut

> **Using an AI coding agent to install from GitHub?** See [AGENTS.md](AGENTS.md) for agent instructions.

---

## Manual Build

```powershell
go build -ldflags="-s -w" -o edp.exe ./cmd/edp
go build -ldflags="-s -w -H=windowsgui" -o edpw.exe ./cmd/edpw
```

* `edp.exe` — console CLI (`status`, `save`, `list`, `apply`, `delete`, `watch`, `autostart`)
* `edpw.exe` — silent background watcher (no console window)

---

## Usage

1. **Save your first port layout** (e.g. monitor plugged into left port):
   Arrange displays in **Settings → System → Display**, then double-click **Save Display Layout** on your Desktop (or run `.\edp.exe save left-port`).

2. **Save your second port layout** (e.g. monitor plugged into right port):
   Plug into the other port, arrange displays how you want, then double-click **Save Display Layout** again (or run `.\edp.exe save right-port`).

3. **Test in foreground (optional):**
   ```powershell
   .\edp.exe watch
   ```

4. **Manage silent startup with Windows:**
   ```powershell
   .\edp.exe autostart enable
   .\edp.exe autostart status
   .\edp.exe autostart disable
   ```

---

## Commands

* `edp.exe status` — show active monitors, port UIDs, coordinates, and matching profile
* `edp.exe save [name]` — save/update layout for the active port topology (`--pause` / `-p` waits for Enter before closing)
* `edp.exe list` — list saved profiles
* `edp.exe apply` — apply the saved profile for the current topology now
* `edp.exe delete <name|signature>` — delete a saved profile
* `edp.exe watch` — run watcher in foreground console
* `edp.exe autostart <enable|disable|status>` — manage `HKCU\...\Run` autostart for `edpw.exe`
