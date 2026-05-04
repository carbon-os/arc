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

const (
	libarcRepo = "https://github.com/carbon-os/libarc"
	vcpkgRepo  = "https://github.com/microsoft/vcpkg"
)

// cloneOrUpdate shallow-clones url into dir, or pulls if dir already exists.
func cloneOrUpdate(dir, url string) error {
	if _, err := os.Stat(dir); err == nil {
		fmt.Printf("   updating %s\n", url)
		repo, err := gogit.PlainOpen(dir)
		if err != nil {
			return fmt.Errorf("open existing repo at %s: %w", dir, err)
		}
		wt, err := repo.Worktree()
		if err != nil {
			return err
		}
		err = wt.Pull(&gogit.PullOptions{RemoteName: "origin"})
		if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
			return fmt.Errorf("pull %s: %w", url, err)
		}
		return nil
	}

	fmt.Printf("   cloning %s\n", url)
	_, err := gogit.PlainClone(dir, false, &gogit.CloneOptions{
		URL:      url,
		Progress: os.Stderr,
		Depth:    1,
	})
	return err
}

func stepCloneLibarc(cfg *buildConfig) error {
	if err := os.MkdirAll(cfg.projectDir, 0o755); err != nil {
		return err
	}

	if err := cloneOrUpdate(cfg.libarcRepoDir, libarcRepo); err != nil {
		return fmt.Errorf("libarc: %w", err)
	}
	if err := cloneOrUpdate(cfg.vcpkgDir, vcpkgRepo); err != nil {
		return fmt.Errorf("vcpkg: %w", err)
	}
	return nil
}

func stepBuildLibarc(cfg *buildConfig) error {
	// ── 1. Bootstrap vcpkg ────────────────────────────────────────────────
	fmt.Printf("   bootstrapping vcpkg\n")
	if err := bootstrapVcpkg(cfg.vcpkgDir); err != nil {
		return fmt.Errorf("vcpkg bootstrap: %w", err)
	}

	// ── 2. vcpkg install (reads vcpkg.json from libarc repo root) ─────────
	fmt.Printf("   vcpkg install\n")
	vcpkgBin := vcpkgBinary(cfg.vcpkgDir)
	if err := RunCmd(cfg.libarcRepoDir, vcpkgBin, "install"); err != nil {
		return fmt.Errorf("vcpkg install: %w", err)
	}

	// ── 3. CMake configure ────────────────────────────────────────────────
	buildDir := filepath.Join(cfg.libarcRepoDir, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return err
	}

	toolchain := filepath.Join(cfg.vcpkgDir, "scripts", "buildsystems", "vcpkg.cmake")
	configureArgs := []string{
		cfg.libarcRepoDir,
		"-B", buildDir,
		"-DCMAKE_BUILD_TYPE=Release",
		"-DCMAKE_TOOLCHAIN_FILE=" + toolchain,
	}

	switch runtime.GOOS {
	case "darwin":
		// Ninja is required on macOS; the default Xcode generator doesn't
		// work for libarc's build (that comes later for the app project).
		configureArgs = append(configureArgs, "-G", "Ninja")
	case "windows":
		// MSVC default generator — no Ninja needed.
	}

	fmt.Printf("   cmake configure (libarc)\n")
	if err := RunCmd(cfg.libarcRepoDir, "cmake", configureArgs...); err != nil {
		return fmt.Errorf("cmake configure: %w", err)
	}

	// ── 4. CMake build ────────────────────────────────────────────────────
	fmt.Printf("   cmake build (libarc)\n")
	if err := RunCmd(cfg.libarcRepoDir, "cmake", "--build", buildDir, "--config", "Release"); err != nil {
		return fmt.Errorf("cmake build: %w", err)
	}

	// ── 5. Copy artifacts into arc-project/ ──────────────────────────────
	built, err := findBuiltLibarc(buildDir)
	if err != nil {
		return err
	}
	fmt.Printf("   copying %s → arc-project/%s\n", filepath.Base(built), filepath.Base(cfg.libarcLib))
	if err := CopyFile(built, cfg.libarcLib); err != nil {
		return err
	}

	srcInclude := filepath.Join(cfg.libarcRepoDir, "include")
	dstInclude := filepath.Join(cfg.libarcDestDir, "include")
	fmt.Printf("   copying libarc/include → arc-project/libarc/include\n")
	if err := CopyDir(srcInclude, dstInclude); err != nil {
		return fmt.Errorf("copy include: %w", err)
	}

	// ── 6. Clean up cloned repos ──────────────────────────────────────────
	fmt.Printf("   removing libarc-repo and vcpkg (no longer needed)\n")
	if err := os.RemoveAll(cfg.libarcRepoDir); err != nil {
		return err
	}
	return os.RemoveAll(cfg.vcpkgDir)
}

// bootstrapVcpkg runs the platform-appropriate bootstrap script.
func bootstrapVcpkg(vcpkgDir string) error {
	switch runtime.GOOS {
	case "windows":
		return RunCmd(vcpkgDir, "cmd", "/C", "bootstrap-vcpkg.bat", "-disableMetrics")
	default:
		return RunCmd(vcpkgDir, "sh", "bootstrap-vcpkg.sh", "-disableMetrics")
	}
}

// vcpkgBinary returns the path to the compiled vcpkg executable.
func vcpkgBinary(vcpkgDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(vcpkgDir, "vcpkg.exe")
	}
	return filepath.Join(vcpkgDir, "vcpkg")
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
	return RunCmd(cfg.wd, "go", "mod", "tidy")
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
		// The app project uses the Xcode generator so Xcode schemes exist for
		// StoreKit patching — this is intentionally different from the Ninja
		// generator used when building libarc itself above.
		configureArgs = append(configureArgs, "-G", "Xcode")
	}

	fmt.Printf("   cmake -B arc-project/build\n")
	return RunCmd(cfg.projectDir, "cmake", configureArgs...)
}

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