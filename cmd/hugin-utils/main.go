package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const huginApp = "net.sourceforge.Hugin"

type box struct{ W, H, X, Y int }
type region struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}
type project struct {
	W, H  int
	ROI   [4]int
	Names []string
	Sizes []box
}
type card struct{ Title, Note, Image, Full string }

func die(err error)                   { fmt.Fprintln(os.Stderr, "hugin-utils:", err); os.Exit(1) }
func logf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
func run(input string, name string, args ...string) ([]byte, error) {
	c := exec.Command(name, args...)
	if input != "" {
		c.Stdin = strings.NewReader(input)
	}
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
func call(name string, args ...string) ([]byte, error) { return run("", name, args...) }
func hugin(input, name string, args ...string) ([]byte, error) {
	if _, err := exec.LookPath(name); err == nil {
		return run(input, name, args...)
	}
	if _, err := exec.LookPath("flatpak"); err != nil {
		return nil, fmt.Errorf("Hugin command %s and Flatpak are unavailable", name)
	}
	return run(input, "flatpak", append([]string{"run", "--command=" + name, huginApp}, args...)...)
}
func file(s string) (string, error) {
	if s == "" {
		return "", errors.New("missing file argument")
	}
	p, err := filepath.Abs(s)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("not a regular file: %s", p)
	}
	return p, nil
}
func directory(s string) (string, error) {
	if s == "" {
		return "", errors.New("missing --out")
	}
	p, err := filepath.Abs(s)
	if err != nil {
		return "", err
	}
	return p, os.MkdirAll(p, 0755)
}
func imageBox(p string) (box, error) {
	b, err := call("magick", "identify", "-format", "%w %h %X %Y", p)
	if err != nil {
		return box{}, err
	}
	var q box
	_, err = fmt.Sscanf(string(b), "%d %d %d %d", &q.W, &q.H, &q.X, &q.Y)
	return q, err
}

var (
	pWidth  = regexp.MustCompile(`(?:^| )w(\d+)`)
	pHeight = regexp.MustCompile(`(?:^| )h(\d+)`)
	pCrop   = regexp.MustCompile(`(?:^| )S(\d+),(\d+),(\d+),(\d+)`)
	pName   = regexp.MustCompile(`(?:^| )n"([^"]+)"`)
)

func matchedInt(re *regexp.Regexp, s string) int {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}
func readProject(p string) (project, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return project{}, err
	}
	var q project
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "p ") {
			q.W, q.H = matchedInt(pWidth, line), matchedInt(pHeight, line)
			q.ROI = [4]int{0, q.W, 0, q.H}
			if m := pCrop.FindStringSubmatch(line); len(m) == 5 {
				for i := 0; i < 4; i++ {
					q.ROI[i], _ = strconv.Atoi(m[i+1])
				}
			}
		}
		if strings.HasPrefix(line, "i ") {
			name := "image"
			if m := pName.FindStringSubmatch(line); len(m) > 1 {
				name = m[1]
			}
			q.Names = append(q.Names, name)
			q.Sizes = append(q.Sizes, box{W: matchedInt(pWidth, line), H: matchedInt(pHeight, line)})
		}
	}
	if q.W < 1 || q.H < 1 || len(q.Names) == 0 {
		return q, fmt.Errorf("invalid Hugin project: %s", p)
	}
	return q, nil
}
func check() error {
	if _, err := exec.LookPath("magick"); err != nil {
		return errors.New("ImageMagick 7 command `magick` is required")
	}
	b, err := call("magick", "-version")
	if err != nil {
		return err
	}
	logf("ImageMagick: %s", strings.Split(string(b), "\n")[0])
	if _, err := exec.LookPath("pano_modify"); err == nil {
		logf("Hugin: native CLI")
		return nil
	}
	if _, err := exec.LookPath("flatpak"); err != nil {
		return errors.New("Hugin CLI or Flatpak is required")
	}
	if _, err := call("flatpak", "info", huginApp); err != nil {
		return err
	}
	logf("Hugin: Flatpak %s", huginApp)
	return nil
}
func cropPreview(src string, r region, dst string) error {
	_, err := call("magick", src, "-background", "#777777", "-alpha", "background", "-crop", geometry(r), "+repage", "-resize", "600x400!", "-quality", "92", dst)
	return err
}
func cropFull(src string, r region, dst string) error {
	_, err := call("magick", src, "-crop", geometry(r), "+repage", "-resize", "600x400!", "-quality", "92", dst)
	return err
}
func geometry(r region) string { return fmt.Sprintf("%dx%d+%d+%d", r.W, r.H, r.X, r.Y) }
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func clamp(v, lo, hi int) int { return max(lo, min(hi, v)) }
func writeHTML(p, title, intro string, cards []card) error {
	var b strings.Builder
	fmt.Fprintf(&b, "<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>%s</title><style>body{font:16px system-ui;background:#181b20;color:#eee;max-width:1500px;margin:auto;padding:24px}article{border-top:1px solid #555;padding:20px 0}img{max-width:100%%;height:auto}a{color:#a9d2ff}</style><h1>%s</h1><p>%s</p>", html.EscapeString(title), html.EscapeString(title), html.EscapeString(intro))
	for _, c := range cards {
		fmt.Fprintf(&b, "<article><h2>%s</h2><p>%s</p><a href=\"%s\"><img loading=\"lazy\" src=\"%s\" alt=\"%s\"></a>", html.EscapeString(c.Title), html.EscapeString(c.Note), html.EscapeString(c.Image), html.EscapeString(c.Image), html.EscapeString(c.Title))
		if c.Full != "" {
			fmt.Fprintf(&b, "<p><a href=\"%s\">Open the complete stitched overlap at native resolution</a></p>", html.EscapeString(c.Full))
		}
		b.WriteString("</article>")
	}
	b.WriteString("</html>\n")
	return os.WriteFile(p, []byte(b.String()), 0644)
}
func writeJSON(p string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0644)
}
func sourceDisagreement(a, b string) (float64, error) {
	raw, err := call("magick", a, b, "-compose", "Difference", "-composite", "-resize", "120x80!", "-colorspace", "Gray", "-depth", "8", "gray:-")
	if err != nil {
		return 0, err
	}
	if len(raw) != 120*80 {
		return 0, errors.New("unexpected source difference raster size")
	}
	sum := 0
	for _, v := range raw {
		sum += max(0, int(v)-12)
	}
	return float64(sum) / float64(len(raw)*243), nil
}
func audit(args []string) error {
	f := flag.NewFlagSet("audit", flag.ContinueOnError)
	pto := f.String("pto", "", "Hugin project")
	pano := f.String("pano", "", "stitched JPEG")
	outArg := f.String("out", "", "output directory")
	scale := f.Int("scale", 25, "preview canvas area percentage")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *scale < 2 || *scale > 100 {
		return errors.New("--scale must be 2–100")
	}
	if err := check(); err != nil {
		return err
	}
	projectFile, err := file(*pto)
	if err != nil {
		return err
	}
	panorama, err := file(*pano)
	if err != nil {
		return err
	}
	out, err := directory(*outArg)
	if err != nil {
		return err
	}
	original, err := readProject(projectFile)
	if err != nil {
		return err
	}
	image, err := imageBox(panorama)
	if err != nil {
		return err
	}
	if abs(image.W-(original.ROI[1]-original.ROI[0])) > 2 || abs(image.H-(original.ROI[3]-original.ROI[2])) > 2 {
		return errors.New("panorama dimensions do not match PTO crop")
	}
	tmp, err := os.MkdirTemp(out, ".hugin-utils-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	previewFile := filepath.Join(tmp, "preview.pto")
	if _, err = hugin("", "pano_modify", fmt.Sprintf("--canvas=%d%%", *scale), "--crop=AUTO", "--output="+previewFile, projectFile); err != nil {
		return err
	}
	preview, err := readProject(previewFile)
	if err != nil {
		return err
	}
	if len(preview.Names) != len(original.Names) {
		return errors.New("source count changed in preview project")
	}
	prefix := filepath.Join(tmp, "remap")
	logf("Remapping %d images at %d%% canvas area…", len(preview.Names), *scale)
	if _, err = hugin("", "nona", "-m", "TIFF_m", "-o", prefix, previewFile); err != nil {
		return err
	}
	strips := make([]string, len(preview.Names))
	boxes := make([]box, len(preview.Names))
	for i := range strips {
		strips[i] = fmt.Sprintf("%s%04d.tif", prefix, i)
		if _, err = file(strips[i]); err != nil {
			return err
		}
		boxes[i], err = imageBox(strips[i])
		if err != nil {
			return err
		}
	}
	var cards []card
	var records []map[string]any
	for i := 0; i+1 < len(boxes); i++ {
		a, c := boxes[i], boxes[i+1]
		left := max(max(a.X, c.X), preview.ROI[0])
		right := min(min(a.X+a.W, c.X+c.W), preview.ROI[1])
		top := max(max(a.Y, c.Y), preview.ROI[2])
		bottom := min(min(a.Y+a.H, c.Y+c.H), preview.ROI[3])
		if right-left < 80 || bottom-top < 120 {
			continue
		}
		pw, ph := min(420, right-left), min(280, bottom-top)
		cx := (left + right) / 2
		var rows []string
		var scores []float64
		for _, row := range []struct {
			name     string
			fraction float64
		}{{"upper", .22}, {"middle", .52}, {"lower", .82}} {
			cy := top + int(float64(bottom-top)*row.fraction)
			r := region{X: clamp(cx-pw/2, left, right-pw), Y: clamp(cy-ph/2, top, bottom-ph), W: pw, H: ph}
			tag := fmt.Sprintf("%02d-%s", i, row.name)
			fa := filepath.Join(tmp, tag+"-a.jpg")
			fb := filepath.Join(tmp, tag+"-b.jpg")
			fp := filepath.Join(tmp, tag+"-p.jpg")
			fd := filepath.Join(tmp, tag+"-diff.jpg")
			fr := filepath.Join(tmp, tag+"-row.jpg")
			if err = cropPreview(strips[i], r, fa); err != nil {
				return err
			}
			if err = cropPreview(strips[i+1], r, fb); err != nil {
				return err
			}
			score, e := sourceDisagreement(fa, fb)
			if e != nil {
				return e
			}
			scores = append(scores, score)
			fw := max(1, r.W*original.W/preview.W)
			fh := max(1, r.H*original.H/preview.H)
			full := region{X: clamp(r.X*original.W/preview.W-original.ROI[0], 0, max(0, image.W-fw)), Y: clamp(r.Y*original.H/preview.H-original.ROI[2], 0, max(0, image.H-fh)), W: min(fw, image.W), H: min(fh, image.H)}
			if err = cropFull(panorama, full, fp); err != nil {
				return err
			}
			if _, err = call("magick", fa, fb, "-compose", "Difference", "-composite", "-auto-level", "-quality", "90", fd); err != nil {
				return err
			}
			if _, err = call("magick", fa, fb, fp, fd, "+append", "-quality", "90", fr); err != nil {
				return err
			}
			rows = append(rows, fr)
			records = append(records, map[string]any{"pair": i, "row": row.name, "preview": r, "panorama": full, "sourceDisagreement": score})
		}
		board := fmt.Sprintf("pair-%02d.jpg", i)
		if _, err = call("magick", rows[0], rows[1], rows[2], "-append", "-quality", "92", filepath.Join(out, board)); err != nil {
			return err
		}
		x0 := clamp(left*original.W/preview.W-original.ROI[0], 0, image.W)
		y0 := clamp(top*original.H/preview.H-original.ROI[2], 0, image.H)
		x1 := clamp(right*original.W/preview.W-original.ROI[0], 0, image.W)
		y1 := clamp(bottom*original.H/preview.H-original.ROI[2], 0, image.H)
		if x1 <= x0 || y1 <= y0 {
			return errors.New("empty mapped overlap crop")
		}
		fullName := fmt.Sprintf("pair-%02d-full.jpg", i)
		if _, err = call("magick", panorama, "-crop", geometry(region{x0, y0, x1 - x0, y1 - y0}), "+repage", "-quality", "90", filepath.Join(out, fullName)); err != nil {
			return err
		}
		score := 0.0
		for _, v := range scores {
			if v > score {
				score = v
			}
		}
		cards = append(cards, card{Title: fmt.Sprintf("Pair %d–%d: %s / %s", i, i+1, original.Names[i], original.Names[i+1]), Note: fmt.Sprintf("Source disagreement %.1f%% (rough priority signal, not defect probability). Rows: upper, middle, lower. Columns: source A, source B, stitched panorama, enhanced difference. Inspect the complete overlap too.", score*100), Image: board, Full: fullName})
		logf("Inspected pair %d–%d", i, i+1)
	}
	if err = writeJSON(filepath.Join(out, "audit.json"), map[string]any{"project": projectFile, "panorama": panorama, "scale": *scale, "panels": records}); err != nil {
		return err
	}
	if err = writeHTML(filepath.Join(out, "index.html"), "Panorama overlap audit", fmt.Sprintf("%d adjacent overlaps. Open each full crop at native resolution and inspect foreground edges.", len(cards)), cards); err != nil {
		return err
	}
	logf("Audit: %s", filepath.Join(out, "index.html"))
	return nil
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func parseRegion(s string, width, height int) (region, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return region{}, errors.New("--region must be x,y,width,height")
	}
	var n [4]int
	for i, v := range parts {
		x, e := strconv.Atoi(v)
		if e != nil {
			return region{}, errors.New("--region must contain integers")
		}
		n[i] = x
	}
	r := region{n[0], n[1], n[2], n[3]}
	if r.X < 0 || r.Y < 0 || r.W < 1 || r.H < 1 || r.X+r.W > width || r.Y+r.H > height {
		return region{}, errors.New("region lies outside panorama")
	}
	return r, nil
}
func mask(args []string) error {
	f := flag.NewFlagSet("mask", flag.ContinueOnError)
	pto := f.String("pto", "", "source PTO")
	image := f.Int("image", -1, "zero-based source index")
	regionArg := f.String("region", "", "x,y,width,height in stitched JPEG")
	outArg := f.String("out", "", "candidate PTO")
	if err := f.Parse(args); err != nil {
		return err
	}
	src, err := file(*pto)
	if err != nil {
		return err
	}
	if *outArg == "" {
		return errors.New("missing --out")
	}
	out, err := filepath.Abs(*outArg)
	if err != nil {
		return err
	}
	if out == src {
		return errors.New("output must differ from input PTO")
	}
	if filepath.Dir(out) != filepath.Dir(src) {
		return errors.New("write candidate PTO beside source PTO so relative image paths resolve")
	}
	p, err := readProject(src)
	if err != nil {
		return err
	}
	if *image < 0 || *image >= len(p.Names) {
		return fmt.Errorf("--image must be 0–%d", len(p.Names)-1)
	}
	r, err := parseRegion(*regionArg, p.ROI[1]-p.ROI[0], p.ROI[3]-p.ROI[2])
	if err != nil {
		return err
	}
	x0, y0 := r.X+p.ROI[0], r.Y+p.ROI[2]
	x1, y1 := x0+r.W-1, y0+r.H-1
	input := fmt.Sprintf("%d %d %d\n%d %d %d\n%d %d %d\n%d %d %d\n", *image, x0, y0, *image, x1, y0, *image, x1, y1, *image, x0, y1)
	b, err := hugin(input, "pano_trafo", "-r", src)
	if err != nil {
		return err
	}
	lines := strings.Fields(strings.TrimSpace(string(b)))
	if len(lines) != 8 {
		return fmt.Errorf("pano_trafo returned %d values, expected 8", len(lines))
	}
	points := make([]string, 8)
	size := p.Sizes[*image]
	for i := 0; i < 8; i += 2 {
		x, e1 := strconv.ParseFloat(lines[i], 64)
		y, e2 := strconv.ParseFloat(lines[i+1], 64)
		if e1 != nil || e2 != nil {
			return errors.New("invalid pano_trafo coordinates")
		}
		if x < 0 || x >= float64(size.W) || y < 0 || y >= float64(size.H) {
			return errors.New("region extends beyond selected source image")
		}
		points[i] = strconv.Itoa(int(x + 0.5))
		points[i+1] = strconv.Itoa(int(y + 0.5))
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	line := fmt.Sprintf("k i%d t1 p\"%s\"", *image, strings.Join(points, " "))
	if err = os.WriteFile(out, []byte(strings.TrimRight(string(data), " \r\n\t")+"\n"+line+"\n"), 0644); err != nil {
		return err
	}
	logf("Added positive mask for source %d (%s): %s", *image, p.Names[*image], out)
	return nil
}

type cell struct {
	X     int     `json:"x"`
	Y     int     `json:"y"`
	Score float64 `json:"score"`
	Mean  float64 `json:"mean"`
}

func compare(args []string) error {
	f := flag.NewFlagSet("compare", flag.ContinueOnError)
	beforeArg := f.String("before", "", "old panorama")
	afterArg := f.String("after", "", "new panorama")
	outArg := f.String("out", "", "output directory")
	top := f.Int("top", 16, "number of changed regions")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *top < 1 || *top > 100 {
		return errors.New("--top must be 1–100")
	}
	before, err := file(*beforeArg)
	if err != nil {
		return err
	}
	after, err := file(*afterArg)
	if err != nil {
		return err
	}
	out, err := directory(*outArg)
	if err != nil {
		return err
	}
	a, err := imageBox(before)
	if err != nil {
		return err
	}
	b, err := imageBox(after)
	if err != nil {
		return err
	}
	if a.W != b.W || a.H != b.H {
		return errors.New("before and after dimensions differ")
	}
	sw, sh := max(1, (a.W+8)/16), max(1, (a.H+8)/16)
	raw, err := call("magick", before, after, "-compose", "Difference", "-composite", "-resize", fmt.Sprintf("%dx%d!", sw, sh), "-colorspace", "Gray", "-depth", "8", "gray:-")
	if err != nil {
		return err
	}
	if len(raw) != sw*sh {
		return errors.New("unexpected difference raster size")
	}
	var cells []cell
	for y := 0; y < sh; y += 55 {
		for x := 0; x < sw; x += 80 {
			endX, endY := min(sw, x+80), min(sh, y+55)
			sum, changed := 0, 0
			for yy := y; yy < endY; yy++ {
				for xx := x; xx < endX; xx++ {
					v := int(raw[yy*sw+xx])
					sum += v
					if v > 12 {
						changed++
					}
				}
			}
			count := (endX - x) * (endY - y)
			cells = append(cells, cell{X: x, Y: y, Score: float64(changed) / float64(count), Mean: float64(sum) / float64(count)})
		}
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].Score == cells[j].Score {
			return cells[i].Mean > cells[j].Mean
		}
		return cells[i].Score > cells[j].Score
	})
	if len(cells) > *top {
		cells = cells[:*top]
	}
	var cards []card
	for i, c := range cells {
		w, h := min(1200, a.W), min(800, a.H)
		r := region{X: clamp((c.X+40)*a.W/sw-w/2, 0, a.W-w), Y: clamp((c.Y+27)*a.H/sh-h/2, 0, a.H-h), W: w, H: h}
		left := filepath.Join(out, fmt.Sprintf("change-%02d-before.jpg", i+1))
		right := filepath.Join(out, fmt.Sprintf("change-%02d-after.jpg", i+1))
		board := fmt.Sprintf("change-%02d.jpg", i+1)
		if err = cropFull(before, r, left); err != nil {
			return err
		}
		if err = cropFull(after, r, right); err != nil {
			return err
		}
		if _, err = call("magick", left, right, "+append", filepath.Join(out, board)); err != nil {
			return err
		}
		os.Remove(left)
		os.Remove(right)
		cards = append(cards, card{Title: fmt.Sprintf("Change %d at (%d, %d)", i+1, r.X, r.Y), Note: fmt.Sprintf("Left before, right after. Changed pixels in coarse cell: %.1f%%. Check improvement or regression.", c.Score*100), Image: board})
	}
	if err = writeJSON(filepath.Join(out, "compare.json"), map[string]any{"before": before, "after": after, "cells": cells}); err != nil {
		return err
	}
	if err = writeHTML(filepath.Join(out, "index.html"), "Panorama change review", "Largest pixel changes first. A large change is not automatically a defect.", cards); err != nil {
		return err
	}
	logf("Comparison: %s", filepath.Join(out, "index.html"))
	return nil
}
func splice(args []string) error {
	f := flag.NewFlagSet("splice", flag.ContinueOnError)
	baseArg := f.String("base", "", "old panorama")
	candidateArg := f.String("candidate", "", "new panorama")
	regionArg := f.String("region", "", "x,y,width,height")
	outArg := f.String("out", "", "final image")
	feather := f.Int("feather", 30, "Gaussian feather radius")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *feather < 0 || *feather > 500 {
		return errors.New("--feather must be 0–500")
	}
	base, err := file(*baseArg)
	if err != nil {
		return err
	}
	candidate, err := file(*candidateArg)
	if err != nil {
		return err
	}
	if *outArg == "" {
		return errors.New("missing --out")
	}
	out, err := filepath.Abs(*outArg)
	if err != nil {
		return err
	}
	if out == base || out == candidate {
		return errors.New("output must differ from inputs")
	}
	a, err := imageBox(base)
	if err != nil {
		return err
	}
	b, err := imageBox(candidate)
	if err != nil {
		return err
	}
	if a.W != b.W || a.H != b.H {
		return errors.New("base and candidate dimensions differ")
	}
	r, err := parseRegion(*regionArg, a.W, a.H)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "hugin-utils-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	maskFile := filepath.Join(tmp, "mask.png")
	patchFile := filepath.Join(tmp, "patch.miff")
	cmd := []string{"-size", fmt.Sprintf("%dx%d", a.W, a.H), "xc:black", "-fill", "white", "-draw", fmt.Sprintf("rectangle %d,%d %d,%d", r.X, r.Y, r.X+r.W-1, r.Y+r.H-1)}
	if *feather > 0 {
		cmd = append(cmd, "-blur", fmt.Sprintf("0x%d", *feather))
	}
	cmd = append(cmd, maskFile)
	if _, err = call("magick", cmd...); err != nil {
		return err
	}
	if _, err = call("magick", candidate, maskFile, "-alpha", "off", "-compose", "CopyOpacity", "-composite", patchFile); err != nil {
		return err
	}
	if _, err = call("magick", base, patchFile, "-compose", "Over", "-composite", "-quality", "96", out); err != nil {
		return err
	}
	if err = writeJSON(out+".json", map[string]any{"base": base, "candidate": candidate, "output": out, "region": r, "feather": *feather, "tool": "hugin-utils splice"}); err != nil {
		return err
	}
	logf("Spliced panorama: %s", out)
	return nil
}
func usage() {
	fmt.Print(`hugin-utils: inspect and repair panorama seams

  hugin-utils doctor
  hugin-utils audit --pto PROJECT.pto --pano PANORAMA.jpg --out DIR [--scale 25]
  hugin-utils mask --pto PROJECT.pto --image INDEX --region x,y,w,h --out CANDIDATE.pto
  hugin-utils compare --before OLD.jpg --after NEW.jpg --out DIR [--top 16]
  hugin-utils splice --base OLD.jpg --candidate NEW.jpg --region x,y,w,h --out FINAL.jpg [--feather 30]

No command edits input photos or PTO files.
`)
}
func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	var err error
	switch os.Args[1] {
	case "help", "--help", "-h":
		usage()
		return
	case "doctor":
		err = check()
	case "audit":
		err = audit(os.Args[2:])
	case "mask":
		err = mask(os.Args[2:])
	case "compare":
		err = compare(os.Args[2:])
	case "splice":
		err = splice(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command: %s", os.Args[1])
	}
	if err != nil {
		die(err)
	}
}
