# Apple Billing Sample App

This example demonstrates how to integrate Apple's In-App Purchases (StoreKit) into your Arc application.

## Prerequisites

Ensure you have built the `arc` CLI tool in your project root:

```bash
go build -o arc cmd/cli/main.go
```

## 1. Build the App

First, compile the application binary:

```bash
go build -o sample-app cmd/apple-billing/main.go
```

## 2. Package the App

Run the `arc` CLI to package the application for macOS. 

To test Apple Billing (StoreKit), your application **should be signed** and packaged as a `.app` bundle. Run the package command targeting macOS:

```bash
./arc --package=macos
```

*Note: If you do not have an Apple Developer certificate yet and just want to verify the packaging process, you can run `./arc --package=macos --skip-sign`. However, be aware that StoreKit requires valid codesigning to test actual transactions successfully.*

## 3. Local StoreKit Testing

When you run the packager, the `arc` CLI automatically reads the `iap` configuration from `arc.json` and generates a local StoreKit configuration file in your output directory:

`dist/Sample App.storekit`

You can open this `.storekit` file in Xcode to manage your test environment, view your configured products, and simulate local purchases without needing App Store Connect.