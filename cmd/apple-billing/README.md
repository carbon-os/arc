# cmd/apple-billing — macOS In-App Purchase Setup

StoreKit will silently refuse to work unless every layer below is correct.
Work through the sections **in order** — skipping ahead causes confusing silent failures.

---

## 1. App Store Connect

### 1.1 Create the app record
1. Sign in to [appstoreconnect.apple.com](https://appstoreconnect.apple.com).
2. **My Apps → (+) New App** — choose **macOS**, set the Bundle ID to match
   whatever you will use in step 2 (e.g. `com.example.myapp`).

### 1.2 Register IAP products
For each product ID you pass to `billing.Config.Products`:

1. Open the app → **Monetization → In-App Purchases → (+)**.
2. Choose **Auto-Renewable Subscription** (`ProductKind = Subscription`) or
   **Non-Consumable / Consumable** (`ProductKind = OneTime`).
3. Set the **Product ID** to the exact string you use in Go, e.g.
   `com.example.myapp.pro_monthly`.
4. Fill in localizations and pricing, then **Save**.
5. Leave status as **Ready to Submit** — sandbox purchases work before review.

---

## 2. Developer Portal — certificates & entitlements

### 2.1 Enable the In-App Purchase capability
1. Go to [developer.apple.com/account](https://developer.apple.com/account) →
   **Certificates, IDs & Profiles → Identifiers**.
2. Select your App ID → enable **In-App Purchase** → **Save**.

### 2.2 Create / download a Developer ID or Mac App Distribution certificate
```bash
# List what you already have
security find-identity -v -p codesigning
```

If you need a new certificate, create it in the portal and download it, or
generate it via Xcode → **Settings → Accounts → Manage Certificates**.

---

## 3. Entitlements file

Create `build/mac/MyApp.entitlements`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <!-- Required for StoreKit on macOS -->
    <key>com.apple.application-identifier</key>
    <string>TEAMID.com.example.myapp</string>

    <key>com.apple.developer.team-identifier</key>
    <string>TEAMID</string>

    <!-- Hardened runtime — required for notarisation -->
    <key>com.apple.security.app-sandbox</key>
    <false/>
    <key>com.apple.security.network.client</key>
    <true/>
</dict>
</plist>
```

Replace `TEAMID` with your 10-character Apple team ID
(`developer.apple.com → Membership → Team ID`).

---

## 4. Build the renderer (C++)

The renderer must be compiled with StoreKit linked and the billing source included.

```bash
cd renderer          # your cmake project root

cmake -B build/mac -G Ninja \
  -DCMAKE_BUILD_TYPE=Release \
  -DCMAKE_OSX_ARCHITECTURES="arm64;x86_64"   # universal binary

cmake --build build/mac --target renderer
```

Confirm StoreKit is linked in your `CMakeLists.txt`:

```cmake
target_link_libraries(renderer PRIVATE
    "-framework WebKit"
    "-framework StoreKit"          # <-- must be present
    "-framework Foundation"
    "-framework AppKit"
)

# billing.mm must be in the sources list
target_sources(renderer PRIVATE
    src/billing.mm
    src/host_channel_unix.cpp
    src/main.cpp
    # ...
)
```

---

## 5. Build the Go app

```bash
# From repo root
go build -o build/mac/MyApp ./cmd/apple-billing
```

Set `AppConfig` in your `main.go`:

```go
app := arc.NewApp(arc.AppConfig{
    Title: "My App",
    Renderer: arc.RendererConfig{
        // Point at the cmake output from step 4
        Path: "build/mac/renderer",
    },
    Logging: true,   // flip off for production
})
```

---

## 6. Assemble the .app bundle

StoreKit checks the bundle structure and signature — it **will not work** from a
bare binary on the path.

```
MyApp.app/
└── Contents/
    ├── Info.plist
    ├── MacOS/
    │   ├── MyApp          ← Go binary
    │   └── renderer       ← C++ renderer binary
    └── Resources/
```

### 6.1 Minimal Info.plist

`build/mac/MyApp.app/Contents/Info.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
    "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleIdentifier</key>       <string>com.example.myapp</string>
    <key>CFBundleName</key>             <string>MyApp</string>
    <key>CFBundleExecutable</key>       <string>MyApp</string>
    <key>CFBundleVersion</key>          <string>1</string>
    <key>CFBundleShortVersionString</key><string>1.0.0</string>
    <key>LSMinimumSystemVersion</key>   <string>13.0</string>
    <key>NSPrincipalClass</key>         <string>NSApplication</string>
    <key>NSHighResolutionCapable</key>  <true/>
</dict>
</plist>
```

### 6.2 Copy binaries in

```bash
APP=build/mac/MyApp.app/Contents/MacOS

mkdir -p "$APP"

cp build/mac/MyApp   "$APP/MyApp"
cp build/mac/renderer "$APP/renderer"

chmod +x "$APP/MyApp" "$APP/renderer"
```

---

## 7. Sign the bundle

Sign the renderer first, then the outer app — always inner-to-outer.

```bash
CERT="Developer ID Application: Your Name (TEAMID)"
BUNDLE="build/mac/MyApp.app"
ENTITLEMENTS="build/mac/MyApp.entitlements"

# Sign the renderer binary
codesign --force --options runtime \
  --sign "$CERT" \
  "$BUNDLE/Contents/MacOS/renderer"

# Sign the Go binary
codesign --force --options runtime \
  --sign "$CERT" \
  "$BUNDLE/Contents/MacOS/MyApp"

# Sign the .app bundle with entitlements
codesign --force --options runtime \
  --entitlements "$ENTITLEMENTS" \
  --sign "$CERT" \
  "$BUNDLE"

# Verify
codesign --verify --deep --strict --verbose=2 "$BUNDLE"
spctl --assess --type execute --verbose "$BUNDLE"
```

---

## 8. Sandbox testing (before submitting)

### 8.1 Create a Sandbox tester account
App Store Connect → **Users and Access → Sandbox Testers → (+)**.
Use an email address that is **not** an existing Apple ID.

### 8.2 Sign out of the real App Store on your Mac
**System Settings → App Store → Sign Out**.
Do **not** sign into the sandbox account here — StoreKit uses the sandbox
account only when it presents the purchase sheet.

### 8.3 Run the signed app
```bash
open build/mac/MyApp.app
```

When the purchase sheet appears, sign in with your sandbox tester credentials.
No real money is charged.

### 8.4 Watch the logs
```bash
# Arc + renderer logs (if Logging: true)
./build/mac/MyApp.app/Contents/MacOS/MyApp 2>&1 | tee app.log

# StoreKit system logs
log stream --predicate 'subsystem == "com.apple.storekit"' --level debug
```

---

## 9. Go usage reference

```go
app.OnReady(func() {
    win := app.NewBrowserWindow(window.Config{Title: "My App"})

    win.OnReady(func() {
        b, err := win.NewBilling(billing.Config{
            Products: []billing.Product{
                {ID: "com.example.myapp.pro_monthly", Kind: billing.Subscription},
                {ID: "com.example.myapp.lifetime",    Kind: billing.OneTime},
            },
        })
        if err != nil {
            log.Fatal(err)
        }

        // Fires once StoreKit returns live metadata
        b.OnProducts(func(products []billing.ProductInfo) {
            for _, p := range products {
                log.Printf("product: %s  %s", p.ID, p.FormattedPrice)
            }
        })

        // Fires on every purchase lifecycle transition
        b.OnPurchase(func(evt billing.PurchaseEvent) {
            switch evt.Status {
            case billing.Purchased, billing.Restored:
                log.Printf("unlocking %s", evt.ProductID)
            case billing.Deferred:
                log.Printf("ask-to-buy pending for %s — do not unlock yet", evt.ProductID)
            case billing.Cancelled:
                log.Printf("user cancelled")
            case billing.Failed:
                log.Printf("purchase failed: %v", evt.Err)
            }
        })

        // Trigger a purchase
        b.Buy("com.example.myapp.pro_monthly")

        // Restore previous purchases (required by App Store guidelines)
        b.Restore()
    })

    win.LoadFile("ui/index.html")
})
```

---

## 10. Common failures

| Symptom | Cause | Fix |
|---|---|---|
| `evtBillingProducts` returns empty slice | Bundle ID mismatch or product not saved in ASC | Check `CFBundleIdentifier` matches ASC exactly |
| Purchase sheet never appears | App not signed / wrong entitlements | Re-run step 7; check `codesign --verify` |
| `SKErrorDomain 0` on buy | Sandbox tester not signed in | Sign out of real store, let sheet prompt for sandbox login |
| `product not found` log from renderer | `Buy()` called before `OnProducts` fires | Call `Buy()` only inside the `OnProducts` callback |
| Works in sandbox, fails in production | Missing **In-App Purchase** capability on provisioning profile | Re-generate profile in portal after enabling capability |
| `Deferred` status, content unlocked early | App incorrectly treating Deferred as Purchased | Only unlock on `Purchased` or `Restored` |