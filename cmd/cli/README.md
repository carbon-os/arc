# Arc CLI

The Arc CLI is the standalone build and packaging tool for your Arc applications. It takes your compiled Go binary and C++ renderer, and bundles them into native, distributable packages for macOS, Windows, and Linux.

## Installation

To build the CLI tool from your project root:

```bash
go build -o arc cmd/cli/main.go
```

## Usage

Run the `arc` binary in the directory containing your configuration file.

```bash
./arc [flags]
```

### Command Line Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--config` | `arc.json` | Path to your configuration file. Useful for CI/CD or separate dev/prod configs. |
| `--target` | (all configured) | Comma-separated list of targets to build (e.g., `macos`, `windows`, `linux`). If omitted, builds all platforms defined in the config. |
| `--skip-sign` | `false` | Bypasses code signing for macOS and Windows. Use this for local testing to avoid certificate requirements. |

**Examples:**
```bash
# Build all platforms defined in arc.json
./arc

# Build only for macOS and Linux
./arc --target=macos,linux

# Build for Windows using a specific staging configuration, without signing
./arc --config=arc.staging.json --target=windows --skip-sign
```

---

## Configuration (`arc.json`)

The CLI reads your packaging configuration from a JSON file (default: `arc.json`). This keeps your application's source code clean and devoid of build-time metadata.

### Root Configuration

| Field | Type | Description |
| :--- | :--- | :--- |
| `appName` | `string` | **Required.** The user-facing name of your application. |
| `package` | `object` | **Required.** The packaging definitions. Contains the following sub-fields: |

### `package` Object

| Field | Type | Description |
| :--- | :--- | :--- |
| `outDir` | `string` | The directory where output bundles are saved. Defaults to `"dist"`. |
| `binaryPath` | `string` | Path to the compiled Go executable. Defaults to the running executable if empty. |
| `rendererBuildDir` | `string` | Path to the C++ renderer build output. Defaults to `"renderer/build/bin"`. |
| `macos` | `object` | macOS packaging configuration. Omit to skip macOS builds. |
| `windows` | `object` | Windows packaging configuration. Omit to skip Windows builds. |
| `linux` | `object` | Linux packaging configuration. Omit to skip Linux builds. |

---

### Platform: `macos`

Produces a `.app` bundle.

| Field | Type | Description |
| :--- | :--- | :--- |
| `bundleID` | `string` | **Required.** Reverse-DNS identifier (e.g., `"com.example.app"`). |
| `version` | `string` | Short version string (e.g., `"1.2.0"`). Defaults to `"1.0.0"`. |
| `build` | `string` | Build number (e.g., `"42"`). Defaults to `"1"`. |
| `minMacOS` | `string` | Minimum supported OS version. Defaults to `"13.0"`. |
| `teamID` | `string` | 10-character Apple Developer Team ID (Required for signing). |
| `signCert` | `string` | Partial name of the certificate to use for codesign (Required for signing). |
| `iap` | `object` | Optional. In-app purchase configuration for StoreKit sandbox testing. |

---

### Platform: `windows`

Produces an `.msix` package.

| Field | Type | Description |
| :--- | :--- | :--- |
| `packageID` | `string` | Internal package identifier. Defaults to `appName` without spaces. |
| `publisher` | `string` | Publisher string matching your signing certificate (e.g., `"CN=Acme Corp, O=Acme Corp, C=US"`). |
| `version` | `string` | Four-part version string (e.g., `"1.2.0.0"`). Defaults to `"1.0.0.0"`. |
| `displayName` | `string` | The name shown in the Start menu. Defaults to `appName`. |
| `certPath` | `string` | Path to your `.pfx` signing certificate. |
| `certPassword` | `string` | Password for the `.pfx` certificate. |

---

### Platform: `linux`

Produces `.deb` and/or `.AppImage` files.

| Field | Type | Description |
| :--- | :--- | :--- |
| `name` | `string` | Internal package name (e.g., `"sample-app"`). Defaults to lowercase `appName`. |
| `version` | `string` | Version string. Defaults to `"1.0.0"`. |
| `maintainer` | `string` | Maintainer contact info (e.g., `"Jane Doe <jane@example.com>"`). |
| `description` | `string` | Short description of the application. |
| `homepage` | `string` | URL to the application's website. |
| `deb` | `boolean` | Set to `true` to build a Debian `.deb` package. |
| `appImage` | `boolean` | Set to `true` to build an AppImage. |

---

## Example `arc.json`

```json
{
  "appName": "Sample App",
  "package": {
    "outDir": "dist",
    "binaryPath": "./bin/sample-app",
    "rendererBuildDir": "renderer/build/bin",
    "macos": {
      "bundleID": "com.example.app",
      "version": "1.2.0",
      "build": "42",
      "minMacOS": "13.0",
      "teamID": "ABCDE12345",
      "signCert": "Developer ID Application: Acme Corp"
    },
    "windows": {
      "packageID": "SampleApp",
      "publisher": "CN=Acme Corp, O=Acme Corp, C=US",
      "version": "1.2.0.0",
      "displayName": "Sample App",
      "certPath": "./certs/win.pfx",
      "certPassword": "super-secret-password"
    },
    "linux": {
      "name": "sample-app",
      "version": "1.2.0",
      "maintainer": "Acme Corp <hello@example.com>",
      "description": "An awesome sample desktop application.",
      "deb": true,
      "appImage": true
    }
  }
}
```