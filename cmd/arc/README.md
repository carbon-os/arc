# arc CLI

The `arc` build tool for Arc applications. Handles everything needed to go
from a Go source tree to a native, distributable host binary — without
requiring a `git` binary or manual CMake setup.

## Installation

```bash
GOPROXY=direct go install github.com/carbon-os/arc/cmd/arc@latest
export PATH="$PATH:$(go env GOPATH)/bin"
```

Or build from source in the repo root:

```bash
go build -o arc ./cmd/arc
```

---

## Usage

```bash
arc <command> [flags]
```

### Commands

| Command | Description |
| :--- | :--- |
| `arc build` | Clone/update libarc, compile your Go module, generate and configure `arc-project/` |
| `arc help` | Print usage information |

---

## `arc build`

```bash
arc build [-o name] [--config arc.json] [go-build-flags] [package]
```

The only command you need for a production build. Runs the following steps in order:

**1. Clone or update libarc**

Uses [go-git v5](https://github.com/go-git/go-git) — a pure Go git
implementation bundled into the `arc` binary — to clone or update
`https://github.com/carbon-os/arc` into `arc-project/arc-repo/`. No `git`
binary is required on the host machine.

**2. Build libarc with CMake**

Configures and builds libarc in Release mode inside `arc-project/arc-repo/libarc/build/`,
then copies the resulting shared library and `libarc/include/` headers into
`arc-project/`. The cloned repo is removed once the copy is complete. On
Windows, `VCPKG_ROOT` must be set.

**3. Resolve Go dependencies**

Runs `go mod init` (if no `go.mod` exists yet) followed by `go mod tidy` to
ensure all dependencies are present before compilation.

**4. Compile your Go module**

Injects a temporary `arc_entry_generated.go` stub into your package, then
calls `go build -buildmode=c-shared` to produce
`arc-project/libarc-module.{ext}`. The stub exports `AppMain` — the symbol
libarc calls in production mode. It is always removed after the build, even
on failure.

**5. Generate `arc-project/`**

Writes a `CMakeLists.txt` and `main.cpp` alongside the compiled libraries.
The `main.cpp` is identical on every platform and never needs to be edited.

**6. Pre-configure the CMake build tree**

Runs `cmake -B arc-project/build` so the project is ready to build or open
in an IDE immediately. On macOS, the `-G Xcode` generator is used so the
project can be opened directly in Xcode.

**7. Configure StoreKit** *(macOS only)*

If an `arc.json` with a `billing` section is present, generates a
`<binary>.storekit` configuration file in `arc-project/` and patches the
`<LaunchAction>` of every `.xcscheme` in the build tree to reference it.
This step is skipped automatically on non-Apple platforms, and is a no-op
when no billing configuration is found.

### Flags

| Flag | Description |
| :--- | :--- |
| `-o name` | Name of the final host binary. Defaults to the current directory name. |
| `--config path` | Path to `arc.json`. Defaults to `arc.json` in the current directory if present. |

Any other flags (e.g. `-race`, `-tags`) are forwarded verbatim to `go build`.

### Examples

```bash
# Standard build
arc build .

# Custom output binary name
arc build -o myapp .

# Explicit arc.json location
arc build -o myapp --config path/to/arc.json .

# With extra go build flags
arc build -race -o myapp .
```

### After running `arc build`

```
your-app/
├── main.go                        ← your app, untouched
├── arc.json                       ← optional app/billing config
├── go.mod
│
└── arc-project/
    ├── CMakeLists.txt             ← auto-generated
    ├── main.cpp                   ← auto-generated host entry point
    ├── libarc-module.dylib        ← your compiled Go logic
    ├── libarc.dylib               ← native webview + run loop
    ├── <binary>.storekit          ← generated from arc.json (macOS, if billing config present)
    ├── libarc/
    │   └── include/               ← libarc headers (for compilation)
    └── build/                     ← cmake build tree, configured and ready
```

To produce the final host binary:

```bash
cd arc-project && cmake --build build
```

Or open `arc-project/build/*.xcodeproj` in Xcode for a full native debug
session with breakpoints, Instruments, and the memory graph.

---

## `arc.json`

An optional `arc.json` file placed alongside your Go source can configure
app metadata and in-app billing. It is detected automatically, or supplied
explicitly via `--config`.

```json
{
  "app": {
    "name": "My App",
    "bundle_id": "com.example.myapp"
  },
  "billing": {
    "identifier": "a1b2c3d4",
    "subscription_groups": [
      {
        "id": "e5f6a7b8",
        "name": "Pro",
        "subscriptions": [
          {
            "product_id": "com.example.myapp.pro.monthly",
            "reference_name": "Pro Monthly",
            "display_price": "4.99",
            "recurring_period": "P1M",
            "family_shareable": false,
            "group_number": 1,
            "internal_id": "c9d0e1f2",
            "localizations": [
              {
                "locale": "en_US",
                "display_name": "Pro Monthly",
                "description": "Full access, billed monthly."
              }
            ]
          }
        ]
      }
    ]
  }
}
```

The `billing` section maps directly to Apple's `.storekit` configuration
format. Identifiers must be 8-character hex strings. `recurring_period`
follows ISO 8601 duration notation (e.g. `P1M` for monthly, `P1Y` for yearly).

---

## Platform requirements

| Platform | Requirements |
| :--- | :--- |
| macOS | Xcode Command Line Tools, CMake ≥ 3.22 |
| Windows | Visual Studio 2022, CMake ≥ 3.22, vcpkg (`VCPKG_ROOT` must be set) |
| Linux | GCC or Clang, CMake ≥ 3.22, `libwebkit2gtk-4.1-dev`, `libgtk-3-dev` |

On Linux, install the webview dependencies before running `arc build`:

```bash
# Debian / Ubuntu
sudo apt install libwebkit2gtk-4.1-dev libgtk-3-dev

# Fedora
sudo dnf install webkit2gtk4.1-devel gtk3-devel
```

---

## Development vs production

`arc build` is only needed to produce a distributable binary. During
day-to-day development you don't use it at all:

```bash
go run .
```

The Go arc package spawns the renderer as a subprocess and drives it over IPC.
No CMake, no C compiler, no Xcode required.

| | Development | Production |
| :--- | :--- | :--- |
| Command | `go run .` | `arc build` then `cmake --build` |
| Process model | Two processes (Go + renderer) | Single process |
| Build requirements | Go toolchain only | CMake, C++ compiler |
| App Store / MSIX ready | No | Yes |
| Sandbox safe | No | Yes |