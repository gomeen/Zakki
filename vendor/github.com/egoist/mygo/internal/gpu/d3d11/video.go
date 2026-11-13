//go:build windows

package d3d11

import (
	"unsafe"

	"github.com/egoist/mygo/internal/scene"
)

// Frames of video (scene.Surface) reach the GPU as the decoder left them:
// a texture in BGRA that Media Foundation drew on a device of its own and
// shares by an NT handle (Surface.Native), which the renderer opens on
// its device once and reads under the texture's keyed mutex, or planes in
// memory, which it uploads, NV12's luma and chroma into textures of their
// own that the shader converts.

const (
	devOpenSharedResource1 = 48 // ID3D11Device1
	keyedAcquireSync       = 8  // IDXGIKeyedMutex
	keyedReleaseSync       = 9
	formatR8G8Unorm        = 49
	// acquireTimeout is how long, in ms, a frame waits for the decoder to
	// finish writing a frame of video into a shared texture.
	acquireTimeout = 100
	waitTimeout    = 0x102
)

var (
	iidID3D11Device1   = guid{0xa04bfb29, 0x08ef, 0x43d6, [8]byte{0xa4, 0x9c, 0xa9, 0xbd, 0xbd, 0xcb, 0xe6, 0x86}}
	iidIDXGIKeyedMutex = guid{0x9d8e1289, 0xd7b3, 0x465f, [8]byte{0x81, 0x26, 0x25, 0x0e, 0x34, 0x9a, 0xf8, 0x5d}}
)

// sharedTexture is a texture a decoder shares, opened on the renderer's
// device.
type sharedTexture struct {
	texture
	mutex uintptr // IDXGIKeyedMutex
}

// videoState is what the renderer keeps of frames of video.
type videoState struct {
	device1 uintptr // ID3D11Device1, for OpenSharedResource1
	shared  map[uint64]*sharedTexture
	// acquired are the keyed mutexes the frame being drawn holds, released
	// once its draws are issued (releaseShared).
	acquired []uintptr
}

// surfaceView returns the view of the texture of a frame of video: its
// luma, for NV12 in memory, whose chroma chromaView returns.
func (r *Renderer) surfaceView(img *scene.Image, s *scene.Surface) uintptr {
	if s.Native != 0 {
		return r.sharedView(s)
	}
	t := r.images[img.ID()]
	cw, ch := (img.W+1)/2, (img.H+1)/2
	if t != nil && (t.chroma == nil || t.w != img.W || t.h != img.H) {
		freeTexture(t)
		t = nil
	}
	if t == nil {
		t = &texture{chroma: &texture{}}
		if r.planeTexture(t, img.W, img.H, formatR8Unorm, s.Planes[0]) != nil ||
			r.planeTexture(t.chroma, cw, ch, formatR8G8Unorm, s.Planes[1]) != nil {
			freeTexture(t)
			delete(r.images, img.ID())
			return 0
		}
		t.ver = img.Version()
		r.images[img.ID()] = t
	} else if t.ver != img.Version() {
		call(r.ctx, ctxUpdateSubresource, t.tex, 0, 0, uintptr(unsafe.Pointer(&s.Planes[0].Pix[0])), uintptr(s.Planes[0].Stride), 0)
		call(r.ctx, ctxUpdateSubresource, t.chroma.tex, 0, 0, uintptr(unsafe.Pointer(&s.Planes[1].Pix[0])), uintptr(s.Planes[1].Stride), 0)
		t.ver = img.Version()
	}
	t.lastFrame = r.frame
	return t.srv
}

// planeTexture makes t a texture of a plane of w×h texels in memory.
func (r *Renderer) planeTexture(t *texture, w, h int, format uint32, p scene.Plane) error {
	if err := r.newTexture(t, w, h, format, nil, 1); err != nil {
		return err
	}
	call(r.ctx, ctxUpdateSubresource, t.tex, 0, 0, uintptr(unsafe.Pointer(&p.Pix[0])), uintptr(p.Stride), 0)
	return nil
}

// chromaView returns the view of the chroma of a frame of video in NV12,
// after imageView returned that of its luma, or 0.
func (r *Renderer) chromaView(img *scene.Image) uintptr {
	if t := r.images[img.ID()]; t != nil && t.chroma != nil {
		return t.chroma.srv
	}
	return 0
}

// sharedView opens the texture a decoder shares, once, and acquires its
// keyed mutex for the frame, which releaseShared releases once its draws
// are issued: the GPU then reads it after the decoder's writes, and the
// decoder writes it again after the frame's reads.
func (r *Renderer) sharedView(s *scene.Surface) uintptr {
	v := &r.video
	t := v.shared[s.NativeID]
	if t == nil {
		if v.device1 == 0 && failed(call(r.device, 0, uintptr(unsafe.Pointer(&iidID3D11Device1)), uintptr(unsafe.Pointer(&v.device1)))) {
			v.device1 = 0
			return 0
		}
		t = &sharedTexture{}
		if failed(call(v.device1, devOpenSharedResource1, s.Native, uintptr(unsafe.Pointer(&iidID3D11Texture2D)), uintptr(unsafe.Pointer(&t.tex)))) {
			return 0
		}
		if failed(call(t.tex, 0, uintptr(unsafe.Pointer(&iidIDXGIKeyedMutex)), uintptr(unsafe.Pointer(&t.mutex)))) ||
			failed(call(r.device, devCreateShaderResourceView, t.tex, 0, uintptr(unsafe.Pointer(&t.srv)))) {
			free(&t.mutex)
			free(&t.srv)
			free(&t.tex)
			return 0
		}
		if v.shared == nil {
			v.shared = map[uint64]*sharedTexture{}
		}
		v.shared[s.NativeID] = t
	}
	if t.lastFrame != r.frame || !v.holds(t.mutex) {
		hr := call(t.mutex, keyedAcquireSync, 0, acquireTimeout)
		if failed(hr) || hr == waitTimeout {
			return 0
		}
		v.acquired = append(v.acquired, t.mutex)
	}
	t.lastFrame = r.frame
	return t.srv
}

func (v *videoState) holds(mutex uintptr) bool {
	for _, m := range v.acquired {
		if m == mutex {
			return true
		}
	}
	return false
}

// releaseShared releases the keyed mutexes of the shared textures the
// frame read, its draws issued.
func (r *Renderer) releaseShared() {
	for _, m := range r.video.acquired {
		call(m, keyedReleaseSync, 0)
	}
	r.video.acquired = r.video.acquired[:0]
}

// sweepShared closes the shared textures no frame read for a while.
func (r *Renderer) sweepShared(all bool) {
	for key, t := range r.video.shared {
		if all || r.frame-t.lastFrame > 240 {
			free(&t.mutex)
			free(&t.srv)
			free(&t.tex)
			delete(r.video.shared, key)
		}
	}
	if all {
		free(&r.video.device1)
	}
}

// freeTexture frees an image's texture, and its chroma's.
func freeTexture(t *texture) {
	free(&t.srv)
	free(&t.tex)
	if t.chroma != nil {
		free(&t.chroma.srv)
		free(&t.chroma.tex)
		t.chroma = nil
	}
}
