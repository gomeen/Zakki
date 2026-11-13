// Package imagesrc seals ui.ImageSource: the pictures ui.Image shows are
// package ui's bitmaps and SVGs, and Frames, the textures of native
// modules (internal/texture), which embed Mark. Package ui knows textures
// only through Frames, so that apps without one link none of their code.
package imagesrc

import "github.com/egoist/mygo/internal/scene"

// Source is what ui.Image shows.
type Source interface{ imageSource() }

// Mark makes the type embedding it a Source.
type Mark struct{}

func (Mark) imageSource() {}

// Frames is a Source whose frames something else draws: a native.Texture.
type Frames interface {
	Source
	// Size returns the size of the last frame, in pixels.
	Size() (w, h float32)
	// Frame returns the image a frame draws, nil before there is one;
	// invalidate asks the window of key for a frame once another comes.
	// Main thread only.
	Frame(key any, invalidate func()) *scene.Image
}
