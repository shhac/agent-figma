// Package imagediff compares a design render with an implementation
// screenshot and says where they differ, as regions rather than a score.
package imagediff

import (
	"image"
	"image/color"
	"image/draw"
	"slices"
)

// cell is the grid size differing pixels are bucketed into before regions
// are merged, so anti-aliasing noise does not split one difference into many.
const cell = 16

const maxRegions = 20

// Box is a rectangle in design pixels.
type Box struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

func (b Box) Center() (int, int) { return b.X + b.W/2, b.Y + b.H/2 }

// Region is one area of difference.
type Region struct {
	Box    Box `json:"box"`
	Pixels int `json:"pixels"`
}

type Result struct {
	MatchPercent float64
	Mismatched   int
	Compared     int
	Regions      []Region
	// Diff shows the design faded with differing pixels in red.
	Diff *image.RGBA
}

// Compare compares the overlapping area of design and actual, pixel by
// pixel; a pixel differs when any channel differs by more than threshold
// after both are flattened onto white.
func Compare(design, actual image.Image, threshold uint8) Result {
	d, a := flatten(design), flatten(actual)
	w := min(d.Bounds().Dx(), a.Bounds().Dx())
	h := min(d.Bounds().Dy(), a.Bounds().Dy())
	diff := image.NewRGBA(d.Bounds())
	gw, gh := (w+cell-1)/cell, (h+cell-1)/cell
	grid := make([]int, gw*gh)
	mismatched := 0
	for y := 0; y < d.Bounds().Dy(); y++ {
		for x := 0; x < d.Bounds().Dx(); x++ {
			dp := d.RGBAAt(x, y)
			if x >= w || y >= h {
				diff.SetRGBA(x, y, color.RGBA{255, 160, 0, 255})
				continue
			}
			if differs(dp, a.RGBAAt(x, y), threshold) {
				mismatched++
				grid[(y/cell)*gw+x/cell]++
				diff.SetRGBA(x, y, color.RGBA{230, 0, 40, 255})
				continue
			}
			diff.SetRGBA(x, y, fade(dp))
		}
	}
	compared := w * h
	match := 100.0
	if compared > 0 {
		match = float64(compared-mismatched) * 100 / float64(compared)
	}
	return Result{MatchPercent: match, Mismatched: mismatched, Compared: compared, Regions: regions(grid, gw, gh, w, h), Diff: diff}
}

func differs(p, q color.RGBA, threshold uint8) bool {
	return absDiff(p.R, q.R) > threshold || absDiff(p.G, q.G) > threshold || absDiff(p.B, q.B) > threshold
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}

func fade(c color.RGBA) color.RGBA {
	mix := func(v uint8) uint8 { return uint8(int(v)/4 + 255*3/4) }
	return color.RGBA{mix(c.R), mix(c.G), mix(c.B), 255}
}

// flatten draws img onto white, so transparent areas compare as the page
// they will sit on.
func flatten(img image.Image) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Over)
	return out
}

// regions merges adjacent grid cells with differences into boxes, largest
// first.
func regions(grid []int, gw, gh, w, h int) []Region {
	seen := make([]bool, len(grid))
	var out []Region
	for start := range grid {
		if grid[start] == 0 || seen[start] {
			continue
		}
		minX, minY, maxX, maxY, pixels := gw, gh, -1, -1, 0
		stack := []int{start}
		seen[start] = true
		for len(stack) > 0 {
			i := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			x, y := i%gw, i/gw
			pixels += grid[i]
			minX, minY, maxX, maxY = min(minX, x), min(minY, y), max(maxX, x), max(maxY, y)
			for _, n := range [][2]int{{x - 1, y}, {x + 1, y}, {x, y - 1}, {x, y + 1}} {
				if n[0] < 0 || n[1] < 0 || n[0] >= gw || n[1] >= gh {
					continue
				}
				j := n[1]*gw + n[0]
				if grid[j] > 0 && !seen[j] {
					seen[j] = true
					stack = append(stack, j)
				}
			}
		}
		x0, y0 := minX*cell, minY*cell
		out = append(out, Region{Box: Box{X: x0, Y: y0, W: min((maxX+1)*cell, w) - x0, H: min((maxY+1)*cell, h) - y0}, Pixels: pixels})
	}
	slices.SortFunc(out, func(a, b Region) int { return b.Pixels - a.Pixels })
	if len(out) > maxRegions {
		out = out[:maxRegions]
	}
	return out
}
