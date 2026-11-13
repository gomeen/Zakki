package ui

import (
	"errors"
	"image"
	"image/draw"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"weak"

	"github.com/egoist/mygo/internal/media"
)

// A VideoDecoder decodes a video's frames one after another, for a
// VideoPlayer to show. NewGIFDecoder and NewAPNGDecoder return one for
// animated GIFs and PNGs; implement one for another format. A player calls
// it from one goroutine at a time. A decoder that also has a method
// Duration() time.Duration reports the video's length through the
// player's Duration.
type VideoDecoder interface {
	// Next decodes the next frame, with io.EOF after the last.
	Next() (VideoFrame, error)
	// Rewind goes back to before the first frame, for the video to play
	// again.
	Rewind() error
}

// A VideoFrame is a picture of a video and how long it shows.
type VideoFrame struct {
	// Image is the picture, of any image type: the player converts it as
	// Bitmap.Update does, and copies it, so the decoder may draw the next
	// frame into it.
	Image image.Image
	// Duration is how long the frame shows; a frame of none shows for
	// 100 ms, as browsers show GIFs'.
	Duration time.Duration
}

// videoAhead is the frames a player keeps ready, ahead of its playhead.
const videoAhead = 2

// A VideoPlayer plays a video, for Video to show. NewVideoPlayer makes one
// of a VideoDecoder's frames, which MyGo decodes in Go; package ui/video
// opens files and streams with the system's own media framework, which
// decodes them on the GPU and plays their sound.
//
// A decoder's frames decode on a goroutine of their own a few ahead, as
// the playhead nears them, and each reaches the screen through a Bitmap's
// Update as it comes due, a frame passed over when the next one is due
// already. A system's frames stay where its decoder left them, on the
// GPU, and the window's renderer draws them from there.
//
// A new player plays as soon as a Video shows it; show it in one Video at
// a time. Call its methods on the main thread, in the view or an input
// handler, as drawing does.
type VideoPlayer struct {
	bitmap *Bitmap
	// eng, unless nil, plays the video with the system's framework (see
	// engine.go); the fields below up to it are the decoder's.
	eng *videoEngine

	dec *videoDecode
	// first is the first frame, kept for less motion, which shows it, and
	// for playing again; firstDur is how long it shows. cur is the frame
	// the playhead, pos, is in, which ends at end, curAt the time of the
	// video it starts at and curDur how long it shows; shows is the one
	// the bitmap shows: cur, or first.
	first    *image.RGBA
	firstDur time.Duration
	cur      *image.RGBA
	shows    *image.RGBA
	curAt    time.Duration
	curDur   time.Duration
	pos, end time.Duration
	// at is when the frame that last built the video began, rt and frame
	// which frame of which window that was, and moved whether the
	// playhead moved on in it.
	at    time.Time
	rt    *engine
	frame uint64
	moved bool
	// frames are what the goroutine decoding ahead sends, done closed as
	// it returns and stop closed to stop it; nil before it starts, and
	// after the video ended or a seek. resume is where the next run goes
	// on: from the decoder's next frame, at the time of the video base,
	// or from its start (rewind).
	frames chan videoFrame
	done   chan struct{}
	stop   chan struct{}
	resume videoResume
	paused bool
	ended  bool
	err    error
	closed bool
	// rate is how fast the video plays, volume and muted its sound's.
	rate   float64
	volume float64
	muted  bool
}

// videoResume is where a player's next run of decoding goes on.
type videoResume struct {
	// rewind goes on from the start: after the first frame, which the
	// player keeps, or from it too (first).
	rewind, first bool
	base          time.Duration
}

// videoDecode is what a player shares with the goroutine decoding ahead of
// it, which must not keep the player alive: a player dropped stops it.
type videoDecode struct {
	d    VideoDecoder
	loop atomic.Bool
	// free are frames shown or passed over, for the goroutine to decode
	// into again.
	free chan *image.RGBA
	// waiting tells that the playhead reached a frame not decoded yet:
	// sending it wakes the window that last built the video, wake, which
	// the goroutine does not keep alive either.
	waiting atomic.Bool
	mu      sync.Mutex
	wake    weak.Pointer[engine]
	quit    chan struct{}
	halted  sync.Once
}

// videoFrame is a frame decoded ahead, starting at the time at of the
// video, or with no image the end of the video, with what ended it.
type videoFrame struct {
	img *image.RGBA
	at  time.Duration
	dur time.Duration
	err error
}

// NewVideoPlayer returns a player of d's frames, playing. It decodes the
// first frame at once, which Video shows from its first frame, and which
// reports a video that does not decode. Make players once, not in the
// view, as bitmaps. Closing the player closes d if d is an io.Closer.
func NewVideoPlayer(d VideoDecoder) (*VideoPlayer, error) {
	f, err := d.Next()
	if err == io.EOF {
		err = errors.New("ui: a video of no frames")
	}
	if err != nil {
		return nil, err
	}
	first := videoRGBA(nil, f.Image)
	dec := &videoDecode{d: d, free: make(chan *image.RGBA, videoAhead+2), quit: make(chan struct{})}
	p := &VideoPlayer{dec: dec, bitmap: NewBitmap(first), first: first, firstDur: videoDuration(f.Duration), rate: 1, volume: 1}
	p.cur, p.shows, p.end, p.curDur = first, first, p.firstDur, p.firstDur
	p.resume.base = p.firstDur
	runtime.AddCleanup(p, func(dec *videoDecode) { dec.halt() }, dec)
	return p, nil
}

// SetLoop plays the video again from its start each time it ends,
// without a gap. It returns the player.
func (p *VideoPlayer) SetLoop(loop bool) *VideoPlayer {
	if p.eng != nil {
		p.eng.setLoop(loop)
		return p
	}
	p.dec.loop.Store(loop)
	return p
}

// Loop reports whether the video plays again as it ends.
func (p *VideoPlayer) Loop() bool {
	if p.eng != nil {
		return p.eng.loop
	}
	return p.dec.loop.Load()
}

// Play goes on from where the video was paused, or plays it again from its
// start once it ended.
func (p *VideoPlayer) Play() {
	if p.eng != nil {
		p.eng.play()
		return
	}
	p.paused = false
	if p.ended && !p.closed {
		p.ended, p.err = false, nil
		p.release(p.cur)
		p.cur, p.pos, p.end, p.curAt, p.curDur = p.first, 0, p.firstDur, 0, p.firstDur
		p.resume = videoResume{rewind: true}
	}
}

// Pause holds the video at the frame it shows.
func (p *VideoPlayer) Pause() {
	if p.eng != nil {
		p.eng.pause()
		return
	}
	p.paused = true
}

// Playing reports whether the video plays: neither paused nor ended. It
// plays while out of sight too, though it does not move on then.
func (p *VideoPlayer) Playing() bool {
	if p.eng != nil {
		return p.eng.playing()
	}
	return !p.paused && !p.ended
}

// Ended reports whether the video played to its end, and stands at its
// last frame; Play plays it again.
func (p *VideoPlayer) Ended() bool {
	if p.eng != nil {
		return p.eng.state == media.Ended
	}
	return p.ended && !p.closed && p.err == nil
}

// Ready reports whether the video has a picture to show: always for a
// decoder's, and for one the system opens once it loaded its first.
func (p *VideoPlayer) Ready() bool {
	if p.eng != nil {
		return p.eng.ready
	}
	return true
}

// Buffering reports whether a stream waits for data it downloads, as a
// player shows a spinner for.
func (p *VideoPlayer) Buffering() bool {
	return p.eng != nil && p.eng.state == media.Buffering
}

// Size returns the size of the video's frames in pixels, which Video
// shows as DIPs: 0×0 until the video is Ready.
func (p *VideoPlayer) Size() (w, h int) { return p.bitmap.Size() }

// Err returns what stopped the video before its end, as a frame that did
// not decode or a stream that broke off: the video shows the frame before
// it.
func (p *VideoPlayer) Err() error {
	if p.eng != nil {
		return p.eng.err
	}
	return p.err
}

// Duration returns the length of the video: 0 while unknown, as for a
// live stream, or a decoder that does not report it.
func (p *VideoPlayer) Duration() time.Duration {
	if p.eng != nil {
		return p.eng.e.Duration()
	}
	if d, ok := p.dec.d.(interface{ Duration() time.Duration }); ok {
		return d.Duration()
	}
	return 0
}

// Position returns the time of the video the player shows.
func (p *VideoPlayer) Position() time.Duration {
	if p.eng != nil {
		return p.eng.position()
	}
	// The frame's start and the time it showed, while it does: till now,
	// as it plays on, though no frame of the window built it since.
	pos := p.pos
	if p.moved && p.rt != nil && !p.rt.closed {
		from := p.at
		if p.rt.shown.After(from) {
			from = p.rt.shown
		}
		if now := p.rt.now(); now.After(from) {
			pos += time.Duration(float64(now.Sub(from)) * p.rate)
		}
	}
	return p.curAt + min(max(pos-(p.end-p.curDur), 0), p.curDur)
}

// Seek shows the video at d, from its start, and goes on playing from
// there unless paused: the frame of a decoder's video showing at d, which
// it decodes from the start up to it.
func (p *VideoPlayer) Seek(d time.Duration) {
	if p.eng != nil {
		p.eng.seek(d)
		return
	}
	if p.closed {
		return
	}
	// The time the video played since the window last built it is
	// before the seek.
	if p.rt != nil && !p.rt.closed {
		p.at = p.rt.now()
	}
	p.seekDecoder(max(d, 0))
}

// SetRate plays the video r times as fast, keeping the pitch of its
// sound: 0.5 at half speed, 2 at double. It returns the player.
func (p *VideoPlayer) SetRate(r float64) *VideoPlayer {
	if r <= 0 {
		r = 1
	}
	p.rate = r
	if p.eng != nil {
		p.eng.e.SetRate(r)
	}
	return p
}

// Rate returns how fast the video plays (SetRate).
func (p *VideoPlayer) Rate() float64 { return p.rate }

// SetVolume sets the volume of the video's sound, from 0 to 1, the full
// volume, as it is from the start. A decoder's video has no sound. It
// returns the player.
func (p *VideoPlayer) SetVolume(v float64) *VideoPlayer {
	p.volume = min(max(v, 0), 1)
	if p.eng != nil {
		p.eng.e.SetVolume(p.volume)
	}
	return p
}

// Volume returns the volume of the video's sound (SetVolume).
func (p *VideoPlayer) Volume() float64 { return p.volume }

// SetMuted silences the video's sound, or plays it again. A muted video
// is a picture only, as a background is: it pauses while out of sight,
// and shows its first frame while the desktop asks for less motion,
// rather than play. It returns the player.
func (p *VideoPlayer) SetMuted(m bool) *VideoPlayer {
	p.muted = m
	if p.eng != nil {
		p.eng.setMuted(m)
	}
	return p
}

// Muted reports whether the video's sound is silenced (SetMuted).
func (p *VideoPlayer) Muted() bool { return p.muted }

// Close stops decoding, and closes the decoder if it is an io.Closer, on
// a goroutine of its own once the frame it may be decoding is done; or
// stops the system's player, and its sound. The video shows the frame it
// was at.
func (p *VideoPlayer) Close() {
	if p.closed {
		return
	}
	p.closed, p.ended = true, true
	if p.eng != nil {
		p.eng.close()
		return
	}
	p.dec.halt()
	done := p.done
	p.frames, p.done, p.stop = nil, nil, nil
	if c, ok := p.dec.d.(io.Closer); ok {
		go func() {
			if done != nil {
				<-done
			}
			c.Close()
		}()
	}
}

// Video creates an element showing a player's video, by default at its
// size as DIPs, scaled to fit when given another size, as Image shows a
// bitmap:
//
//	//go:embed waves.gif
//	var wavesGIF []byte
//
//	d, err := ui.NewGIFDecoder(wavesGIF)
//	...
//	app.waves, err = ui.NewVideoPlayer(d)
//	app.waves.SetLoop(true)
//
//	ui.Video(c, app.waves).Fill().Fit(ui.Cover)
//
// The video moves on only while the view builds it and some of the window
// shows: one that went out of the view, or a window out of sight, goes on
// from where it stood; a system's video with sound plays on, unseen, as a
// browser's does. Each frame of the video is a frame of the window, which
// After asks for as it comes due. While the desktop asks for less motion
// (Preferences.ReduceMotion), a decoder's video, and a system's muted one,
// shows its first frame and does not play.
func coreVideo(c *context, p *VideoPlayer) *node {
	if p == nil {
		return coreImage(c, nil)
	}
	if p.eng != nil {
		p.eng.advance(c, p)
	} else {
		p.advance(c)
	}
	return coreImage(c, p.bitmap)
}

// advance moves the playhead on by the time the video played since the
// frame that last built it, once a frame, and shows the frame due: the
// last one of those that came due, the others passed over, so that a video
// decoded late catches up rather than lags.
func (p *VideoPlayer) advance(c *context) {
	rt, now := c.rt, c.now
	if p.rt == rt && p.frame == rt.frame {
		return // another pass of the frame, which moved it already
	}
	// Only time the video showed moves it: since the frame before, which
	// built it playing too, and since some of the window showed again
	// (SurfaceShown), as no frame builds while none of it shows. What it
	// showed after its last frame before the window went out of sight,
	// less than a frame of the video, does not count.
	from := p.at
	if rt.shown.After(from) {
		from = rt.shown
	}
	if p.moved && p.rt == rt && p.frame+1 == rt.frame && now.After(from) {
		p.pos += time.Duration(float64(now.Sub(from)) * p.rate)
	}
	p.rt, p.frame, p.at = rt, rt.frame, now
	if rt.preferences().ReduceMotion {
		p.moved = false
		p.show(p.first)
		return
	}
	p.moved = !p.paused && !p.ended && !rt.host.occluded()
	if !p.moved {
		if !p.paused && !p.ended {
			// Out of sight, as keys typed into a window that another
			// covers build a frame: the frame After asks for waits for
			// the window to show (holdFrame), and goes on from there.
			c.After(p.untilNext())
		}
		p.show(p.cur)
		return
	}
	if p.frames == nil {
		p.start()
	}
	p.dec.mu.Lock()
	p.dec.wake = weak.Make(rt)
	p.dec.mu.Unlock()
	for p.pos >= p.end {
		f, ok := p.receive()
		if !ok {
			break // not decoded yet: sending it wakes the window
		}
		if f.img == nil {
			p.ended, p.moved, p.err = true, false, f.err
			p.frames = nil
			p.resume = videoResume{rewind: true}
			break
		}
		p.release(p.cur)
		p.cur, p.curAt, p.curDur = f.img, f.at, f.dur
		p.end += f.dur
	}
	p.show(p.cur)
	if p.moved && p.pos < p.end {
		c.After(p.untilNext())
	}
}

// untilNext is the time of the window until the next frame is due.
func (p *VideoPlayer) untilNext() time.Duration {
	return time.Duration(float64(max(p.end-p.pos, 0)) / p.rate)
}

// start has a goroutine decode the frames ahead of the playhead, from
// where resume says: those after the first, which the player decoded, or
// after the one a seek showed, or once the video ended those from its
// start again, after the goroutine that ended it returned.
func (p *VideoPlayer) start() {
	if p.done != nil {
		<-p.done
	}
	frames, done, stop := make(chan videoFrame, videoAhead), make(chan struct{}), make(chan struct{})
	p.frames, p.done, p.stop = frames, done, stop
	go p.dec.run(frames, done, stop, p.resume)
}

// stopRun stops the goroutine decoding ahead, once the frame it may be
// decoding is done, and drops what it decoded.
func (p *VideoPlayer) stopRun() {
	if p.stop != nil {
		close(p.stop)
	}
	if p.done != nil {
		<-p.done
	}
	p.frames, p.done, p.stop = nil, nil, nil
	p.dec.waiting.Store(false)
}

// seekDecoder shows the frame of the video at d, decoding the frames
// from its start up to it, or its last frame, ended, for a d at or past
// its end, and has decoding go on after it.
func (p *VideoPlayer) seekDecoder(d time.Duration) {
	p.stopRun()
	fail := func(err error) {
		p.ended, p.err, p.moved = true, err, false
		p.resume = videoResume{rewind: true}
	}
	dec := p.dec.d
	if err := dec.Rewind(); err != nil {
		fail(err)
		return
	}
	// Each frame is copied, into a buffer no frame shown holds, as the
	// decoder may draw the next into its image: the last one is the one
	// shown when d is past the end.
	var buf *image.RGBA
	select {
	case buf = <-p.dec.free:
	default:
	}
	var at, dur time.Duration
	found, end := false, false
	for {
		f, err := dec.Next()
		if err == io.EOF {
			end = true
			break
		}
		if err != nil {
			fail(err)
			return
		}
		if found {
			at += dur
		}
		buf, dur, found = videoRGBA(buf, f.Image), videoDuration(f.Duration), true
		if at+dur > d {
			break
		}
	}
	if !found {
		fail(errors.New("ui: a video of no frames"))
		return
	}
	p.release(p.cur)
	p.cur, p.curAt, p.curDur = buf, at, dur
	// The playhead within the frame as d is.
	into := min(d-at, dur)
	p.end = p.pos + dur - into
	p.err = nil
	if end && !p.dec.loop.Load() {
		p.ended, p.moved = true, false
		p.resume = videoResume{rewind: true}
		return
	}
	p.ended = false
	if end {
		p.resume = videoResume{rewind: true, first: true} // looping
	} else {
		p.resume = videoResume{base: at + dur}
	}
}

// receive takes the next frame decoded, reporting false when there is none
// yet, which the goroutine is to wake the window for as it sends one.
func (p *VideoPlayer) receive() (videoFrame, bool) {
	select {
	case f := <-p.frames:
		return f, true
	default:
	}
	p.dec.waiting.Store(true)
	select {
	case f := <-p.frames:
		// Sent meanwhile: a wake may come still, for a frame that
		// changes nothing.
		p.dec.waiting.Store(false)
		return f, true
	default:
		return videoFrame{}, false
	}
}

// release gives a frame no longer shown back to the goroutine, to decode
// into again.
func (p *VideoPlayer) release(img *image.RGBA) {
	if img == p.first || img == p.shows {
		return
	}
	select {
	case p.dec.free <- img:
	default:
	}
}

// show has the bitmap show img, which stays as it is until the next show,
// as Bitmap.Update asks.
func (p *VideoPlayer) show(img *image.RGBA) {
	if img == p.shows {
		return
	}
	old := p.shows
	p.shows = img
	p.bitmap.Update(img)
	if old != p.cur {
		p.release(old)
	}
}

// run decodes frames into frames until the video ends, or the player
// stops it (stop) or is gone (halt): from where r says, the first frame
// of a video that plays again too, which the player keeps, after
// rewinding. It sends what ended the video as a frame with no image, nil
// at its end.
func (dec *videoDecode) run(frames chan<- videoFrame, done chan<- struct{}, stop <-chan struct{}, r videoResume) {
	defer close(done)
	send := func(f videoFrame) bool {
		select {
		case frames <- f:
		case <-dec.quit:
			return false
		case <-stop:
			return false
		}
		if dec.waiting.Swap(false) {
			dec.mu.Lock()
			rt := dec.wake.Value()
			dec.mu.Unlock()
			if rt != nil {
				rt.host.invalidate()
			}
		}
		return true
	}
	at := r.base
	if r.rewind {
		at = 0
		err := dec.d.Rewind()
		if err == nil && !r.first {
			var f VideoFrame
			if f, err = dec.d.Next(); err == nil {
				at = videoDuration(f.Duration)
			}
		}
		if err != nil {
			send(videoFrame{err: err})
			return
		}
	}
	for {
		select {
		case <-dec.quit:
			return
		case <-stop:
			return
		default:
		}
		f, err := dec.d.Next()
		if err == io.EOF && dec.loop.Load() {
			if err = dec.d.Rewind(); err == nil {
				f, err = dec.d.Next()
				at = 0
			}
		}
		if err != nil {
			if err == io.EOF {
				err = nil
			}
			send(videoFrame{err: err})
			return
		}
		var buf *image.RGBA
		select {
		case buf = <-dec.free:
		default:
		}
		dur := videoDuration(f.Duration)
		if !send(videoFrame{img: videoRGBA(buf, f.Image), at: at, dur: dur}) {
			return
		}
		at += dur
	}
}

// halt stops the goroutine decoding ahead.
func (dec *videoDecode) halt() { dec.halted.Do(func() { close(dec.quit) }) }

// videoRGBA copies img into dst, or into a new image when dst is nil or of
// another size, converting it as Bitmap.Update does.
func videoRGBA(dst *image.RGBA, img image.Image) *image.RGBA {
	b := img.Bounds()
	if dst == nil || dst.Rect.Dx() != b.Dx() || dst.Rect.Dy() != b.Dy() {
		dst = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	}
	draw.Draw(dst, dst.Rect, img, b.Min, draw.Src)
	return dst
}

// videoDuration is how long a frame shows: 100 ms for none.
func videoDuration(d time.Duration) time.Duration {
	if d <= 0 {
		return 100 * time.Millisecond
	}
	return d
}
