package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpliceAndCompare(t *testing.T) {
	if _, err := exec.LookPath("magick"); err != nil {
		t.Skip("ImageMagick is not installed")
	}
	dir := t.TempDir()
	base := filepath.Join(dir, "base.png")
	candidate := filepath.Join(dir, "candidate.png")
	final := filepath.Join(dir, "final.png")
	if _, err := call("magick", "-size", "120x80", "xc:red", base); err != nil {
		t.Fatal(err)
	}
	if _, err := call("magick", "-size", "120x80", "xc:blue", candidate); err != nil {
		t.Fatal(err)
	}
	if err := splice([]string{"-base", base, "-candidate", candidate, "-region", "30,20,60,40", "-out", final, "-feather", "0"}); err != nil {
		t.Fatal(err)
	}
	for _, point := range []struct{ xy, want string }{{"0,0", "srgb(255,0,0)"}, {"60,40", "srgb(0,0,255)"}, {"119,79", "srgb(255,0,0)"}} {
		got, err := call("magick", final, "-format", "%[pixel:p{"+point.xy+"}]", "info:")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(got)) != point.want {
			t.Errorf("pixel %s = %s, want %s", point.xy, got, point.want)
		}
	}
	if _, err := os.Stat(final + ".json"); err != nil {
		t.Fatal(err)
	}
	review := filepath.Join(dir, "review")
	if err := compare([]string{"-before", base, "-after", final, "-out", review, "-top", "2"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "compare.json", "change-01.jpg"} {
		if _, err := os.Stat(filepath.Join(review, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRejectsRegionOutsideImage(t *testing.T) {
	if _, err := parseRegion("100,0,30,20", 120, 80); err == nil {
		t.Fatal("expected an out-of-bounds error")
	}
}
