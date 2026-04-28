package plist

import "strings"
import "text/template"

type InfoData struct {
	BundleID   string
	AppName    string
	Executable string
	Version    string
	Build      string
	MinMacOS   string
}

type EntitlementsData struct {
	TeamID   string
	BundleID string
}

const infoTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key>        <string>{{.BundleID}}</string>
    <key>CFBundleName</key>              <string>{{.AppName}}</string>
    <key>CFBundleDisplayName</key>       <string>{{.AppName}}</string>
    <key>CFBundleExecutable</key>        <string>{{.Executable}}</string>
    <key>CFBundlePackageType</key>       <string>APPL</string>
    <key>CFBundleShortVersionString</key><string>{{.Version}}</string>
    <key>CFBundleVersion</key>           <string>{{.Build}}</string>
    <key>LSMinimumSystemVersion</key>    <string>{{.MinMacOS}}</string>
    <key>NSPrincipalClass</key>          <string>NSApplication</string>
    <key>NSHighResolutionCapable</key>   <true/>
</dict>
</plist>
`

const entitlementsTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.application-identifier</key>
    <string>{{.TeamID}}.{{.BundleID}}</string>

    <key>com.apple.developer.team-identifier</key>
    <string>{{.TeamID}}</string>

    <key>com.apple.security.app-sandbox</key>
    <false/>

    <key>com.apple.security.network.client</key>
    <true/>
</dict>
</plist>
`

func RenderInfo(data InfoData) string         { return render(infoTmpl, data) }
func RenderEntitlements(data EntitlementsData) string { return render(entitlementsTmpl, data) }

func render(tmpl string, data any) string {
	t := template.Must(template.New("").Parse(tmpl))
	var b strings.Builder
	_ = t.Execute(&b, data)
	return b.String()
}