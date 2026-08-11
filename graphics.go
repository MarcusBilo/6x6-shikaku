package main

// graphics.go

// Coordinate systems
//
// This renderer uses pixel space with a top-left origin and Y growing down.
// SetProjection's mgl32.Ortho2D(0, w, h, 0) folds the Y flip into the matrix,
// matching GLFW's top-left, Y-down cursor coordinates.
//
// Consequences:
// 1. CellCoords/CellRect use the same top-left origin as grid[y][x].
// 2. Textures rendered through this projection are vertically flipped relative
//    to CPU-uploaded data: flipV is true for render targets, false for the font atlas.
// 3. OpenGL calls outside the projection (viewport, scissor, readback) retain
//    GL's bottom-left origin. For top-left rects, convert with:
//    glY = surfaceHeight - topY - rectHeight.
// 4. One unit equals one pixel only when the framebuffer matches WindowSize.
//    Pixel centres are then at half-integers; use integer bounds for
//    one-pixel-wide geometry.

import (
	"fmt"
	"image"
	"strings"
	"unsafe"

	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/mathgl/mgl32"
	"golang.org/x/image/font/basicfont"
)

// ---------------------------------------------------------------------------------------------------------------------

type Color = mgl32.Vec4

var (
	ColorWhite = Color{1, 1, 1, 1}
	ColorBlack = Color{0, 0, 0, 1}
)

const (
	floatsPerVertex = 4
	vertsPerQuad    = 6
	maxQuads        = 192
	_               = uint(maxQuads - ((4 * GridSize * GridSize) + 4) - 1) // one draw call per state group
	maxFlushFloats  = maxQuads * vertsPerQuad * floatsPerVertex
	ringSegments    = 4
	ringFloats      = maxFlushFloats * ringSegments
	ringBytes       = ringFloats * 4
)

type batchState struct {
	mode  int32
	color Color
	tex   uint32
}

type Renderer struct {
	prog uint32
	vao  uint32
	vbo  uint32

	locProj  int32
	locMode  int32
	locColor int32
	locTex   int32

	pendingVerts []float32
	state        batchState
	ringPos      int

	proj mgl32.Mat4
}

func NewRenderer() (*Renderer, error) {
	prog, err := linkProgram(vertexShaderSrc, fragmentShaderSrc)
	if err != nil {
		return nil, err
	}

	r := &Renderer{
		prog:         prog,
		pendingVerts: make([]float32, 0, maxFlushFloats),
	}

	r.locProj = gl.GetUniformLocation(prog, gl.Str("uProj\x00"))
	r.locMode = gl.GetUniformLocation(prog, gl.Str("uMode\x00"))
	r.locColor = gl.GetUniformLocation(prog, gl.Str("uColor\x00"))
	r.locTex = gl.GetUniformLocation(prog, gl.Str("uTex\x00"))

	// A dead or misspelt uniform gives -1, and glUniform* on -1 is a no-op with no GL error.
	assert(r.locProj >= 0 && r.locMode >= 0 && r.locColor >= 0 && r.locTex >= 0,
		"renderer: shader uniform not found -- name typo in GetUniformLocation")

	gl.GenVertexArrays(1, &r.vao)
	gl.BindVertexArray(r.vao)

	gl.GenBuffers(1, &r.vbo)
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, ringBytes, nil, gl.STREAM_DRAW)

	stride := int32(floatsPerVertex * 4)
	gl.EnableVertexAttribArray(0)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, stride, 0)
	gl.EnableVertexAttribArray(1)
	gl.VertexAttribPointerWithOffset(1, 2, gl.FLOAT, false, stride, 2*4)

	gl.UseProgram(prog)
	gl.Uniform1i(r.locTex, 0)

	gl.Disable(gl.DEPTH_TEST)
	gl.Disable(gl.BLEND)

	// Flush assumes the program and VAO remain bound for the renderer's lifetime.
	assertBound(gl.CURRENT_PROGRAM, r.prog, "program")
	assertBound(gl.VERTEX_ARRAY_BINDING, r.vao, "VAO")

	return r, nil

}

func assertBound(pname uint32, want uint32, what string) {
	var got int32
	gl.GetIntegerv(pname, &got)
	assert(uint32(got) == want,
		"renderer: "+what+" not bound after NewRenderer -- Flush assumes it stays bound")
}

// Must stay in lockstep with the uMode branches in fragmentShaderSrc
const (
	modeFlat    int32 = 0
	modeGlyph   int32 = 1
	modeTexture int32 = 2
)

func (r *Renderer) SetProjection(m mgl32.Mat4) {
	r.Flush()
	r.proj = m // &r.proj[0] points into the Renderer, so m does not escape
	gl.UniformMatrix4fv(r.locProj, 1, false, &r.proj[0])
}

func (r *Renderer) FillRect(x, y, w, h float32, col Color) {
	r.setState(batchState{mode: modeFlat, color: col})
	r.appendQuad(x, y, w, h, 0, 0, 0, 0)
}

func (r *Renderer) StrokeRect(x, y, w, h, thickness float32, col Color) {
	assert(thickness > 0, "renderer: stroke with no thickness")
	assert(w >= thickness && h >= thickness,
		"renderer: stroke thicker than the rect it outlines")

	half := thickness / 2
	r.FillRect(x-half, y-half, w+thickness, thickness, col)   // top
	r.FillRect(x-half, y+h-half, w+thickness, thickness, col) // bottom
	r.FillRect(x-half, y+half, thickness, h-thickness, col)   // left
	r.FillRect(x+w-half, y+half, thickness, h-thickness, col) // right
}

// See consequence 2 of the coordinate block.
func (r *Renderer) DrawTexture(x, y, w, h float32, tex uint32, flipV bool) {
	v0, v1 := float32(0), float32(1)
	if flipV {
		v0, v1 = 1, 0
	}
	r.setState(batchState{mode: modeTexture, color: ColorWhite, tex: tex})
	r.appendQuad(x, y, w, h, 0, v0, 1, v1)
}

func (r *Renderer) Clear(c Color) {
	assert(len(r.pendingVerts) == 0,
		"renderer: Clear with geometry queued -- it would draw over the clear")

	gl.ClearColor(c[0], c[1], c[2], c[3])
	gl.Clear(gl.COLOR_BUFFER_BIT)
}

func (r *Renderer) setState(next batchState) {
	if next != r.state {
		r.Flush()
		r.state = next
	}
}

func (r *Renderer) setGlyphState(col Color, tex uint32) {
	r.setState(batchState{mode: modeGlyph, color: col, tex: tex})
}

func (r *Renderer) appendQuad(x, y, w, h, u0, v0, u1, v1 float32) {
	if len(r.pendingVerts)+vertsPerQuad*floatsPerVertex > cap(r.pendingVerts) {
		r.Flush()
	}

	x1, y1 := x+w, y+h
	r.pendingVerts = append(r.pendingVerts,
		x, y, u0, v0,
		x1, y, u1, v0,
		x1, y1, u1, v1,

		x, y, u0, v0,
		x1, y1, u1, v1,
		x, y1, u0, v1,
	)

	assert(cap(r.pendingVerts) == maxFlushFloats,
		"renderer: pendingVerts reallocated -- appendQuad let the batch overrun")
}

func (r *Renderer) Flush() {
	floatCount := len(r.pendingVerts)
	if floatCount == 0 {
		return
	}

	assert(r.state.mode >= modeFlat && r.state.mode <= modeTexture,
		"renderer: batch mode has no branch in the fragment shader")
	assert(r.state.mode == modeFlat || r.state.tex != 0,
		"renderer: textured batch with no texture bound")

	// Rebind each flush to avoid writing into another buffer.
	gl.BindBuffer(gl.ARRAY_BUFFER, r.vbo)

	if r.ringPos+floatCount > ringFloats {
		gl.BufferData(gl.ARRAY_BUFFER, ringBytes, nil, gl.STREAM_DRAW)
		r.ringPos = 0
	}
	assert(r.ringPos+floatCount <= ringFloats, "renderer: upload overruns the ring")

	byteOff := r.ringPos * 4
	byteLen := floatCount * 4

	// Advance through a ring buffer to avoid repeatedly overwriting in-use data.
	// unsafe.Pointer, not gl.Ptr: gl.Ptr boxes into interface{} and reflects each call.
	gl.BufferSubData(gl.ARRAY_BUFFER, byteOff, byteLen, unsafe.Pointer(&r.pendingVerts[0]))

	gl.Uniform1i(r.locMode, r.state.mode)
	c := r.state.color
	gl.Uniform4f(r.locColor, c[0], c[1], c[2], c[3])
	if r.state.mode != modeFlat {
		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, r.state.tex)
	}

	gl.DrawArrays(gl.TRIANGLES,
		int32(r.ringPos/floatsPerVertex),
		int32(floatCount/floatsPerVertex))

	r.ringPos += floatCount
	r.pendingVerts = r.pendingVerts[:0]
}

const vertexShaderSrc = `#version 330 core
layout (location = 0) in vec2 aPos;
layout (location = 1) in vec2 aUV;

uniform mat4 uProj;

out vec2 vUV;

void main() {
	gl_Position = uProj * vec4(aPos, 0.0, 1.0);
	vUV = aUV;
}
` + "\x00"

const fragmentShaderSrc = `#version 330 core
in vec2 vUV;

out vec4 fragColor;

uniform int       uMode;  // 0 = flat colour, 1 = glyph mask, 2 = RGB texture
uniform vec4      uColor;
uniform sampler2D uTex;

void main() {
	if (uMode == 0) {
		fragColor = uColor;
	} else if (uMode == 1) {
		// GL_ALPHA is not valid in the core profile, so the font mask arrives as GL_R8
		// and the shader samples it from red. It uses a threshold with discard, not a
		// blend. The text is hard-edged either way while GL_BLEND is off, and discard
		// keeps this branch independent of the pipeline state.
		if (texture(uTex, vUV).r < 0.5) {
			discard;
		}
		fragColor = uColor;
	} else {
		fragColor = vec4(texture(uTex, vUV).rgb, 1.0);
	}
}
` + "\x00"

func compileShader(src string, shaderType uint32) (uint32, error) {
	shader := gl.CreateShader(shaderType)

	csrc, free := gl.Strs(src)
	gl.ShaderSource(shader, 1, csrc, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLen int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLen)
		infoLog := strings.Repeat("\x00", int(logLen)+1)
		gl.GetShaderInfoLog(shader, logLen, nil, gl.Str(infoLog))
		gl.DeleteShader(shader)
		return 0, fmt.Errorf("compile shader: %s", infoLog)
	}
	return shader, nil
}

func linkProgram(vertSrc, fragSrc string) (uint32, error) {
	vs, err := compileShader(vertSrc, gl.VERTEX_SHADER)
	if err != nil {
		return 0, err
	}
	fs, err := compileShader(fragSrc, gl.FRAGMENT_SHADER)
	if err != nil {
		gl.DeleteShader(vs)
		return 0, err
	}

	prog := gl.CreateProgram()
	gl.AttachShader(prog, vs)
	gl.AttachShader(prog, fs)
	gl.LinkProgram(prog)

	gl.DeleteShader(vs)
	gl.DeleteShader(fs)

	var status int32
	gl.GetProgramiv(prog, gl.LINK_STATUS, &status)
	if status == gl.FALSE {
		var logLen int32
		gl.GetProgramiv(prog, gl.INFO_LOG_LENGTH, &logLen)
		infoLog := strings.Repeat("\x00", int(logLen)+1)
		gl.GetProgramInfoLog(prog, logLen, nil, gl.Str(infoLog))
		gl.DeleteProgram(prog)
		return 0, fmt.Errorf("link program: %s", infoLog)
	}
	return prog, nil
}

// ---------------------------------------------------------------------------------------------------------------------

type FontAtlas struct {
	tex  uint32
	face *basicfont.Face
	// The whole atlas texture, not one glyph: glyphV divides by atlasH for the V coordinate.
	atlasW, atlasH int
	// a fractional scale enlarges unevenly.
	scale float32
}

func NewFontAtlas(face *basicfont.Face) *FontAtlas {
	bounds := face.Mask.Bounds()
	w, h := bounds.Dx(), bounds.Dy()

	// These fail silently at draw time: a U/V outside [0,1] is not a GL error;
	// CLAMP_TO_EDGE samples the nearest edge texel, so text comes out wrong with no log.
	assert(face.Width > 0 && face.Height > 0, "font: face has no glyph box")
	assert(w >= face.Width, "font: mask is narrower than one glyph")
	assert(face.Advance >= face.Width,
		"font: advance is narrower than a glyph -- text would measure narrower than it draws")
	for _, rng := range face.Ranges {
		if rng.High <= rng.Low {
			continue
		}
		lastRow := (int(rng.High-1-rng.Low) + rng.Offset + 1) * face.Height
		assert(lastRow <= h, "font: face range runs off the end of the mask")
	}

	maskPixels := make([]uint8, w*h)
	if alpha, ok := face.Mask.(*image.Alpha); ok {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				maskPixels[y*w+x] = alpha.AlphaAt(bounds.Min.X+x, bounds.Min.Y+y).A
			}
		}
	} else {
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				_, _, _, a := face.Mask.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				maskPixels[y*w+x] = uint8(a >> 8)
			}
		}
	}

	f := &FontAtlas{face: face, atlasW: w, atlasH: h, scale: 1}

	gl.GenTextures(1, &f.tex)
	// On failure GenTextures leaves the name 0 and reports nothing
	assert(f.tex != 0, "font: glGenTextures produced no texture name")
	gl.BindTexture(gl.TEXTURE_2D, f.tex)

	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 1)

	// GL_ALPHA is not in the core profile, so the mask is R8, sampled from red. The
	// top row uploads first, so v grows down: glyph quads need no V flip, FBO textures do.
	gl.TexImage2D(gl.TEXTURE_2D, 0, int32(gl.R8), int32(w), int32(h), 0,
		gl.RED, gl.UNSIGNED_BYTE, gl.Ptr(maskPixels))

	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)

	gl.PixelStorei(gl.UNPACK_ALIGNMENT, 4)

	return f
}

func (f *FontAtlas) glyphV(ch rune) (v0, v1 float32, ok bool) {
	for _, rng := range f.face.Ranges {
		if ch < rng.Low || rng.High <= ch {
			continue
		}
		row := (int(ch-rng.Low) + rng.Offset) * f.face.Height
		return float32(row) / float32(f.atlasH),
			float32(row+f.face.Height) / float32(f.atlasH), true
	}
	return 0, 0, false
}

// every drawn glyph is ASCII, so byte i is the whole rune with no UTF-8 decode.
type textSource interface{ ~string | ~[]byte }

func drawGlyphs[T textSource](f *FontAtlas, gfx *Renderer, text T, x, y int, col Color, scale float32) {
	face := f.face
	glyphUWidth := float32(face.Width) / float32(f.atlasW)

	glyphW := float32(face.Width) * scale
	glyphH := float32(face.Height) * scale
	advance := float32(face.Advance) * scale
	leftBearing := float32(face.Left) * scale
	ascent := float32(face.Ascent) * scale

	penX := float32(x) + leftBearing
	lineTop := float32(y) - ascent

	gfx.setGlyphState(col, f.tex)
	for i := 0; i < len(text); i++ {
		ch := rune(text[i])
		if ch == '\n' {
			penX = float32(x) + leftBearing
			lineTop += glyphH
			continue
		}
		v0, v1, ok := f.glyphV(ch)
		if !ok {
			if v0, v1, ok = f.glyphV('\ufffd'); !ok {
				penX += advance
				continue
			}
		}
		gfx.appendQuad(penX, lineTop, glyphW, glyphH, 0, v0, glyphUWidth, v1)
		penX += advance
	}
}

// DrawTextCentered assumes a monospaced font where every glyph has the same advance.
func (f *FontAtlas) DrawTextCentered(gfx *Renderer, s string, centerX, y int, col Color) {
	widest, line := 0, 0

	for _, ch := range s {
		if ch == '\n' {
			if line > widest {
				widest = line
			}
			line = 0
			continue
		}
		line++
	}

	if line > widest {
		widest = line
	}

	textWidth := int(float32(widest*f.face.Advance) * f.scale)

	drawGlyphs(f, gfx, s, centerX-textWidth/2, y, col, f.scale)
}
