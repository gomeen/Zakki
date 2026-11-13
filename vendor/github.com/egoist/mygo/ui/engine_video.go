package ui

import (
	"sync"
	"time"
	"weak"

	"github.com/egoist/mygo/internal/media"
	"github.com/egoist/mygo/internal/scene"
)

// NewEngineVideoPlayer returns a player of a video that e, an engine of
// the system's media framework, plays: package ui/video's Open makes
// them. The player holds the engine paused until a Video shows it, then
// plays it, unless paused.
//
// It is exported for ui/video only, as media is internal: apps open
// videos with ui/video.
func NewEngineVideoPlayer(e media.Engine) *VideoPlayer {
	v := &videoEngine{e: e, held: true}
	p := &VideoPlayer{bitmap: &Bitmap{img: scene.NewImageRGBA(0, 0, nil)}, eng: v, rate: 1, volume: 1}
	e.SetWake(v.wakeWindow)
	return p
}

// videoEngine is what a VideoPlayer keeps of the engine playing its video.
type videoEngine struct {
	e     media.Engine
	loop  bool
	state media.State
	err   error
	// ready tells the engine gave a picture, which the bitmap shows.
	ready bool
	// paused is the app's Pause, held a pause of the player's own, while
	// nothing shows the video or, muted, while it is out of the view or
	// the window out of sight, or the desktop asks for less motion.
	paused, held bool
	closed       bool
	// muted is the player's SetMuted, and shown whether a Video showed
	// the video yet.
	muted, shown bool
	// rt and frame are the window and its frame that last built the
	// video, built the frame since which it showed in each.
	rt    *engine
	frame uint64

	mu   sync.Mutex
	wake weak.Pointer[engine]
}

// wakeWindow has the window that last built the video build a frame:
// called by the engine, from any goroutine.
func (v *videoEngine) wakeWindow() {
	v.mu.Lock()
	rt := v.wake.Value()
	v.mu.Unlock()
	if rt != nil {
		rt.host.invalidate()
	}
}

func (v *videoEngine) setLoop(loop bool) {
	v.loop = loop
	v.e.SetLoop(loop)
}

// play goes on, or plays again from the start once ended, unless the
// player holds it.
func (v *videoEngine) play() {
	if v.closed {
		return
	}
	v.paused = false
	if v.state == media.Ended {
		v.e.Seek(0)
		v.state = media.Ready
	}
	if !v.held {
		v.e.Play()
	}
	v.wakeWindow()
}

func (v *videoEngine) pause() {
	if v.closed {
		return
	}
	v.paused = true
	v.e.Pause()
	v.wakeWindow()
}

func (v *videoEngine) playing() bool {
	return !v.closed && !v.paused && v.state != media.Ended && v.state != media.Failed
}

func (v *videoEngine) position() time.Duration {
	if v.closed {
		return 0
	}
	return v.e.Position()
}

func (v *videoEngine) seek(d time.Duration) {
	if v.closed {
		return
	}
	v.e.Seek(max(d, 0))
	if v.state == media.Ended {
		v.state = media.Ready
	}
	v.wakeWindow()
}

func (v *videoEngine) close() {
	if v.closed {
		return
	}
	v.closed = true
	v.e.SetWake(nil)
	v.e.Close()
	v.state = media.Ended
}

// hold pauses the engine for the player, or lets it play again.
func (v *videoEngine) hold(on bool) {
	if v.closed || v.held == on {
		return
	}
	v.held = on
	switch {
	case on && !v.paused:
		v.e.Pause()
	case !on && v.playing():
		v.e.Play()
	}
}

// advance shows the picture the engine has due as the frame being built
// shows, once a frame, and asks for the frame of the next one.
func (v *videoEngine) advance(c *context, p *VideoPlayer) {
	rt := c.rt
	if v.rt == rt && v.frame == rt.frame {
		return
	}
	v.rt, v.frame, v.shown = rt, rt.frame, true
	rt.videos = append(rt.videos, v)
	if v.closed {
		return
	}
	v.mu.Lock()
	v.wake = weak.Make(rt)
	v.mu.Unlock()
	state, err := v.e.State()
	if state == media.Ended && v.loop {
		state = media.Ready
	}
	v.state, v.err = state, err
	// A muted video is a picture, as a background is: it plays only
	// while some of it shows, and stands still for less motion.
	occluded := rt.host.occluded()
	v.hold(v.muted && (occluded || rt.preferences().ReduceMotion))
	if occluded {
		return // no frame builds until some of the window shows again
	}
	if pic, changed, ok := v.e.Frame(c.now); ok {
		v.ready = true
		if changed {
			p.bitmap.showPicture(pic)
		}
		if v.state == media.Loading {
			// The first picture, which makes the engine ready: it plays
			// on from this frame, with no wake to come for it.
			v.state, v.err = v.e.State()
			if v.state == media.Ended && v.loop {
				v.state = media.Ready
			}
		}
	}
	if d := v.e.Interval(); d > 0 && v.playing() && !v.held && v.state != media.Loading {
		c.After(time.Duration(float64(d) / p.rate))
	}
}

// sweepVideos holds the muted videos of engines that the frame just built
// did not show, which went out of the view, once a frame that built the
// view: the frames that only paint it again keep the frame number.
func (rt *engine) sweepVideos() {
	if rt.videoSweep == rt.frame {
		return
	}
	rt.videoSweep = rt.frame
	for _, v := range rt.shownVideos {
		if v.rt != rt || v.frame != rt.frame {
			v.unseen()
		}
	}
	rt.shownVideos, rt.videos = rt.videos, rt.shownVideos[:0]
}

// releaseVideos holds the videos of a window that closed.
func (rt *engine) releaseVideos() {
	for _, v := range rt.shownVideos {
		v.unseen()
	}
	for _, v := range rt.videos {
		v.unseen()
	}
	rt.shownVideos, rt.videos = nil, nil
}

// unseen holds a video nothing shows any more, if it is muted.
func (v *videoEngine) unseen() {
	if v.muted {
		v.hold(true)
	}
}

// setMuted mutes the sound, or plays it again: a video held as muted
// out of the view plays on, unseen, once it has sound.
func (v *videoEngine) setMuted(m bool) {
	v.muted = m
	v.e.SetMuted(m)
	if !m && v.shown {
		v.hold(false)
	}
}
