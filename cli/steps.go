package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	gogit "github.com/go-git/go-git/v5"
)

const arcRepo = "https://github.com/carbon-os/arc"

func stepCloneLibarc(cfg *buildConfig) error {
	if _, err := os.Stat(cfg.arcRepoDir); err == nil {
		fmt.Printf("   updating %s\n", arcRepo)
		repo, err := gogit.PlainOpen(cfg.arcRepoDir)
		if err != nil {
			return fmt.Errorf("open existing repo: %w", err)
		}
		wt, err := repo.Worktree()
		if err != nil {
			return err
		}
		err = wt.Pull(&gogit.PullOptions{RemoteName: "origin"})
		if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return fmt.Errorf("pull: %w", err)
		}
		return nil
	}

	fmt.Printf("   downloading libarc from %s\n", arcRepo)
	if err := os.MkdirAll(cfg.projectDir, 0o755); err != nil {
		return err
	}
	_, err := gogit.PlainClone(cfg.arcRepoDir, false, &gogit.CloneOptions{
		URL:      arcRepo,
		Progress: os.Stderr,
		Depth:    1,
	})
	return err
}

func stepBuildLibarc(cfg *buildConfig) error {
	buildDir := filepath.Join(cfg.libarcDir, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return err
	}

	configureArgs := []string{cfg.libarcDir, "-B", buildDir, "-DCMAKE_BUILD_TYPE=Release"}
	if runtime.GOOS == "windows" {
		vcpkg := os.Getenv("VCPKG_ROOT")
		if vcpkg == "" {
			return fmt.Errorf("VCPKG_ROOT is not set — required for Windows builds")
		}
		configureArgs = append(configureArgs,
			"-DCMAKE_TOOLCHAIN_FILE="+filepath.Join(vcpkg, "scripts", "buildsystems", "vcpkg.cmake"),
		)
	}

	fmt.Printf("   cmake configure\n")
	if err := RunCmd(cfg.libarcDir, "cmake", configureArgs...); err != nil {
		return fmt.Errorf("cmake configure: %w", err)
	}
	fmt.Printf("   cmake build\n")
	if err := RunCmd(cfg.libarcDir, "cmake", "--build", buildDir, "--config", "Release"); err != nil {
		return fmt.Errorf("cmake build: %w", err)
	}

	built, err := findBuiltLibarc(buildDir)
	if err != nil {
		return err
	}

	fmt.Printf("   copying %s → arc-project/%s\n", filepath.Base(built), filepath.Base(cfg.libarcLib))
	if err := CopyFile(built, cfg.libarcLib); err != nil {
		return err
	}

	srcInclude := filepath.Join(cfg.libarcDir, "include")
	dstInclude := filepath.Join(cfg.libarcDestDir, "include")
	fmt.Printf("   copying libarc/include → arc-project/libarc/include\n")
	if err := CopyDir(srcInclude, dstInclude); err != nil {
		return fmt.Errorf("copy include: %w", err)
	}

	fmt.Printf("   removing arc-repo (no longer needed)\n")
	return os.RemoveAll(cfg.arcRepoDir)
}

func findBuiltLibarc(buildDir string) (string, error) {
	name := LibarcFileName()
	candidates := []string{
		filepath.Join(buildDir, "lib", name),
		filepath.Join(buildDir, "lib", "Release", name),
		filepath.Join(buildDir, "Release", name),
		filepath.Join(buildDir, name),
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not found — searched:\n  %s",
		name, strings.Join(candidates, "\n  "))
}

func stepGoMod(cfg *buildConfig) error {
	goMod := filepath.Join(cfg.wd, "go.mod")
	if _, err := os.Stat(goMod); os.IsNotExist(err) {
		modulePath := "app/" + cfg.binaryName
		fmt.Printf("   go mod init %s\n", modulePath)
		if err := RunCmd(cfg.wd, "go", "mod", "init", modulePath); err != nil {
			return fmt.Errorf("go mod init: %w", err)
		}
	} else {
		fmt.Printf("   go.mod already exists, skipping init\n")
	}

	fmt.Printf("   go mod tidy\n")
	if err := RunCmd(cfg.wd, "go", "mod", "tidy"); err != nil {
		return fmt.Errorf("go mod tidy: %w", err)
	}
	return nil
}

func stepCompileGoModule(cfg *buildConfig) error {
	fmt.Printf("   injecting arc_entry_generated.go\n")
	if err := os.WriteFile(cfg.stubPath, []byte(ArcEntryStub), 0o644); err != nil {
		return fmt.Errorf("write stub: %w", err)
	}
	defer func() {
		fmt.Printf("   removing arc_entry_generated.go\n")
		os.Remove(cfg.stubPath)
	}()

	if err := os.MkdirAll(cfg.projectDir, 0o755); err != nil {
		return err
	}

	goArgs := []string{"build", "-buildmode=c-shared", "-o", cfg.moduleLib}
	goArgs = append(goArgs, cfg.goFlags...)
	goArgs = append(goArgs, cfg.goPackage)

	fmt.Printf("   go %s\n", strings.Join(goArgs, " "))
	return RunCmd(cfg.wd, "go", goArgs...)
}

func stepGenerateProject(cfg *buildConfig) error {
	data := projectData{
		BinaryName:     cfg.binaryName,
		LibArcName:     filepath.Base(cfg.libarcLib),
		ModuleName:     filepath.Base(cfg.moduleLib),
		LoadModulePath: loadModulePath(filepath.Base(cfg.moduleLib)),
		LibArcInclude:  "libarc/include",
	}

	// On macOS, pull app name and bundle ID from arc.json so CMake can
	// produce a proper .app bundle with Info.plist — required for StoreKit.
	if runtime.GOOS == "darwin" && cfg.arcJSON != "" {
		if arcCfg, err := LoadArcConfig(cfg.arcJSON); err == nil && arcCfg.App != nil {
			data.BundleID = arcCfg.App.BundleID
			data.AppName = arcCfg.App.Name
		}
	}

	files := []struct {
		name string
		tmpl string
	}{
		{"CMakeLists.txt", CMakeListsTmpl},
		{"main.cpp", MainCppTmpl},
	}

	for _, f := range files {
		path := filepath.Join(cfg.projectDir, f.name)
		fmt.Printf("   writing arc-project/%s\n", f.name)
		t, err := template.New(f.name).Delims("{{", "}}").Parse(f.tmpl)
		if err != nil {
			return fmt.Errorf("parse template %s: %w", f.name, err)
		}
		out, err := os.Create(path)
		if err != nil {
			return err
		}
		if err := t.Execute(out, data); err != nil {
			out.Close()
			return fmt.Errorf("render %s: %w", f.name, err)
		}
		out.Close()
	}
	return nil
}

func stepConfigureCmake(cfg *buildConfig) error {
	buildDir := filepath.Join(cfg.projectDir, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return err
	}

	configureArgs := []string{cfg.projectDir, "-B", buildDir, "-DCMAKE_BUILD_TYPE=Release"}
	if runtime.GOOS == "darwin" {
		configureArgs = append(configureArgs, "-G", "Xcode")
	}
	if runtime.GOOS == "windows" {
		if vcpkg := os.Getenv("VCPKG_ROOT"); vcpkg != "" {
			configureArgs = append(configureArgs,
				"-DCMAKE_TOOLCHAIN_FILE="+filepath.Join(vcpkg, "scripts", "buildsystems", "vcpkg.cmake"),
			)
		}
	}

	fmt.Printf("   cmake -B arc-project/build\n")
	return RunCmd(cfg.projectDir, "cmake", configureArgs...)
}

// stepStoreKit is macOS-only and a no-op when arc.json has no billing config.
func stepStoreKit(cfg *buildConfig) error {
	if cfg.arcJSON == "" {
		fmt.Printf("   no arc.json found — skipping\n")
		return nil
	}

	arcCfg, err := LoadArcConfig(cfg.arcJSON)
	if err != nil {
		return err
	}
	if arcCfg.Billing == nil {
		fmt.Printf("   arc.json has no billing config — skipping\n")
		return nil
	}

	fmt.Printf("   generating %s.storekit from arc.json\n", cfg.binaryName)
	skPath, err := GenerateStoreKit(arcCfg.Billing, cfg.binaryName, cfg.projectDir)
	if err != nil {
		return err
	}
	fmt.Printf("   wrote %s\n", filepath.Base(skPath))

	// Patch every .xcscheme found in the build tree.
	buildDir := filepath.Join(cfg.projectDir, "build")
	schemes, err := filepath.Glob(
		filepath.Join(buildDir, "*.xcodeproj", "xcshareddata", "xcschemes", "*.xcscheme"),
	)
	if err != nil {
		return err
	}
	if len(schemes) == 0 {
		fmt.Printf("   no .xcscheme files found — skipping scheme patch\n")
		return nil
	}
	for _, scheme := range schemes {
		fmt.Printf("   patching %s\n", filepath.Base(scheme))
		if err := PatchXcodeScheme(scheme, skPath); err != nil {
			return fmt.Errorf("patch scheme %s: %w", filepath.Base(scheme), err)
		}
	}
	return nil
}