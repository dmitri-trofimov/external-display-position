# AI Agent Installation Guide

Follow these steps when a user asks you to install this repository (e.g., *"install External Display Position from GitHub"*).

> **Platform requirement:** Windows 10/11 (`amd64`).

---

## 1. Clone into a persistent directory
If the user hasn't already cloned the repository locally, clone it into a **persistent** folder (not `/tmp` or `%TEMP%`), because the Windows startup entry and Desktop shortcut point directly to the built `edpw.exe` and `edp.exe` inside the repo directory.

Suggested default if the user didn't specify a folder:
```powershell
git clone <REPO_URL> "$env:USERPROFILE\ExternalDisplayPosition"
cd "$env:USERPROFILE\ExternalDisplayPosition"
```

---

## 2. Check prerequisites & ask the user
Before running `install.ps1` non-interactively, check Go and confirm the user's preferences:

1. **Check Go:**
   ```powershell
   Get-Command go.exe -ErrorAction SilentlyContinue
   ```
   If Go is **not** installed, ask the user:
   > *"Go is not installed on this machine. Should I install Go automatically using `winget` (`GoLang.Go`), or would you prefer to install it manually?"*

2. **Ask about Windows startup:**
   > *"Would you like me to add the silent background display watcher (`edpw.exe`) to Windows startup and start it now?"*

*(If the user already told you "install everything automatically" / "yes to all", you can skip asking and pass `-y`.)*

---

## 3. Run `install.ps1`
Pass the flags matching the user's answers so the script does not hang waiting on interactive `Read-Host` prompts:

* **Auto-install Go (if needed) + enable startup + start daemon + Desktop shortcut:**
  ```powershell
  powershell -ExecutionPolicy Bypass -File .\install.ps1 -y
  ```

* **Granular non-interactive flags:**
  * `-InstallGo` — install Go via `winget` if missing (without prompting)
  * `-EnableStartup` — add `edpw.exe` to `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` and start it now
  * `-DisableStartup` — skip adding to Windows startup
  * `-SkipShortcut` — skip creating the Desktop shortcut

Example (Go already installed, user declined startup):
```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1 -DisableStartup
```

---

## 4. Verify the installation
Run these checks and report the observed results:
```powershell
.\edp.exe status
.\edp.exe autostart status
Test-Path (Join-Path ([Environment]::GetFolderPath('Desktop')) 'Save Display Layout.lnk')
Get-Process edpw -ErrorAction SilentlyContinue
```

---

## 5. Guide the user on first-time setup
Tell the user how to save each physical port's layout:
1. Plug the monitor into the first port, arrange it in **Settings → System → Display**, and double-click **Save Display Layout** on the Desktop (or run `.\edp.exe save left-port`).
2. Plug the monitor into the other port, arrange it how you want, and double-click **Save Display Layout** again (or run `.\edp.exe save right-port`).
3. From then on, switching ports applies the saved layout automatically.
