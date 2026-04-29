package cli

import "runtime"

func SharedExt() string {
	switch runtime.GOOS {
	case "darwin":
		return ".dylib"
	case "windows":
		return ".dll"
	default:
		return ".so"
	}
}

func LibarcFileName() string {
	switch runtime.GOOS {
	case "windows":
		return "arc.dll"
	case "darwin":
		return "libarc.dylib"
	default:
		return "libarc.so"
	}
}

func loadModulePath(moduleBase string) string {
	switch runtime.GOOS {
	case "darwin":
		return "@executable_path/" + moduleBase
	default:
		return moduleBase
	}
}