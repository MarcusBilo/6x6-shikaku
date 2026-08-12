package main

// screen_play.go

import (
	"fmt"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/mathgl/mgl32"
	"log"
	"strconv"
)

func (g *Game) updatePlay() {
	in := g.input

	if g.waitingForRelease {
		if !in.LeftPressed() {
			g.waitingForRelease = false
		}
		return
	}

	// where the player clicked, not where the pointer was at the end of the poll batch.
	if px, py, ok := in.LeftPressEvent(); ok {
		g.dragStart = cellAt(px, py)
		g.dragging = true
	}

	if px, py, ok := in.LeftReleaseEvent(); ok {
		if g.dragging {
			g.toggleRect(normalizeRect(g.dragStart, cellAt(px, py)))
			g.solved = g.checkSolved()
		}
		g.dragging = false
	}
}

// cellAt is the only pixel-to-cell conversion in the program + clamp to board
func cellAt(px, py int) CellCoords {
	return CellCoords{
		x: max(0, min(px/CellSize, GridSize-1)),
		y: max(0, min(py/CellSize, GridSize-1)),
	}
}

func normalizeRect(a, b CellCoords) CellRect {
	return CellRect{
		topLeft:     CellCoords{x: min(a.x, b.x), y: min(a.y, b.y)},
		bottomRight: CellCoords{x: max(a.x, b.x), y: max(a.y, b.y)},
	}
}

func (g *Game) toggleRect(r CellRect) {
	normalized := r.topLeft.x <= r.bottomRight.x && r.topLeft.y <= r.bottomRight.y
	assert(normalized, "game: rect is not normalized")
	onBoard := r.topLeft.x >= 0 && r.topLeft.y >= 0 && r.bottomRight.x < GridSize && r.bottomRight.y < GridSize
	assert(onBoard, "game: rect is not on the board")

	for i, existing := range g.rects {
		if existing == r {
			g.rects = append(g.rects[:i], g.rects[i+1:]...)
			return
		}
	}

	for _, existing := range g.rects {
		if rectsOverlap(existing, r) {
			return
		}
	}

	g.rects = append(g.rects, r)
}

func rectsOverlap(a, b CellRect) bool {
	return a.topLeft.x <= b.bottomRight.x &&
		a.bottomRight.x >= b.topLeft.x &&
		a.topLeft.y <= b.bottomRight.y &&
		a.bottomRight.y >= b.topLeft.y
}

func (g *Game) checkSolved() bool {
	for i := range g.rects {
		for j := i + 1; j < len(g.rects); j++ {
			assert(!rectsOverlap(g.rects[i], g.rects[j]),
				"game: rects overlap -- checkSolved's area-as-coverage argument is void")
		}
	}

	if len(g.rects) == 0 || len(g.rects) != len(g.clues) {
		return false
	}

	totalArea := 0
	for _, r := range g.rects {
		area := (r.bottomRight.x - r.topLeft.x + 1) * (r.bottomRight.y - r.topLeft.y + 1)
		totalArea += area

		cluesInside := 0
		clueArea := 0
		for _, c := range g.clues {
			// col is x, row is y -- the one place the rect's axes meet the
			// clue's, so a swapped axis here would still look correct.
			if c.col >= r.topLeft.x && c.col <= r.bottomRight.x &&
				c.row >= r.topLeft.y && c.row <= r.bottomRight.y {
				cluesInside++
				clueArea = c.area
			}
		}

		if cluesInside != 1 || clueArea != area {
			return false
		}
	}

	return totalArea == GridSize*GridSize
}

func (g *Game) drawPlay() {
	gfx := g.gfx

	gfx.Clear(ColorWhite)
	gfx.DrawTexture(0, 0, WindowSize, WindowSize, g.staticLayer.tex, true)

	for _, rect := range g.rects {
		drawPlayerRect(gfx, rect)
	}

	if g.dragging {
		in := g.input
		pixelX, pixelY := in.cursorX, in.cursorY
		drawPlayerRect(gfx, normalizeRect(g.dragStart, cellAt(pixelX, pixelY)))
	}
}

func drawPlayerRect(gfx *Renderer, rect CellRect) {

	const (
		rectInset  = 6
		rectStroke = 4
	)

	// The stroke is centred, so the border takes rectStroke/2 per side and leaves w-rectStroke
	const _ = uint64(CellSize - rectInset*2 - rectStroke - 1)

	onBoard := rect.topLeft.x >= 0 && rect.topLeft.y >= 0 && rect.bottomRight.x < GridSize && rect.bottomRight.y < GridSize
	assert(onBoard, "draw: player rect outside the board")

	x := float32(rect.topLeft.x*CellSize) + rectInset
	y := float32(rect.topLeft.y*CellSize) + rectInset
	w := float32((rect.bottomRight.x-rect.topLeft.x+1)*CellSize) - rectInset*2
	h := float32((rect.bottomRight.y-rect.topLeft.y+1)*CellSize) - rectInset*2

	gfx.StrokeRect(x, y, w, h, rectStroke, ColorBlack)
}

func (g *Game) cacheStaticLayer() {

	const gridLineWidth = float32(1)

	if g.staticLayer == nil {
		rt, err := NewRenderTarget(WindowSize, WindowSize)
		if err != nil {
			log.Fatalf("static layer: %v", err)
		}
		g.staticLayer = rt
	}

	gfx := g.gfx
	font := g.font
	layer := g.staticLayer

	layer.Begin(gfx)
	gfx.Clear(ColorWhite)

	for i := 0; i <= GridSize; i++ {
		linePos := float32(i * CellSize)
		lineOffset := lineOrigin(linePos, gridLineWidth, WindowSize)
		gfx.FillRect(lineOffset, 0, gridLineWidth, WindowSize, ColorBlack)
		gfx.FillRect(0, lineOffset, WindowSize, gridLineWidth, ColorBlack)
	}

	for y := 0; y < GridSize; y++ {
		for x := 0; x < GridSize; x++ {
			if g.grid[y][x] == 0 {
				continue
			}
			clueText := strconv.Itoa(g.grid[y][x])
			posX := x*CellSize + CellSize/2
			posY := y*CellSize + CellSize/2 + int(float32(font.face.Ascent)*font.scale)/2
			font.DrawTextCentered(gfx, clueText, posX, posY, ColorBlack)
		}
	}

	layer.End(gfx)
}

func lineOrigin(center, thickness, limit float32) float32 {
	start := float32(int(center - thickness/2 + 0.5))
	if start < 0 {
		start = 0
	}
	if start+thickness > limit {
		start = limit - thickness
	}

	assert(start >= 0 && start+thickness <= limit, "draw: line escapes the surface")
	return start
}

type RenderTarget struct {
	fbo, tex uint32
	w, h     int32
	bound    bool
}

// A real error return: framebuffer completeness is a runtime device fact, and the
// status code carries information a panic could not.
func NewRenderTarget(w, h int) (*RenderTarget, error) {
	assert(w > 0 && h > 0, "draw: render target with no area")
	rt := &RenderTarget{w: int32(w), h: int32(h)}

	gl.GenTextures(1, &rt.tex)
	gl.BindTexture(gl.TEXTURE_2D, rt.tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, int32(gl.RGBA8), rt.w, rt.h, 0,
		gl.RGBA, gl.UNSIGNED_BYTE, nil)

	// NEAREST so the grid lines survive the FBO-to-screen sample.
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	gl.GenFramebuffers(1, &rt.fbo)
	gl.BindFramebuffer(gl.FRAMEBUFFER, rt.fbo)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, rt.tex, 0)

	status := gl.CheckFramebufferStatus(gl.FRAMEBUFFER)
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	if status != gl.FRAMEBUFFER_COMPLETE {
		return nil, fmt.Errorf("framebuffer incomplete: 0x%x", status)
	}
	return rt, nil
}

func (rt *RenderTarget) Begin(r *Renderer) {
	assert(!rt.bound, "draw: RenderTarget.Begin without a matching End")
	rt.bound = true

	r.Flush()
	gl.BindFramebuffer(gl.FRAMEBUFFER, rt.fbo)
	// GL's bottom-left origin here
	gl.Viewport(0, 0, rt.w, rt.h)
	r.SetProjection(mgl32.Ortho2D(0, float32(rt.w), float32(rt.h), 0))
}

// End restores the default framebuffer and the window's projection, not the
// projection that was current before Begin.
func (rt *RenderTarget) End(r *Renderer) {
	assert(rt.bound, "draw: RenderTarget.End without a matching Begin")
	rt.bound = false

	r.Flush()
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.Viewport(0, 0, WindowSize, WindowSize)
	r.SetProjection(mgl32.Ortho2D(0, WindowSize, WindowSize, 0))
}
