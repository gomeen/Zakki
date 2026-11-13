//go:build windows

package d3d11

import (
	"errors"
	"fmt"
	"time"
	"unsafe"
)

// Uncapped (MYGO_UNCAPPED, package pacing), a frame presents as soon as it
// is drawn, and the next draws at once: the swap chain is made without the
// frame latency object, so that no frame waits for the screen to have room
// (latency.go), and every frame presents with sync interval 0, none waiting
// for DWM to compose the last (waitComposed) nor, when it changed nothing,
// for the next refresh (waitRefresh). Where the system shows frames torn
// (DXGI_FEATURE_PRESENT_ALLOW_TEARING, Windows 10 1703 on with a driver
// that can), the window's swap chain allows tearing and its frames present
// with it, flipping to the screen mid-refresh once DWM gives the window the
// display (independent flip, as a borderless window covering the screen
// has); a window DWM composes still shows the newest frame at each refresh.
// A swap chain for composition (NewComposed) has no tearing: its frames
// present with sync interval 0 all the same.
//
// While the window changes size, from its first change until the swap
// chain settles (resize, settleDelay), frames present paced
// (presentsUncapped), as without MYGO_UNCAPPED: none with tearing, a frame
// of a new size waiting for DWM to show it (Render, waitShown), the others
// each waiting for the composition that shows it (waitComposed), and one
// that changed nothing for the next refresh (waitRefresh). Uncapped, a live
// resize had one present take 2 s, on a Radeon 780M and a 59 Hz display,
// with the window resized every 50 ms while it animated, and frames then
// came at 92 to 95 a second, about two a composition besides the resize's,
// where paced frames took no more than a refresh. The resize's buffers are
// larger than the window and shown clipped, so DWM composes the window and
// flips none of its frames to the screen torn: frames presented faster than
// DWM composes only queue for it, filling the buffers, while DWM resizes the
// window, and the frame of the next size, which waits for DWM to show it,
// waits behind them. Paced, at most one frame a composition reaches DWM.
// Before ResizeBuffers, no view of the buffers stays bound (unbindBuffers),
// as DXGI asks.
//
// Unlike a paced frame of a new size, which calls DwmFlush on the main
// thread and returns once DWM composed, an uncapped one waits for DWM at
// most two refreshes, DwmFlush called on another thread (awaitRefresh,
// shared with waitComposed's): every wait of an uncapped frame on DWM is
// bounded. With that wait unbounded, a resize the app made with
// SetWindowPos every 50 ms still had one present take 2.0 s, at its first
// frame or after its last change, on the Radeon 780M, though its other
// frames presented paced, while the same resize without MYGO_UNCAPPED,
// and a drag of the window's edge uncapped, had none. The frame of a new size draws
// within SetWindowPos (WM_SIZE), and its DwmFlush was the only wait of an
// uncapped frame with no bound: it returns at DWM's next composition,
// which frames flipped to the screen torn, as the uncapped ones before the
// resize, do not bring about, and DWM may itself wait on the window's
// thread while the window changes size; 2 s exactly looks like the end of
// such a wait. A frame that waits for DWM no longer than two refreshes,
// and then returns to the window's messages, leaves DWM nothing to wait
// on, and a composition missed costs it two refreshes.
//
// Nor does Present itself wait long on the main thread while the window
// changes size. With the frame of a new size's wait bounded, the
// SetWindowPos resize still had one present take 2.0 s in 2 runs of 12 on
// the Radeon 780M, once a second into the resize and once after its last
// change, both of a frame not of a new size that redrew part of the last
// (Present1 with dirty rectangles, sync interval 0, no flags). Such a
// frame's wait for its composition is bounded and off the main thread; on
// the main thread it calls nothing that may wait long but Present and,
// every sweepEvery frames, DwmGetCompositionTimingInfo (readPeriod). Paced,
// a frame presents only once the frame latency object said the swap chain
// has room for it (latency.go), so its Present has nothing to wait for;
// uncapped, there is no such object, and Present waits for room itself:
// for the GPU to finish the frame before (one frame queued at most), which
// waits for DWM to give its buffer back. DWM held up by the resize keeps
// the buffers it holds, a frame drawn once the wait for its composition
// timed out queues behind them, and Present then waits on DWM, which may
// itself wait on the window's thread; that is the likeliest 2 s, though
// nothing measured it. So while the window changes size, uncapped frames
// present with DXGI_PRESENT_DO_NOT_WAIT (presentFlags), with which Present
// returns DXGI_ERROR_WAS_STILL_DRAWING where it would wait. Present is tried
// again for a refresh at most (showSoon), as long as the GPU or DWM take to
// make room unless held up; after that the frame waits, unpresented
// (holdFrame): the main thread goes back to the window's messages, the
// frame after waits for it (Ready), and it presents after the next
// composition, or two refreshes, tried as before (presentHeld). A frame
// drawn meanwhile, as one of a new size within SetWindowPos, takes its
// place, drawn whole as none showed it. A frame of a new size that Present
// refuses for a refresh waits so too, its wait for DWM to show it
// (waitShown) left out, rather than wait in Present within SetWindowPos,
// where DWM may wait on the window's thread: the window shows the last
// frame at its new size until it presents, a composition or two later,
// which WARP on a busy CI runner does too. A frame waits so maxHeld at
// most: without that bound, on the Radeon 780M, 2 runs of 30 SetWindowPos
// resizes held their last frame, and nothing drew for the 2 to 3.5 s the
// app ran on: the frame after waited for it, which DXGI never took with
// the flag, tried after each composition.
//
// A frame held maxHeld presents as it would without the flag, waiting in
// Present as long as DXGI has it, but on another goroutine (presentAside,
// goAside), and after it, until DXGI takes a frame at once with the swap
// chain settled (gaveUp), frames present paced, with the flag, and the
// buffers resize, to settle the swap chain or for a size they do not hold,
// on another goroutine too (resizeAside). While such a call is under way,
// the main thread makes none to the device's context nor the swap chain,
// which it may not while another thread does: it goes back to the
// window's messages, a frame drawn meanwhile, as one of a new size within
// SetWindowPos, is put off, the window showing the last frame, the frame
// after waits for the call (Ready), and Release waits for it to return,
// releaseWait at most, past which it leaves the GPU objects to the
// process's end: DXGI may send the window a message meanwhile.
// Once it returned, the frame after draws, at once if one waits, or once
// the window asks for one (askFrame), as the settle timer's may have been
// put off. DXGI refusing frames was not ended by anything the app did
// without waiting. Giving the held frame up, never presented, and drawing
// the frames after (bac8b96): in the one run of 40 where frames were given
// up on, three in a row after the last change, the frame that then
// settled the swap chain took 994 ms in Render on the main thread, in
// ResizeBuffers or the Present without the flag after it, and frames came
// again after it. Waiting on neither (0338404), as the frame that would
// resize the buffers was put off until DXGI took a frame with the flag,
// probed by presenting the frame last drawn with it, and an event query
// said the GPU drew the frames before: in 4 runs of 70, every frame after
// the give-ups, 167 of them over the 4 s watched, was put off, nothing
// presenting and nothing resizing, the window showing a stale frame. The
// stats could not tell which of the two waits held, as a frame put off
// said the same for both; the code holds no fault in either (the probe
// presents with the flag, and a success clears the wait; the query ended
// after the frames drawn, GetData's S_OK told from S_FALSE), and neither
// had a bound, so what ended them was DXGI or the GPU moving on by
// itself, which they did not. That the probe was refused throughout is
// the likelier, as DXGI refused the held frames for seconds before
// maxHeld; nothing measured it. A call that waits for DXGI, made off the
// main thread, ends that state as the 994 ms frame did, while the window
// keeps taking its messages, which DWM may wait on as the window changes
// size. Nothing can end such a call sooner, and it has no bound of the
// app's; the waits seen, 994 ms and the 2 s presents above, all ended by
// themselves, and the window takes its messages meanwhile. Frame stats
// tell how many times Present said it was still drawing, a frame left to
// present later, a frame put off while a call waits on another thread
// ("put off while Present waits on another thread", or ResizeBuffers),
// the frame after it ("after Present waited on another thread"), and one
// paced until DXGI takes a frame at once. Nor does the main thread read
// the refresh period from DWM uncapped, which may wait for it: another
// goroutine does (readPeriodAside), and none while the window changes
// size, as the period does not change with it. A swap chain that takes no
// DXGI_PRESENT_DO_NOT_WAIT (DXGI_ERROR_INVALID_CALL) presents without it,
// waiting as before (noWaitRefused).
//
// With two buffers, a frame presented with sync interval 0 that DWM has not
// shown yet holds one and the screen the other, and the next frame waits
// for the screen to give its buffer back, at the next refresh: the swap
// chain has three (uncappedBuffers), so that a frame replaces the one
// queued, which DWM drops. The device's frame latency, one frame queued at
// most (init), still keeps the main thread from running ahead of the GPU:
// Present blocks for it, and the GPU's pace is the frames'.
const (
	// swapChainTearing is DXGI_SWAP_CHAIN_FLAG_ALLOW_TEARING, and
	// presentTearing DXGI_PRESENT_ALLOW_TEARING.
	swapChainTearing = 0x800
	presentTearing   = 0x200
	// factory5CheckFeatureSupport is IDXGIFactory5::CheckFeatureSupport,
	// and featureTearing DXGI_FEATURE_PRESENT_ALLOW_TEARING.
	factory5CheckFeatureSupport = 28
	featureTearing              = 0
	// uncappedBuffers is how many buffers an uncapped swap chain has at
	// least.
	uncappedBuffers = 3
	// presentDoNotWait is DXGI_PRESENT_DO_NOT_WAIT, with which Present
	// returns DXGI_ERROR_WAS_STILL_DRAWING rather than wait, and
	// dxgiErrorInvalidCall DXGI_ERROR_INVALID_CALL, which it returns where
	// the flag is not taken.
	presentDoNotWait         = 0x8
	dxgiErrorWasStillDrawing = 0x887a000a
	dxgiErrorInvalidCall     = 0x887a0001
)

// errStillDrawing is what show returns when Present, with
// DXGI_PRESENT_DO_NOT_WAIT, would have waited: the frame is not presented.
var errStillDrawing = errors.New("d3d11: the swap chain has no room for the frame yet")

var iidIDXGIFactory5 = guid{0x7632e1f5, 0xee65, 0x4dca, [8]byte{0x87, 0xfd, 0x84, 0xcd, 0x75, 0xf8, 0x83, 0x8d}}

// tearingSupported reports whether the system shows frames presented with
// tearing, as the factory's IDXGIFactory5 (DXGI 1.5) tells.
func tearingSupported(factory uintptr) bool {
	var f5 uintptr
	if failed(call(factory, 0, uintptr(unsafe.Pointer(&iidIDXGIFactory5)), uintptr(unsafe.Pointer(&f5)))) {
		return false
	}
	defer free(&f5)
	var allow int32 // BOOL
	if failed(call(f5, factory5CheckFeatureSupport, featureTearing, uintptr(unsafe.Pointer(&allow)), unsafe.Sizeof(allow))) {
		return false
	}
	return allow != 0
}

// swapChainFlags returns the flags the swap chain is made and resized
// with: the frame latency object's, or uncapped, tearing's where it
// presents with tearing.
func (r *Renderer) swapChainFlags() uint32 {
	switch {
	case !r.uncapped:
		return swapChainWaitable
	case r.tearing:
		return swapChainTearing
	}
	return 0
}

// presentFlags returns the flags of a present after sync vertical blanks:
// tearing's for one at once (sync 0), where the swap chain allows it and
// frames present uncapped; uncapped, when frames present paced, as while
// the window changes size, DXGI_PRESENT_DO_NOT_WAIT, where the frame can
// present later (post) and the swap chain takes it.
func (r *Renderer) presentFlags(sync uintptr) uintptr {
	switch {
	case sync == 0 && r.tearing && r.presentsUncapped():
		return presentTearing
	case r.uncapped && !r.presentsUncapped() && r.post != nil && !r.noWaitRefused:
		return presentDoNotWait
	}
	return 0
}

// showSoon presents the back buffer with DXGI_PRESENT_DO_NOT_WAIT, trying
// again for at most a refresh while Present would wait, as for the GPU to
// finish the last frame or DWM to give a buffer back, which takes that
// long at most when DWM is not held up; then it returns errStillDrawing,
// and the frame waits (holdFrame).
func (r *Renderer) showSoon(sync uintptr) error {
	deadline := time.Now().Add(r.cost.refreshPeriod())
	for {
		err := r.show(sync)
		if err != errStillDrawing {
			return err
		}
		r.refused++
		if !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(time.Millisecond)
	}
}

// holdFrame returns err, a present's, but for errStillDrawing: the frame
// drawn, which Present refused, then presents after the next composition,
// or a refresh or two, from refreshed (presentHeld), while the main thread
// goes back to the window's messages, and the frame after waits for that
// (Ready). A frame drawn before, as one of a new size within SetWindowPos,
// takes its place (Render), and one that waited too long presents on
// another goroutine (presentHeld).
func (r *Renderer) holdFrame(err error) error {
	if err != errStillDrawing {
		return err
	}
	// The GPU draws the frame meanwhile.
	call(r.ctx, ctxFlush)
	r.unpresented = true
	if r.heldAt.IsZero() {
		r.heldAt = time.Now()
	}
	r.waitComposed()
	return nil
}

// maxHeld is how long a frame waits to present at most (presentHeld);
// tests change it.
var maxHeld = 100 * time.Millisecond

// askFrame has the window ask for a frame, as the settle timer does
// (armSettle); tests replace it.
var askFrame = func(hwnd uintptr) { procInvalidateRect.Call(hwnd, 0, 0) }

// presentHeld presents the frame Present refused (holdFrame), or has it
// wait again, and reports whether the frame after can draw now. A frame
// that waited maxHeld presents without DXGI_PRESENT_DO_NOT_WAIT, on
// another goroutine (presentAside), and the frame after waits for it; until
// DXGI takes a frame at once, frames present paced, and the buffers
// resize on another goroutine too (gaveUp).
func (r *Renderer) presentHeld() bool {
	r.unpresented = false
	if time.Since(r.heldAt) >= maxHeld {
		r.gaveUp = true
		r.presentAside()
		return false
	}
	err := r.present(0)
	if err == nil {
		// The frame after waits for the composition that shows it.
		r.waitComposed()
		return false
	}
	if err = r.holdFrame(err); err == nil {
		return false
	}
	if r.failed == nil {
		r.failed = err // the next frame finds it
	}
	return true
}

// presentsUncapped reports whether frames present without waiting for the
// display: uncapped (MYGO_UNCAPPED), but while the window changes size, or
// after a frame that waited maxHeld until DXGI takes one at once (gaveUp),
// when they present paced.
func (r *Renderer) presentsUncapped() bool {
	return r.uncapped && !r.resizing && !r.gaveUp
}

// errAside is what buffers returns when another goroutine resizes the
// buffers (resizeAside): the frame is put off.
var errAside = errors.New("d3d11: the buffers resize on another thread")

// presentAside presents the frame drawn, which Present refused for
// maxHeld, as it would without DXGI_PRESENT_DO_NOT_WAIT, waiting as long
// as DXGI has it wait, on another goroutine (goAside).
func (r *Renderer) presentAside() {
	sc, flags, params := r.swapChain, r.presentFlags(0)&^presentDoNotWait, r.presentParams()
	r.goAside("Present", func() uintptr { return presentCall(sc, 0, flags, params) }, func(hr uintptr) error {
		if failed(hr) {
			return fmt.Errorf("d3d11: present failed: %#x", uint32(hr))
		}
		r.held = r.ctx1 != 0 && uint32(hr) != dxgiStatusOccluded
		return nil
	})
}

// resizeAside makes the swap chain's n buffers bw×bh pixels, showing w×h
// of them, as buffers does, ResizeBuffers called on another goroutine
// (goAside), the views of the buffers released before.
func (r *Renderer) resizeAside(w, h, bw, bh, n int) {
	sc, flags := r.swapChain, uintptr(r.swapChainFlags())
	r.goAside("ResizeBuffers", func() uintptr { return resizeBuffersCall(sc, uintptr(n), uintptr(bw), uintptr(bh), flags) }, func(hr uintptr) error {
		if failed(hr) {
			return fmt.Errorf("d3d11: cannot resize the swap chain: %#x", uint32(hr))
		}
		return r.viewBuffers(w, h, bw, bh)
	})
}

// goAside has another goroutine run fn, a call to the swap chain named
// what, which may wait for DWM, and then runs done with what it returned
// on the main thread (post). Meanwhile, the main thread makes no call to the
// device's context nor the swap chain: a frame is put off (Render), the
// frame after waits (Ready), and Release waits for fn (releaseWait). Once it
// did, the frame after draws, at once if one waits, or once the window
// asks for one (askFrame).
func (r *Renderer) goAside(what string, fn func() uintptr, done func(hr uintptr) error) {
	ended, post := make(chan struct{}), r.post
	r.aside, r.asideEnded = what, ended
	go func() {
		hr := fn()
		close(ended)
		post(func() {
			if r.asideEnded != ended {
				return // released meanwhile
			}
			r.aside, r.asideEnded, r.asideDone = "", nil, what
			if err := done(hr); err != nil && r.failed == nil {
				r.failed = err // the next frame finds it
			}
			if r.wake != nil {
				r.woke()
				return
			}
			askFrame(r.hwnd)
		})
	}()
}

// readPeriodAside reads the display's refresh period from DWM on another
// goroutine, as DwmGetCompositionTimingInfo may wait for it, and sets it on
// the main thread (post). Without post, it reads it at once.
func (r *Renderer) readPeriodAside() {
	if r.post == nil {
		r.cost.readPeriod()
		return
	}
	if r.readingPeriod {
		return
	}
	r.readingPeriod = true
	post := r.post
	go func() {
		p := dwmPeriod()
		post(func() {
			r.readingPeriod = false
			if p != 0 {
				r.cost.period = p
			}
		})
	}()
}

// unbindBuffers unbinds the render target views of the swap chain's
// buffers from the device context and submits the commands drawing into
// them, before ResizeBuffers, which needs no reference to the buffers
// left: a view bound to the context is one. The context waits for the GPU
// to be done with them by itself.
func (r *Renderer) unbindBuffers() {
	call(r.ctx, ctxOMSetRenderTargets, 0, 0, 0)
	r.drawing = 0
	call(r.ctx, ctxFlush)
}
