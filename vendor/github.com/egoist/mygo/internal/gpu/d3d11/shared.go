//go:build windows

package d3d11

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"unsafe"

	"github.com/egoist/mygo/internal/gpu"
	"github.com/egoist/mygo/internal/gpu/d3d11/device"
)

// The renderers of a process share one Direct3D 11 device and its
// immediate context (#1517): a device costs the driver tens of megabytes,
// which a device for each window would cost each window. So do what never
// changes once made, the shaders, the input layout and the states, while
// each renderer keeps its swap chain and what its frames change: its
// buffers, textures and queries. The device goes with the last renderer
// holding it.
//
// Renderers draw on the main thread, one frame at a time, so the context
// they share needs no lock of theirs, and each frame binds all the state it
// draws with. The device is multithread protected all the same
// (ID3D10Multithread): Direct3D then serializes each call to the context
// with DXGI's calls on the device's swap chains, so that a swap chain may
// present or resize its buffers on another thread while other windows draw.
//
// When the GPU device is removed (a driver update, a GPU reset), a
// renderer's next present fails and the window makes another renderer
// (ui's windowHost.render): the first one made finds the device removed
// and makes a new one, which the renderers made after it share. The others
// find the device they hold replaced as they next draw (Render), and fail
// so that their windows make them again on the new one, as they would on
// a device of their own that failed. A device made with WARP, for want of
// a GPU, is replaced the same way as a renderer is made once the GPU can
// make one again, as ui does from time to time while drawing in software.

// sharedDevice is the device renderers share, with what they draw with
// that never changes.
type sharedDevice struct {
	device, ctx, ctx1, factory uintptr
	warp                       bool
	// refs counts the renderers holding the device.
	refs int

	vs, ps, layout                             uintptr
	blend, holeBlend, overBlend, overHoleBlend uintptr
	raster, samp                               uintptr
	passVS, downPS, blurPS                     uintptr
	classes                                    [1 << gpu.NumClasses]*classPipe
	compileClasses                             bool
}

// devices holds the device new renderers share, nil before the first or
// after the last renderer released it, or when it was found removed.
var devices struct {
	sync.Mutex
	current *sharedDevice
}

// errDeviceReplaced is Render's error for a renderer whose device another
// one replaced, removed or drawing with WARP.
var errDeviceReplaced = errors.New("d3d11: the device shared by the windows was made again")

// deviceRemoved reports whether the GPU device of d was removed; tests
// replace it.
var deviceRemoved = func(d *sharedDevice) bool {
	const devGetDeviceRemovedReason = 39
	return call(d.device, devGetDeviceRemovedReason) != 0
}

// newDeviceOnGPU makes a device on the GPU only, for one drawing with WARP
// to be replaced; tests replace it.
var newDeviceOnGPU = device.NewOnGPU

// acquireDevice returns the device new renderers share, which the caller
// releases with releaseDevice: the current one, or a new one where there
// is none, it was removed, or it draws with WARP and the GPU can make one
// now.
func acquireDevice() (*sharedDevice, error) {
	devices.Lock()
	defer devices.Unlock()
	d := devices.current
	if d != nil && deviceRemoved(d) {
		devices.current, d = nil, nil
	}
	if d != nil && d.warp {
		if dev, ctx, err := newDeviceOnGPU(); err == nil {
			if n, err := newSharedDevice(dev, ctx, false); err == nil {
				devices.current, d = n, n
			}
		}
	}
	if d == nil {
		dev, ctx, warp, err := device.New()
		if err != nil {
			return nil, err
		}
		if d, err = newSharedDevice(dev, ctx, warp); err != nil {
			return nil, err
		}
		devices.current = d
	}
	d.refs++
	return d, nil
}

// releaseDevice lets go of d, freeing it with the last renderer holding it.
func releaseDevice(d *sharedDevice) {
	devices.Lock()
	defer devices.Unlock()
	if d.refs--; d.refs > 0 {
		return
	}
	if devices.current == d {
		devices.current = nil
	}
	d.free()
}

// replaced reports whether renderers made now share another device than d.
func (d *sharedDevice) replaced() bool {
	devices.Lock()
	defer devices.Unlock()
	return devices.current != d
}

// newSharedDevice makes what renderers draw with on the device dev, whose
// immediate context is ctx; it releases both when it fails.
func newSharedDevice(dev, ctx uintptr, warp bool) (*sharedDevice, error) {
	d := &sharedDevice{device: dev, ctx: ctx, warp: warp}
	if err := d.init(); err != nil {
		d.free()
		return nil, err
	}
	return d, nil
}

var iidID3D10Multithread = guid{0x9b7e4e00, 0x342c, 0x4106, [8]byte{0xa1, 0x9f, 0x4f, 0x27, 0x04, 0xf6, 0x89, 0xf0}}

func (d *sharedDevice) init() error {
	const setMultithreadProtected = 5 // ID3D10Multithread
	var mt uintptr
	if !failed(call(d.device, 0, uintptr(unsafe.Pointer(&iidID3D10Multithread)), uintptr(unsafe.Pointer(&mt)))) {
		call(mt, setMultithreadProtected, 1)
		free(&mt)
	}
	// The factory that made the device's adapter makes the swap chains.
	var dxgiDev, adapter uintptr
	if failed(call(d.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice)), uintptr(unsafe.Pointer(&dxgiDev)))) {
		return errors.New("d3d11: no DXGI device")
	}
	defer free(&dxgiDev)
	// One frame queued ahead of the screen, not three: a frame shows the
	// input of when it was drawn while frames follow each other, as when
	// scrolling or animating.
	var dxgiDev1 uintptr
	if !failed(call(d.device, 0, uintptr(unsafe.Pointer(&iidIDXGIDevice1)), uintptr(unsafe.Pointer(&dxgiDev1)))) {
		call(dxgiDev1, dxgiDevSetMaxLatency, 1)
		free(&dxgiDev1)
	}
	if noPartial || failed(call(d.ctx, 0, uintptr(unsafe.Pointer(&iidID3D11DeviceContext1)), uintptr(unsafe.Pointer(&d.ctx1)))) {
		d.ctx1 = 0
	}
	if failed(call(dxgiDev, dxgiDevGetAdapter, uintptr(unsafe.Pointer(&adapter)))) {
		return errors.New("d3d11: no adapter")
	}
	defer free(&adapter)
	if failed(call(adapter, dxgiGetParent, uintptr(unsafe.Pointer(&iidIDXGIFactory2)), uintptr(unsafe.Pointer(&d.factory)))) {
		return errors.New("d3d11: DXGI 1.2 is required")
	}

	// The shaders compiled ahead of time, or from shader.hlsl when they are
	// older or a test asks (compileShaders), as the device is made.
	code, err := shaderCode()
	if err != nil {
		return err
	}
	d.compileClasses = sourceSum() != shaderSum || compileShaders
	vsCode := code.vs
	for _, s := range []struct {
		code []byte
		out  *uintptr
		vtbl int
		name string
	}{{code.vs, &d.vs, devCreateVertexShader, "vertex"}, {code.ps, &d.ps, devCreatePixelShader, "pixel"},
		{code.passVS, &d.passVS, devCreateVertexShader, "pass vertex"}, {code.down, &d.downPS, devCreatePixelShader, "down"},
		{code.blur, &d.blurPS, devCreatePixelShader, "blur"}} {
		if len(s.code) == 0 || failed(call(d.device, s.vtbl, uintptr(unsafe.Pointer(&s.code[0])), uintptr(len(s.code)), 0, uintptr(unsafe.Pointer(s.out)))) {
			return fmt.Errorf("d3d11: cannot create the %s shader", s.name)
		}
	}
	names := []string{"RECT", "RADII", "INNER", "COLOR", "COLOR", "COLOR", "GRAD", "UV", "CLIP", "CLIPR", "PARAMS", "TRANSFORM", "TRANSFORM", "TRANSFORM"}
	indices := []uint32{0, 0, 0, 0, 1, 2, 0, 0, 0, 0, 0, 0, 1, 2}
	elems := make([]inputElement, len(names))
	cstr := make([][]byte, len(names))
	for i, n := range names {
		cstr[i] = append([]byte(n), 0)
		elems[i] = inputElement{SemanticName: &cstr[i][0], SemanticIndex: indices[i], Format: formatR32G32B32A32Float,
			AlignedByteOffset: uint32(16 * i), InputSlotClass: 1, InstanceDataStepRate: 1}
	}
	if failed(call(d.device, devCreateInputLayout, uintptr(unsafe.Pointer(&elems[0])), uintptr(len(elems)),
		uintptr(unsafe.Pointer(&vsCode[0])), uintptr(len(vsCode)), uintptr(unsafe.Pointer(&d.layout)))) {
		return errors.New("d3d11: cannot create the input layout")
	}

	// Dual-source blending: the destination times one minus the shader's
	// second color, the source's alpha of each channel (D3D11_BLEND_ONE,
	// D3D11_BLEND_INV_SRC1_COLOR and D3D11_BLEND_INV_SRC1_ALPHA).
	bd := blendDesc{}
	bd.RenderTarget[0] = rtBlend{BlendEnable: 1, SrcBlend: 2, DestBlend: 17, BlendOp: 1, SrcBlendAlpha: 2, DestBlendAlpha: 19, BlendOpAlpha: 1, WriteMask: 0xf}
	if failed(call(d.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&d.blend)))) {
		return errors.New("d3d11: cannot create the blend state")
	}
	// Holes take none of the source (D3D11_BLEND_ZERO).
	bd.RenderTarget[0].SrcBlend, bd.RenderTarget[0].SrcBlendAlpha = 1, 1
	if failed(call(d.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&d.holeBlend)))) {
		return errors.New("d3d11: cannot create the blend state of holes")
	}
	// One color: its alpha is the source's (D3D11_BLEND_INV_SRC_ALPHA).
	bd.RenderTarget[0].SrcBlend, bd.RenderTarget[0].DestBlend, bd.RenderTarget[0].SrcBlendAlpha, bd.RenderTarget[0].DestBlendAlpha = 2, 6, 2, 6
	if failed(call(d.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&d.overBlend)))) {
		return errors.New("d3d11: cannot create the blend state of one color")
	}
	bd.RenderTarget[0].SrcBlend, bd.RenderTarget[0].SrcBlendAlpha = 1, 1
	if failed(call(d.device, devCreateBlendState, uintptr(unsafe.Pointer(&bd)), uintptr(unsafe.Pointer(&d.overHoleBlend)))) {
		return errors.New("d3d11: cannot create the blend state of holes of one color")
	}
	rd := rasterizerDesc{FillMode: 3, CullMode: 1, DepthClip: 1, Scissor: 1}
	if failed(call(d.device, devCreateRasterizerState, uintptr(unsafe.Pointer(&rd)), uintptr(unsafe.Pointer(&d.raster)))) {
		return errors.New("d3d11: cannot create the rasterizer state")
	}
	sd := samplerDesc{Filter: 0x15, AddressU: 3, AddressV: 3, AddressW: 3, ComparisonFunc: 1, MaxLOD: math.MaxFloat32}
	if failed(call(d.device, devCreateSamplerState, uintptr(unsafe.Pointer(&sd)), uintptr(unsafe.Pointer(&d.samp)))) {
		return errors.New("d3d11: cannot create the sampler")
	}
	return nil
}

func (d *sharedDevice) free() {
	if d.ctx != 0 {
		call(d.ctx, ctxClearState)
		call(d.ctx, ctxFlush)
	}
	for _, s := range classShaders {
		if p := d.classes[s.classes]; p != nil {
			free(&p.vs)
			free(&p.ps)
		}
	}
	d.classes = [len(d.classes)]*classPipe{}
	for _, p := range []*uintptr{&d.samp, &d.raster, &d.overHoleBlend, &d.overBlend, &d.holeBlend, &d.blend, &d.layout,
		&d.blurPS, &d.downPS, &d.passVS, &d.ps, &d.vs, &d.factory, &d.ctx1, &d.ctx, &d.device} {
		free(p)
	}
}

// useDevice has r draw on d, with what d holds.
func (r *Renderer) useDevice(d *sharedDevice) {
	r.dev = d
	r.device, r.ctx, r.ctx1, r.factory, r.warp = d.device, d.ctx, d.ctx1, d.factory, d.warp
	r.vs, r.ps, r.layout = d.vs, d.ps, d.layout
	r.blend, r.holeBlend, r.overBlend, r.overHoleBlend = d.blend, d.holeBlend, d.overBlend, d.overHoleBlend
	r.raster, r.samp = d.raster, d.samp
	r.passVS, r.downPS, r.blurPS = d.passVS, d.downPS, d.blurPS
	r.classes, r.compileClasses = &d.classes, d.compileClasses
}

// dropDevice has r let go of its device, which it no longer draws on.
func (r *Renderer) dropDevice() {
	if r.dev == nil {
		return
	}
	d := r.dev
	r.useDevice(&sharedDevice{})
	r.dev, r.classes = nil, nil
	releaseDevice(d)
}
