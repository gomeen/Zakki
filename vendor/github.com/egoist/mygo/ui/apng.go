package ui

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"time"
)

// apngDecoder decodes an animated PNG's frames, each drawn over what the
// ones before it left, as their disposal and blending say, into one
// picture. It keeps each frame compressed, as a PNG of its own, and
// decodes it as it plays.
type apngDecoder struct {
	frames []apngFrame
	next   int
	pic    *image.RGBA
	// saved is the picture before the last frame drew, for a frame that
	// disposes to the previous one; nil for one that does not.
	saved []byte
}

// apngFrame is a frame of an animated PNG: the rectangle it draws, how
// long it shows, and how it goes (dispose) and draws (blend).
type apngFrame struct {
	png            []byte
	rect           image.Rectangle
	delay          time.Duration
	dispose, blend byte
}

const (
	apngDisposeBackground = 1
	apngDisposePrevious   = 2
	apngBlendOver         = 1
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// NewAPNGDecoder returns a decoder of an animated PNG's frames, for
// NewVideoPlayer, as browsers show them: a frame of 10 ms or less shows
// for 100 ms. A PNG that is not animated is a video of one frame. Each
// frame is kept compressed, and decoded as it plays. The player's
// SetLoop, not the PNG's count of plays, says whether it plays again.
func NewAPNGDecoder(data []byte) (VideoDecoder, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		return nil, errors.New("ui: not a PNG")
	}
	var (
		ihdr     []byte
		extra    [][]byte // PLTE and tRNS, as each frame's PNG needs them
		frames   []apngFrame
		cur      *apngFrame
		body     []byte // the frame's compressed pixels
		animated bool
		w, h     int
		sawIDAT  bool
	)
	finish := func() error {
		if cur == nil {
			return nil
		}
		if len(body) == 0 {
			return errors.New("ui: an APNG frame of no data")
		}
		cur.png = framePNG(ihdr, extra, cur.rect.Dx(), cur.rect.Dy(), body)
		frames = append(frames, *cur)
		cur, body = nil, nil
		return nil
	}
	rest := data[len(pngSignature):]
	for len(rest) >= 12 {
		n := binary.BigEndian.Uint32(rest)
		if uint64(n)+12 > uint64(len(rest)) {
			return nil, errors.New("ui: a truncated PNG")
		}
		kind, chunk := string(rest[4:8]), rest[8:8+n]
		raw := rest[:12+n]
		rest = rest[12+n:]
		switch kind {
		case "IHDR":
			if n != 13 {
				return nil, errors.New("ui: a PNG of a bad header")
			}
			ihdr = chunk
			w, h = int(binary.BigEndian.Uint32(chunk)), int(binary.BigEndian.Uint32(chunk[4:]))
			if w <= 0 || h <= 0 || uint64(w)*uint64(h) > math.MaxInt/4 {
				return nil, errors.New("ui: a PNG of an invalid size")
			}
		case "PLTE", "tRNS":
			extra = append(extra, raw)
		case "acTL":
			animated = true
		case "fcTL":
			if n != 26 || ihdr == nil {
				return nil, errors.New("ui: an APNG of a bad frame control")
			}
			if err := finish(); err != nil {
				return nil, err
			}
			fw, fh := binary.BigEndian.Uint32(chunk[4:]), binary.BigEndian.Uint32(chunk[8:])
			fx, fy := binary.BigEndian.Uint32(chunk[12:]), binary.BigEndian.Uint32(chunk[16:])
			r := image.Rect(int(fx), int(fy), int(fx)+int(fw), int(fy)+int(fh))
			if fw == 0 || fh == 0 || uint64(fx)+uint64(fw) > uint64(w) || uint64(fy)+uint64(fh) > uint64(h) {
				return nil, errors.New("ui: an APNG frame out of its picture")
			}
			num, den := binary.BigEndian.Uint16(chunk[20:]), binary.BigEndian.Uint16(chunk[22:])
			if den == 0 {
				den = 100
			}
			delay := time.Duration(num) * time.Second / time.Duration(den)
			if delay <= 10*time.Millisecond {
				delay = 100 * time.Millisecond
			}
			cur = &apngFrame{rect: r, delay: delay, dispose: chunk[24], blend: chunk[25]}
			if len(frames) == 0 && cur.dispose == apngDisposePrevious {
				cur.dispose = apngDisposeBackground // nothing before it
			}
		case "IDAT":
			// The default image is the first frame when a frame control
			// comes before it; else, in an animated PNG, not a frame.
			sawIDAT = true
			if cur != nil || !animated {
				body = append(body, chunk...)
			}
		case "fdAT":
			if cur != nil && n >= 4 {
				body = append(body, chunk[4:]...)
			}
		case "IEND":
			rest = nil
		}
	}
	if ihdr == nil || !sawIDAT {
		return nil, errors.New("ui: a PNG of no image")
	}
	if err := finish(); err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		if len(body) == 0 {
			return nil, errors.New("ui: a PNG of no image")
		}
		frames = []apngFrame{{png: framePNG(ihdr, extra, w, h, body), rect: image.Rect(0, 0, w, h), delay: 100 * time.Millisecond}}
	}
	return &apngDecoder{frames: frames, pic: image.NewRGBA(image.Rect(0, 0, w, h))}, nil
}

// framePNG returns a PNG of a frame alone: the header of the animated
// PNG, of the frame's size, its palette and transparency, and the frame's
// pixels.
func framePNG(ihdr []byte, extra [][]byte, w, h int, body []byte) []byte {
	var b bytes.Buffer
	b.Write(pngSignature)
	hdr := append([]byte(nil), ihdr...)
	binary.BigEndian.PutUint32(hdr, uint32(w))
	binary.BigEndian.PutUint32(hdr[4:], uint32(h))
	writeChunk(&b, "IHDR", hdr)
	for _, c := range extra {
		b.Write(c)
	}
	writeChunk(&b, "IDAT", body)
	writeChunk(&b, "IEND", nil)
	return b.Bytes()
}

func writeChunk(b *bytes.Buffer, kind string, data []byte) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(data)))
	b.Write(n[:])
	crc := crc32.NewIEEE()
	crc.Write([]byte(kind))
	crc.Write(data)
	b.WriteString(kind)
	b.Write(data)
	binary.BigEndian.PutUint32(n[:], crc.Sum32())
	b.Write(n[:])
}

func (d *apngDecoder) Next() (VideoFrame, error) {
	i := d.next
	if i >= len(d.frames) {
		return VideoFrame{}, io.EOF
	}
	f := &d.frames[i]
	img, err := png.Decode(bytes.NewReader(f.png))
	if err != nil {
		return VideoFrame{}, err
	}
	if i > 0 {
		d.dispose(i - 1)
	}
	if f.dispose == apngDisposePrevious {
		d.saved = append(d.saved[:0], d.pic.Pix...)
	}
	op := draw.Src
	if f.blend == apngBlendOver {
		op = draw.Over
	}
	draw.Draw(d.pic, f.rect, img, img.Bounds().Min, op)
	d.next++
	return VideoFrame{Image: d.pic, Duration: f.delay}, nil
}

func (d *apngDecoder) Rewind() error {
	d.next = 0
	clear(d.pic.Pix)
	return nil
}

// Duration is the length of the animation, for the player's Duration.
func (d *apngDecoder) Duration() time.Duration {
	var t time.Duration
	for _, f := range d.frames {
		t += f.delay
	}
	return t
}

// dispose undoes frame i as its disposal says, before the next draws: it
// clears its rectangle to transparent, restores what was under it, or
// leaves it.
func (d *apngDecoder) dispose(i int) {
	switch d.frames[i].dispose {
	case apngDisposeBackground:
		r := d.frames[i].rect
		for y := r.Min.Y; y < r.Max.Y; y++ {
			clear(d.pic.Pix[d.pic.PixOffset(r.Min.X, y):d.pic.PixOffset(r.Max.X, y)])
		}
	case apngDisposePrevious:
		if d.saved != nil {
			copy(d.pic.Pix, d.saved)
		}
	}
}
