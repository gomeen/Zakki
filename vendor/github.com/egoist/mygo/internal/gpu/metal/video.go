//go:build darwin

package metal

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/mygo/internal/scene"
)

// Frames of video (scene.Surface) reach the GPU as the decoder left them:
// a CVPixelBuffer backed by an IOSurface (Surface.Native), which a
// CVMetalTextureCache wraps as a texture for each of its planes without a
// copy, or planes in memory, which are uploaded. CoreVideo is loaded with
// the first frame of video, so that windows showing none do not map it.

const pixelFormatRG8Unorm = 30

var (
	cvOnce sync.Once
	cvErr  error

	cvMetalTextureCacheCreate                 func(allocator, attrs, device, texAttrs uintptr, out *uintptr) int32
	cvMetalTextureCacheCreateTextureFromImage func(allocator, cache, image, attrs uintptr, format uint, w, h, plane uint, out *uintptr) int32
	cvMetalTextureGetTexture                  func(tex uintptr) uintptr
	cvMetalTextureCacheFlush                  func(cache uintptr, options uint64)
	cvPixelBufferGetWidthOfPlane              func(buf uintptr, plane uint) uint
	cvPixelBufferGetHeightOfPlane             func(buf uintptr, plane uint) uint
	cvPixelBufferGetWidth                     func(buf uintptr) uint
	cvPixelBufferGetHeight                    func(buf uintptr) uint
)

func loadCoreVideo() error {
	cvOnce.Do(func() {
		h, err := purego.Dlopen("/System/Library/Frameworks/CoreVideo.framework/CoreVideo", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			cvErr = err
			return
		}
		defer func() {
			if v := recover(); v != nil {
				cvErr = v.(error)
			}
		}()
		purego.RegisterLibFunc(&cvMetalTextureCacheCreate, h, "CVMetalTextureCacheCreate")
		purego.RegisterLibFunc(&cvMetalTextureCacheCreateTextureFromImage, h, "CVMetalTextureCacheCreateTextureFromImage")
		purego.RegisterLibFunc(&cvMetalTextureGetTexture, h, "CVMetalTextureGetTexture")
		purego.RegisterLibFunc(&cvMetalTextureCacheFlush, h, "CVMetalTextureCacheFlush")
		purego.RegisterLibFunc(&cvPixelBufferGetWidthOfPlane, h, "CVPixelBufferGetWidthOfPlane")
		purego.RegisterLibFunc(&cvPixelBufferGetHeightOfPlane, h, "CVPixelBufferGetHeightOfPlane")
		purego.RegisterLibFunc(&cvPixelBufferGetWidth, h, "CVPixelBufferGetWidth")
		purego.RegisterLibFunc(&cvPixelBufferGetHeight, h, "CVPixelBufferGetHeight")
	})
	return cvErr
}

// videoState is what the renderer keeps of frames of video: the texture
// cache, and the CVMetalTextures (CF objects) that frames in flight read,
// released once the frame of their slot completed.
type videoState struct {
	cache uintptr
	// held are the CVMetalTextures given up while the frame of a slot was
	// built, which frames in flight may read still: released once that
	// slot's frame completed (waitSlot), which the queue finishes after
	// those before it.
	held [ringSize][]uintptr
}

// surfaceTexture returns the texture of a frame of video: its luma, for
// NV12, whose chroma chromaTexture returns.
func (r *Renderer) surfaceTexture(img *scene.Image, s *scene.Surface) id {
	t := r.images[img.ID()]
	if t != nil && t.ver == img.Version() && t.w == img.W && t.h == img.H && (t.chroma != 0 || s.Format != scene.SurfaceNV12) {
		t.lastFrame = r.frame
		return t.tex
	}
	if s.Native != 0 {
		if r.wrapSurface(img, s) {
			t = r.images[img.ID()]
			t.lastFrame = r.frame
			return t.tex
		}
		return 0
	}
	// Planes in memory: uploaded into textures of their own.
	cw, ch := (img.W+1)/2, (img.H+1)/2
	if t != nil && (t.cv != [2]uintptr{} || t.chroma == 0 || t.w != img.W || t.h != img.H) {
		r.dropTexture(t)
		delete(r.images, img.ID())
		t = nil
	}
	if t == nil {
		luma := r.newTexture(img.W, img.H, pixelFormatR8Unorm, usageShaderRead, s.Planes[0].Pix, s.Planes[0].Stride)
		chroma := r.newTexture(cw, ch, pixelFormatRG8Unorm, usageShaderRead, s.Planes[1].Pix, s.Planes[1].Stride)
		if luma == 0 || chroma == 0 {
			release(&luma)
			release(&chroma)
			return 0
		}
		t = &texture{tex: luma, chroma: chroma, w: img.W, h: img.H, ver: img.Version()}
		r.images[img.ID()] = t
	} else {
		r.waitLast() // frames in flight read these textures
		replace := sel("replaceRegion:mipmapLevel:withBytes:bytesPerRow:")
		msgReplaceRegion(t.tex, replace, mtlRegion{W: uint(img.W), H: uint(img.H), D: 1}, 0, unsafe.Pointer(&s.Planes[0].Pix[0]), uint(s.Planes[0].Stride))
		msgReplaceRegion(t.chroma, replace, mtlRegion{W: uint(cw), H: uint(ch), D: 1}, 0, unsafe.Pointer(&s.Planes[1].Pix[0]), uint(s.Planes[1].Stride))
		t.ver = img.Version()
	}
	t.lastFrame = r.frame
	return t.tex
}

// wrapSurface makes the textures of a frame in a CVPixelBuffer, without a
// copy, the image's: its luma and chroma for NV12, else its BGRA.
func (r *Renderer) wrapSurface(img *scene.Image, s *scene.Surface) bool {
	if loadCoreVideo() != nil {
		return false
	}
	v := &r.video
	if v.cache == 0 {
		if cvMetalTextureCacheCreate(0, 0, r.device, 0, &v.cache) != 0 {
			v.cache = 0
			return false
		}
	}
	var cv [2]uintptr
	wrap := func(plane uint, format uint, w, h uint) bool {
		return cvMetalTextureCacheCreateTextureFromImage(0, v.cache, s.Native, 0, format, w, h, plane, &cv[plane]) == 0 && cv[plane] != 0
	}
	ok := false
	if s.Format == scene.SurfaceNV12 {
		ok = wrap(0, pixelFormatR8Unorm, cvPixelBufferGetWidthOfPlane(s.Native, 0), cvPixelBufferGetHeightOfPlane(s.Native, 0)) &&
			wrap(1, pixelFormatRG8Unorm, cvPixelBufferGetWidthOfPlane(s.Native, 1), cvPixelBufferGetHeightOfPlane(s.Native, 1))
	} else {
		ok = wrap(0, pixelFormatBGRA8Unorm, cvPixelBufferGetWidth(s.Native), cvPixelBufferGetHeight(s.Native))
	}
	if !ok {
		for _, c := range cv {
			if c != 0 {
				purego.SyscallN(cfRelease, c)
			}
		}
		return false
	}
	if old := r.images[img.ID()]; old != nil {
		r.dropTexture(old)
	}
	t := &texture{w: img.W, h: img.H, ver: img.Version(), cv: cv}
	t.tex = cvMetalTextureGetTexture(cv[0])
	if cv[1] != 0 {
		t.chroma = cvMetalTextureGetTexture(cv[1])
	}
	r.images[img.ID()] = t
	return true
}

// chromaTexture returns the texture of the chroma of a frame of video in
// NV12, after imageTexture returned that of its luma, or 0.
func (r *Renderer) chromaTexture(img *scene.Image) uintptr {
	if t := r.images[img.ID()]; t != nil {
		return t.chroma
	}
	return 0
}

// dropTexture frees an image's textures: at once those the renderer
// made, and those wrapping a CVPixelBuffer once the frames in flight,
// which may read them, completed.
func (r *Renderer) dropTexture(t *texture) {
	if t.cv != [2]uintptr{} {
		for _, c := range t.cv {
			if c != 0 {
				r.video.held[r.inUse] = append(r.video.held[r.inUse], c)
			}
		}
		t.cv, t.tex, t.chroma = [2]uintptr{}, 0, 0
		return
	}
	release(&t.tex)
	release(&t.chroma)
}

// releaseHeld releases the CVMetalTextures given up while the frame of a
// slot was built, once it completed.
func (r *Renderer) releaseHeld(slot int) {
	held := r.video.held[slot]
	for _, c := range held {
		purego.SyscallN(cfRelease, c)
	}
	r.video.held[slot] = held[:0]
	if len(held) > 0 && r.video.cache != 0 {
		cvMetalTextureCacheFlush(r.video.cache, 0)
	}
}

// releaseVideo frees what frames of video left, the frames in flight
// done.
func (r *Renderer) releaseVideo() {
	for i := range r.video.held {
		r.releaseHeld(i)
	}
	if r.video.cache != 0 {
		purego.SyscallN(cfRelease, r.video.cache)
		r.video.cache = 0
	}
}
