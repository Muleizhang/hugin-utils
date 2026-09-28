---
name: hugin-utils
description: Inspect and repair visible stitching seams in Hugin panoramas using repeatable overlap boards, change reviews, and localized image splices. Use when a user asks to diagnose or improve an existing panorama and its Hugin PTO project.
---

# Hugin panorama seam review

Build this project's CLI with `go build -o bin/hugin-utils ./cmd/hugin-utils` if the binary is absent. It uses ImageMagick and Hugin CLI (native commands or the `net.sourceforge.Hugin` Flatpak). Run `bin/hugin-utils help` for options.

1. Keep source photos and the original PTO intact. Use new output paths for candidates. Run `doctor` to check dependencies.
2. Run `audit --pto PROJECT.pto --pano PANORAMA.jpg --out AUDIT_DIR`. Open `AUDIT_DIR/index.html` and inspect **every adjacent overlap**, especially the lower foreground row. The columns show both remapped sources, the stitched result, and enhanced difference. Open each card's complete overlap crop at native resolution to check between sample rows and near both sides. A bright difference or high score means source disagreement, not necessarily a bad seam. Examine nonadjacent overlaps when the scene calls for it.
3. Fix distant geometry through control points and Hugin optimization. For near foreground parallax, choose one source for each distinct feature. Use `mask --pto PROJECT.pto --image INDEX --region x,y,width,height --out CANDIDATE.pto` to add a positive mask. The region uses coordinates in the cropped stitched JPEG. Save the candidate beside the source PTO so relative image paths work. Render a small preview first. Do not optimize the whole panorama to make one nearby object coincide at the cost of distant alignment.
4. Render a full-size candidate. Run `compare --before OLD.jpg --after NEW.jpg --out REVIEW_DIR`; inspect all top change boards plus the repaired area and both ends of each new mask at native resolution. Also scan the full panorama. A changed region can be an improvement or a regression.
5. If Hugin moves a seam elsewhere, revise its mask or use `splice --base OLD.jpg --candidate NEW.jpg --region x,y,width,height --out FINAL.jpg`. Keep the region tight and inspect its perimeter. `splice` writes `FINAL.jpg.json` with the inputs and region. The PTO alone does not reproduce a spliced JPEG; retain both source renders and the JSON recipe.

Do not call a panorama finished after checking only the reported defect. Check the rest of the near shoreline and foreground before delivery. Report what was inspected and any remaining uncertainty.
