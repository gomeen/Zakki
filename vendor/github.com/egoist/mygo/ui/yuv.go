package ui

import (
	"encoding/binary"
	"fmt"

	"github.com/egoist/mygo/internal/media"
	"github.com/egoist/mygo/internal/scene"
)

// A YUVImage is a picture in Y′CbCr with its chroma at half the size both
// ways (4:2:0), as video decoders give their frames, for a bitmap to show
// without converting it to RGBA (Bitmap.UpdateYUV).
type YUVImage struct {
	// Format is how the planes hold the samples.
	Format YUVFormat
	// Width and Height are the picture's size in pixels. Its chroma is
	// half as wide and high, rounded up.
	Width, Height int
	// Planes are the luma, a byte a pixel, then NV12's chroma, Cb and Cr
	// interleaved, a pair of bytes for each sample, or I420's Cb and Cr,
	// a byte each. Strides are their rows' lengths in bytes, which may be
	// longer than the samples, as decoders pad them.
	Planes  [3][]byte
	Strides [3]int
	// Matrix converts the samples to RGB, and FullRange tells that they
	// take all of 0 to 255 rather than video's range, 16 to 235 for luma
	// and 16 to 240 for chroma, as JPEG's do and video's rarely do.
	Matrix    YUVMatrix
	FullRange bool
}

// YUVFormat is how a YUVImage lays out its samples.
type YUVFormat uint8

const (
	// NV12 is a plane of luma and one of chroma, Cb and Cr interleaved, as
	// hardware decoders leave pictures (VideoToolbox, Direct3D, VA-API).
	NV12 YUVFormat = iota
	// I420 is a plane of luma, one of Cb and one of Cr, as software
	// decoders leave pictures (FFmpeg's YUV420P).
	I420
)

// YUVMatrix is the standard whose coefficients convert Y′CbCr to RGB.
type YUVMatrix uint8

const (
	// BT601 is standard definition video's, and JPEG's.
	BT601 YUVMatrix = iota
	// BT709 is HD video's, as most streams, cameras and screen
	// recordings are.
	BT709
	// BT2020 is UHD video's. Only its matrix applies: HDR video shows with
	// the transfer of SDR.
	BT2020
)

// UpdateYUV shows img in place of the bitmap's picture, as Update does,
// for frames of video a decoder gives in Y′CbCr: the GPU uploads its
// planes, 1.5 bytes a pixel against RGBA's 4, and converts them as it
// samples them, so that showing a frame costs the CPU no conversion. The
// planes are taken as they are, without a copy, but for I420's chroma,
// which goes into a plane the bitmap keeps, interleaved as NV12's: don't
// change them until the next update. A renderer that draws on the CPU,
// and a capture, converts the picture as it needs it.
//
// The bitmap shows the picture at its size, which the GPU scales as it
// samples it, without the halved copies of a bitmap's picture shown much
// smaller: have the decoder give the size the bitmap shows at, where it
// can. Call it on the main thread, in the view or through Window.Update.
// It panics on a picture its planes cannot hold, or of a format or a
// matrix it does not know.
func (b *Bitmap) UpdateYUV(img *YUVImage) {
	if b.img.Scene != nil {
		return // a canvas's: Canvas.Draw draws it
	}
	s := img.surface(&b.chroma)
	w, h := img.Width, img.Height
	b.showPicture(media.Picture{Surface: s, W: w, H: h, RGBA: func(dst []byte) []byte { return s.RGBA(dst, w, h) }})
}

// surface returns the frame of video showing img, NV12's planes as they
// are and I420's chroma interleaved into chroma, which it grows as
// needed. It panics on planes too small for img's size.
func (img *YUVImage) surface(chroma *[]byte) *scene.Surface {
	w, h := img.Width, img.Height
	if w <= 0 || h <= 0 {
		panic(fmt.Sprintf("ui: a YUVImage of %d×%d", w, h))
	}
	if img.Matrix > BT2020 {
		panic(fmt.Sprintf("ui: a YUVImage of matrix %d", img.Matrix))
	}
	cw, ch := (w+1)/2, (h+1)/2
	img.plane(0, w, h)
	s := &scene.Surface{Format: scene.SurfaceNV12, Matrix: scene.YUVMatrix(img.Matrix)}
	if img.FullRange {
		s.Matrix += scene.FullRange
	}
	s.Planes[0] = scene.Plane{Pix: img.Planes[0], Stride: img.Strides[0]}
	switch img.Format {
	case NV12:
		img.plane(1, 2*cw, ch)
		if img.Strides[1]%2 != 0 {
			// Renderers read the chroma in pairs of bytes, rows of whole pairs.
			panic(fmt.Sprintf("ui: the chroma of a %d×%d NV12 YUVImage in rows of %d bytes, an odd number", w, h, img.Strides[1]))
		}
		s.Planes[1] = scene.Plane{Pix: img.Planes[1], Stride: img.Strides[1]}
	case I420:
		img.plane(1, cw, ch)
		img.plane(2, cw, ch)
		if n := 2 * cw * ch; cap(*chroma) < n {
			*chroma = make([]byte, n)
		}
		c := (*chroma)[:2*cw*ch]
		for y := range ch {
			interleave(c[2*cw*y:][:2*cw], img.Planes[1][img.Strides[1]*y:][:cw], img.Planes[2][img.Strides[2]*y:][:cw])
		}
		s.Planes[1] = scene.Plane{Pix: c, Stride: 2 * cw}
	default:
		panic(fmt.Sprintf("ui: a YUVImage of format %d", img.Format))
	}
	return s
}

// plane panics unless plane i holds rows of w bytes, h of them.
func (img *YUVImage) plane(i, w, h int) {
	stride := img.Strides[i]
	if stride < w || len(img.Planes[i]) < stride*(h-1)+w {
		panic(fmt.Sprintf("ui: plane %d of a %d×%d YUVImage holds %d bytes in rows of %d, too few for %d rows of %d", i, img.Width, img.Height, len(img.Planes[i]), stride, h, w))
	}
}

// interleave writes u and v into dst, a byte of each in turn, as NV12's
// chroma holds them.
func interleave(dst, u, v []byte) {
	x := 0
	for ; x+4 <= len(u); x += 4 {
		binary.LittleEndian.PutUint64(dst[2*x:], uint64(u[x])|uint64(v[x])<<8|uint64(u[x+1])<<16|uint64(v[x+1])<<24|
			uint64(u[x+2])<<32|uint64(v[x+2])<<40|uint64(u[x+3])<<48|uint64(v[x+3])<<56)
	}
	for ; x < len(u); x++ {
		dst[2*x], dst[2*x+1] = u[x], v[x]
	}
}
