package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestHelpUsesAkaveFSBranding(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "akavefs")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}

	goBinary := filepath.Join(runtime.GOROOT(), "bin", "go")
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}

	build := exec.Command(goBinary, "build", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, output)
	}

	help := exec.Command(binary, "--help")
	output, err := help.CombinedOutput()
	if err != nil {
		t.Fatalf("run --help: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "akavefs - Mount an S3 bucket locally") {
		t.Fatalf("help output does not use AkaveFS branding:\n%s", output)
	}
}
