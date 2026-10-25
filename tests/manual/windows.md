# Manual Test Paths — Windows (M7 persistence vectors)

Run these after `rosmarops baseline` on a clean machine. Each section documents
the setup step, expected finding, and cleanup. All tests require Windows 10/11
with PowerShell 5.1+ and the RosmarOps binary built from this branch.

---

## Prerequisites

```powershell
# Build and baseline first.
go build -o rosmarops.exe ./cmd/rosmarops
.\rosmarops.exe init
.\rosmarops.exe baseline
```

After each test: run `rosmarops run` and confirm the expected finding appears.
Then undo the change, run `rosmarops baseline` again, and confirm no findings.

---

## 1. Shell profile modified (`shell_profile_modified`)

**What to test:** modifying any of the 4 PowerShell `$PROFILE` scopes triggers
a RED `shell_profile_modified` finding.

**Setup:**
```powershell
# Add a harmless comment to the current user's profile.
Add-Content $PROFILE "# vigil-test"
```

**Expected finding:**
- Severity: `red`
- Category: `shell_profile_modified`
- WhyItMatters populated, SuggestedAction populated

**Cleanup:**
```powershell
# Remove the last line.
$lines = Get-Content $PROFILE
$lines | Select-Object -SkipLast 1 | Set-Content $PROFILE
.\rosmarops.exe baseline
```

---

## 2. New scheduled task — no logon trigger (`scheduled_task_new`)

**What to test:** a new task registered within the last 7 days (no logon trigger)
produces a YELLOW `scheduled_task_new` finding.

**Setup:**
```powershell
$action  = New-ScheduledTaskAction -Execute "notepad.exe"
$trigger = New-ScheduledTaskTrigger -Daily -At "03:00AM"
Register-ScheduledTask -TaskName "VigilTest_Daily" -Action $action -Trigger $trigger -Force
```

**Expected finding:**
- Severity: `yellow`
- Category: `scheduled_task_new`
- task_name = `VigilTest_Daily`

**Cleanup:**
```powershell
Unregister-ScheduledTask -TaskName "VigilTest_Daily" -Confirm:$false
.\rosmarops.exe baseline
```

---

## 3. New scheduled task with logon trigger (`scheduled_task_logon_trigger`)

**What to test:** a new task with a logon trigger produces a RED
`scheduled_task_logon_trigger` finding.

**Setup:**
```powershell
$action  = New-ScheduledTaskAction -Execute "notepad.exe"
$trigger = New-ScheduledTaskTrigger -AtLogOn
Register-ScheduledTask -TaskName "VigilTest_Logon" -Action $action -Trigger $trigger -Force
```

**Expected finding:**
- Severity: `red`
- Category: `scheduled_task_logon_trigger`
- task_name = `VigilTest_Logon`
- has_logon_trigger = `true`

**Cleanup:**
```powershell
Unregister-ScheduledTask -TaskName "VigilTest_Logon" -Confirm:$false
.\rosmarops.exe baseline
```

---

## 4. New Defender exclusion (`av_exclusion_new`)

**Setup (requires elevated PowerShell):**
```powershell
# Add a path exclusion.
Add-MpPreference -ExclusionPath "C:\VigilTest\excluded"
```

**Expected finding:**
- Severity: `red`
- Category: `av_exclusion_new`
- exclusion_kind = `path`
- exclusion_value = `C:\VigilTest\excluded`

**Cleanup (elevated):**
```powershell
Remove-MpPreference -ExclusionPath "C:\VigilTest\excluded"
.\rosmarops.exe baseline
```

**Variant — process exclusion:**
```powershell
Add-MpPreference -ExclusionProcess "vigil-test.exe"
# Expected: exclusion_kind = process, exclusion_value = vigil-test.exe
Remove-MpPreference -ExclusionProcess "vigil-test.exe"
```

**Variant — extension exclusion:**
```powershell
Add-MpPreference -ExclusionExtension ".vigiltest"
# Expected: exclusion_kind = extension, exclusion_value = .vigiltest
Remove-MpPreference -ExclusionExtension ".vigiltest"
```

---

## 5. New global npm package (`global_package_new`)

**Setup:**
```powershell
npm install -g cowsay
```

**Expected finding:**
- Severity: `yellow`
- Category: `global_package_new`
- package_manager = `npm`
- package_name = `cowsay`

**Cleanup:**
```powershell
npm uninstall -g cowsay
.\rosmarops.exe baseline
```

**Variant — pnpm (if pnpm is installed):**
```powershell
pnpm add -g cowsay
# Expected: package_manager = pnpm
pnpm remove -g cowsay
```

---

## 6. Non-standard npm registry (`npm_registry_non_standard`)

**Setup:**
```powershell
# Append a non-standard registry to ~/.npmrc.
Add-Content "$env:USERPROFILE\.npmrc" "registry=http://localhost:4873/"
```

**Expected finding:**
- Severity: `red`
- Category: `npm_registry_non_standard`
- registry_url = `http://localhost:4873/`

**Cleanup:**
```powershell
# Remove the registry line.
$lines = Get-Content "$env:USERPROFILE\.npmrc" | Where-Object { $_ -notmatch '^registry=' }
$lines | Set-Content "$env:USERPROFILE\.npmrc"
.\rosmarops.exe baseline
```

---

## 7. Allow-list suppression (globals)

**What to test:** a package on the allow-list does NOT produce a finding.

**Setup:**
1. Add `cowsay` to `rosmarops.config.json`:
   ```json
   "allow_list": {
     "global_packages": [{"id": "cowsay", "note": "test allow-list"}]
   }
   ```
2. `npm install -g cowsay`
3. `rosmarops run`

**Expected:** no finding for `cowsay`; `allow_listed_count` incremented in report.

**Cleanup:**
```powershell
npm uninstall -g cowsay
# Remove allow-list entry from config.
.\rosmarops.exe baseline
```

---

## Notes

- Defender exclusion tests require an elevated PowerShell session.
- If `Get-MpPreference` is unavailable (e.g., Defender service not running),
  the `avexclusions` scanner logs a warning and skips — no findings, no error.
- Scheduled task tests: tasks registered > 7 days ago will NOT appear in findings
  (outside the recency window). This is expected behaviour.
- On first `rosmarops run` after `rosmarops baseline`, all surfaces are established;
  only changes after the baseline produce findings.
