package main

// main.go

import (
	"fmt"
	"github.com/go-gl/gl/v3.3-core/gl"
	"github.com/go-gl/glfw/v3.3/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"golang.org/x/image/font/basicfont"
	"log"
	"math"
	"math/bits"
	"runtime"
	"time"
)

func init() {
	runtime.LockOSThread()
}

// ---------------------------------------------------------------------------------------------------------------------

// panic on the unrecoverable
func assert(cond bool, msg string) {
	if !cond {
		panic(msg)
	}
}

const (
	debugOverlayEnabled     = true
	debugOverlayOnByDefault = false
	slowFrameLogging        = false
	maxSlowLogs             = 64

	// Prevent runaway frame time.
	maxDeltaTime = 250 * time.Millisecond

	// Minimum time reserved for spinning before the deadline.
	minSpinMargin = 200 * time.Microsecond

	// Spin margin target: mean overshoot plus this many standard deviations.
	spinMarginSigmas = 2

	// Exponential moving average weight for the latest sample.
	spinMarginAlpha = 1.0 / 64.0

	// Limits spin time to at most 1/minFrameSlacks of frame slack.
	minFrameSlacks = 2
)

var (
	// maxFPS == 0 turns frame pacing off
	maxFPS = defaultRate
	maxTPS = defaultRate
)

// spin floor fits under the budget ceiling (frameBudget / minFrameSlacks) at the tightest rate (maxPreset)
const _ = uint64(time.Second/maxPreset - minFrameSlacks*minSpinMargin)

// maxDeltaTime covers at least one tick period at the slowest rate (minPreset)
const _ = uint64(maxDeltaTime*minPreset - time.Second)

const (
	CellSize   = 100
	WindowSize = GridSize * CellSize
)

func main() {

	if err := glfw.Init(); err != nil {
		log.Fatalf("glfw init: %v", err)
	}
	defer glfw.Terminate()

	glfw.WindowHint(glfw.ContextVersionMajor, 3)
	glfw.WindowHint(glfw.ContextVersionMinor, 3)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True) // required on macOS

	// Nothing uses depth or stencil, GLFW requests 24/8 by default.
	glfw.WindowHint(glfw.DepthBits, 0)
	glfw.WindowHint(glfw.StencilBits, 0)

	// unscaled so projection and mouse-to-grid mapping agree.
	glfw.WindowHint(glfw.Resizable, glfw.False)
	glfw.WindowHint(glfw.CocoaRetinaFramebuffer, glfw.False)
	glfw.WindowHint(glfw.ScaleToMonitor, glfw.False)

	win, err := glfw.CreateWindow(WindowSize, WindowSize, "Shikaku", nil, nil)
	if err != nil {
		log.Fatalf("create window: %v", err)
	}
	win.MakeContextCurrent()

	glfw.SwapInterval(0)

	if err = gl.Init(); err != nil {
		log.Fatalf("gl init: %v", err)
	}

	fbW, fbH := win.GetFramebufferSize()
	if fbW != WindowSize || fbH != WindowSize {
		log.Printf("warning: framebuffer is %dx%d, expected %dx%d -- "+
			"HiDPI scaling is still active and mouse coordinates will drift",
			fbW, fbH, WindowSize, WindowSize)
	}

	// See renderer.go's coordinate block before making this a partial framebuffer.
	gl.Viewport(0, 0, int32(fbW), int32(fbH))

	renderer, err := NewRenderer()
	if err != nil {
		log.Fatalf("renderer: %v", err)
	}
	defer func() {
		gl.DeleteBuffers(1, &renderer.vbo)
		gl.DeleteVertexArrays(1, &renderer.vao)
		gl.DeleteProgram(renderer.prog)
	}()

	renderer.SetProjection(mgl32.Ortho2D(0, WindowSize, WindowSize, 0))

	font := NewFontAtlas(basicfont.Face7x13)
	defer func() {
		gl.DeleteTextures(1, &font.tex)
	}()
	font.scale = 2

	game := NewGame(renderer, font, NewInput(win), debugOverlayOnByDefault)

	win.SetKeyCallback(func(_ *glfw.Window, key glfw.Key, _ int,
		action glfw.Action, _ glfw.ModifierKey) {
		if action != glfw.Press {
			return
		}
		switch key {
		case glfw.KeyEscape:
			game.settingsOpen = !game.settingsOpen
			game.dragging = false
		case glfw.KeyF3:
			//noinspection GoBoolExpressions
			if debugOverlayEnabled {
				game.showDebug = !game.showDebug
			}
		}
	})

	runGameLoop(win, game)
}

func runGameLoop(win *glfw.Window, game *Game) {
	var (
		tickAccumulator   float64
		prevFrameStart    = time.Now()
		statsWindowStart  = time.Now()
		nextFrameDeadline = time.Now()
		framesInWindow    int
		ticksInWindow     int

		lastWaitLate time.Duration

		wasPaced bool
	)

	for !win.ShouldClose() {
		frameStart := time.Now()

		secondsPerTick := 1.0 / float64(maxTPS)
		var frameBudget time.Duration
		if maxFPS > 0 {
			frameBudget = time.Second / time.Duration(maxFPS)
			spinFloor = minSpinMargin
			spinCeiling = frameBudget / minFrameSlacks
		}
		paced := frameBudget > 0
		if paced && !wasPaced {
			nextFrameDeadline = frameStart
		}
		wasPaced = paced

		dt := frameStart.Sub(prevFrameStart)
		prevFrameStart = frameStart
		dt = min(dt, maxDeltaTime)

		pollStart := time.Now()
		glfw.PollEvents()
		pollBlocked := time.Since(pollStart)

		game.input.SampleCursor()

		tickStart := time.Now()
		tickAccumulator += dt.Seconds()
		ticksThisIter := 0
		for tickAccumulator >= secondsPerTick {
			game.input.PumpButtons()
			game.Update()
			tickAccumulator -= secondsPerTick
			ticksInWindow++
			ticksThisIter++
		}
		tickTime := time.Since(tickStart)

		drawStart := time.Now()
		game.Draw()
		drawTime := time.Since(drawStart)

		swapStart := time.Now()
		win.SwapBuffers()
		swapBlocked := time.Since(swapStart)
		framesInWindow++

		//noinspection GoBoolExpressions
		if slowFrameLogging && frameBudget > 0 && slowLogsWritten < maxSlowLogs {
			if work := pollBlocked + tickTime + drawTime + swapBlocked; work > frameBudget {
				slowLogsWritten++
				log.Printf("slow frame at %.3fs: work %v = poll %v + ticks %d in %v"+
					" + draw %v + swap %v",
					time.Since(processStart).Seconds(), work, pollBlocked,
					ticksThisIter, tickTime, drawTime, swapBlocked)
				if slowLogsWritten == maxSlowLogs {
					log.Printf("slow frame log full at %d lines", maxSlowLogs)
				}
			}
		}

		if elapsed := time.Since(statsWindowStart); elapsed >= time.Second {
			secs := elapsed.Seconds()
			fps := float64(framesInWindow) / secs
			tps := float64(ticksInWindow) / secs
			win.SetTitle(fmt.Sprintf("Shikaku - FPS: %.1f, TPS: %.1f - ESC", fps, tps))
			//noinspection GoBoolExpressions
			if debugOverlayEnabled {
				game.setDebugStats()
			}
			framesInWindow, ticksInWindow = 0, 0
			statsWindowStart = time.Now()
		}

		if frameBudget == 0 {
			continue
		}

		nextFrameDeadline = nextFrameDeadline.Add(frameBudget)
		now := time.Now()

		waitLate, resync := lastWaitLate, pacingResyncRequested
		lastWaitLate, pacingResyncRequested = 0, false

		switch {
		case now.After(nextFrameDeadline.Add(frameBudget)):
			switch {
			case pollBlocked >= frameBudget:
				// OS blocked the pump; report as BLOCK.
				pacing.pollBlockedWorst = max(pacing.pollBlockedWorst, pollBlocked)

			case swapBlocked >= frameBudget:
				// Driver or compositor blocked the present.
				pacing.swapBlockedWorst = max(pacing.swapBlockedWorst, swapBlocked)

			case waitLate >= frameBudget:
				// Already accounted for as WAIT.

			case resync:
				// Puzzle generation, not frame work.

			default:
				pacing.framesDropped++
			}
			nextFrameDeadline = now

		case now.After(nextFrameDeadline):
			// Work missed the deadline; puzzle generation is excluded
			if !resync {
				pacing.workOverrunWorst = max(pacing.workOverrunWorst, now.Sub(nextFrameDeadline))
			}

		default:
			lastWaitLate = waitUntil(nextFrameDeadline)
			pacing.waitOverrunWorst = max(pacing.waitOverrunWorst, lastWaitLate)
		}
	}
}

// ---------------------------------------------------------------------------------------------------------------------

var pacing struct {
	workOverrunWorst time.Duration
	waitOverrunWorst time.Duration
	pollBlockedWorst time.Duration
	swapBlockedWorst time.Duration
	framesDropped    int
}

var pacingResyncRequested bool

// Uses uptime so slow frames align with GODEBUG=gctrace=1 timestamps.
var (
	processStart    = time.Now()
	slowLogsWritten int
)

func requestPacingResync() {
	pacingResyncRequested = true
}

var (
	spinMean, spinVariance float64 // nanoseconds
	spinSeeded             bool
	spinFloor, spinCeiling time.Duration
)

func observeSpinMargin(overshoot time.Duration) {
	x := float64(overshoot)
	if !spinSeeded {
		spinMean, spinSeeded = x, true
		return
	}
	delta := x - spinMean
	spinMean += spinMarginAlpha * delta
	spinVariance += spinMarginAlpha * (delta*delta - spinVariance)
}

func spinMarginValue() time.Duration {
	margin := time.Duration(spinMean + spinMarginSigmas*math.Sqrt(spinVariance))
	if margin < spinFloor {
		return spinFloor
	}
	if margin > spinCeiling {
		return spinCeiling
	}
	return margin
}

func waitUntil(deadline time.Time) (late time.Duration) {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return -remaining
		}
		if safe := spinMarginValue(); remaining > safe {
			requested := remaining - safe
			start := time.Now()
			time.Sleep(requested)
			observeSpinMargin(time.Since(start) - requested)
			continue
		}
		// Busy-wait intentionally: Gosched would introduce a scheduler round trip.
		// Async preemption can still interrupt the loop for GC.
	}
}

// ---------------------------------------------------------------------------------------------------------------------

func seedRNG(seed uint64) uint64 {
	if seed == 0 {
		// Zero is an absorbing state for xorshift.
		seed = 0x9E3779B97F4A7C15
	}
	return seed
}

var rngState = seedRNG(uint64(time.Now().UnixNano()))

// Marsaglia's xorshift64.
func nextRandom() uint64 {
	x := rngState
	x ^= x << 13
	x ^= x >> 7
	x ^= x << 17
	rngState = x
	return x * 0x2545F4914F6CDD1D
}

func randInt(n int) int {
	if n <= 1 {
		return 0
	}
	// Lemire's nearly-divisionless method: multiply a random word by n as a 128-bit
	// product and take the high half. Only the rare bad case needs the modulo.
	high, low := bits.Mul64(nextRandom(), uint64(n))
	if low < uint64(n) {
		threshold := -uint64(n) % uint64(n)
		for low < threshold {
			high, low = bits.Mul64(nextRandom(), uint64(n))
		}
	}
	return int(high)
}

func randFloat64() float64 {
	const (
		float64Mantissa = 53
		discardedBits   = 64 - float64Mantissa
	)
	return float64(nextRandom()>>discardedBits) * (1.0 / (1 << float64Mantissa))
}
