// Package notify shows the Windows notification of a scheduled review
// (decision 2026-10-26, integridad y revisión semanal, J7): under Windows
// PowerShell's app identity, registering nothing. The PowerShell script is
// a constant; the notification's XML is built here, escaped, and reaches the
// script on its standard input, never inside the command.
package notify

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
)

// AppID is Windows PowerShell's application identity.
const AppID = `{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe`

// Script reads ToastNotifier.Setting and, only when it is Enabled and an XML
// comes on the standard input, shows it; it prints the setting. It is
// constant: nothing of the report enters it.
const Script = `$ErrorActionPreference = 'Stop'
$null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$n = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + AppID + `')
$s = [string]$n.Setting
$x = [Console]::In.ReadToEnd()
if ($s -eq 'Enabled' -and $x.Length -gt 0) {
  $d = New-Object Windows.Data.Xml.Dom.XmlDocument
  $d.LoadXml($x)
  $n.Show((New-Object Windows.UI.Notifications.ToastNotification $d))
}
[Console]::Out.Write($s)`

// Args are powershell.exe's arguments: always the same.
func Args() []string { return []string{"-NonInteractive", "-NoProfile", "-Command", Script} }

// Settings of ToastNotifier.Setting and the report's codes for the ones that
// keep the notification from showing (report.v2 notification_unavailable).
const Enabled = "Enabled"

var unavailable = map[string]string{
	"DisabledForApplication": "disabled_for_application",
	"DisabledForUser":        "disabled_for_user",
	"DisabledByGroupPolicy":  "disabled_by_group_policy",
	"DisabledByManifest":     "disabled_by_manifest",
}

// Reasons are the codes of notification_unavailable.
var Reasons = []string{"disabled_for_application", "disabled_for_user", "disabled_by_group_policy", "disabled_by_manifest"}

// Unavailable is the report's code for a setting that keeps the
// notification from showing; false for Enabled or an unknown answer.
func Unavailable(setting string) (string, bool) {
	code, ok := unavailable[strings.TrimSpace(setting)]
	return code, ok
}

// ToastXML is the notification: one line of text, and a click that opens
// uri (activationType protocol). Text and URI are XML-escaped.
func ToastXML(text, uri string) string {
	return `<toast activationType="protocol" launch="` + esc(uri) + `"><visual><binding template="ToastGeneric"><text>` +
		esc(text) + `</text></binding></visual></toast>`
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// FileURI is the file:/// URI of a Windows path, percent-encoded (UTF-8):
// C:\Users\Ana Pérez\x.html → file:///C:/Users/Ana%20P%C3%A9rez/x.html.
func FileURI(path string) string {
	p := strings.ReplaceAll(path, `\`, "/")
	var b strings.Builder
	b.WriteString("file:///")
	p = strings.TrimLeft(p, "/")
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '-', c == '.', c == '_', c == '~', c == '/':
			b.WriteByte(c)
		case c == ':' && i == 1: // the drive letter's colon
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
